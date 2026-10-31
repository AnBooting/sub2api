# PR #18 审查（2026-11-01）

结论：建议修改后再合并。发现 3 项已用生产代码复现的问题（2 项 P1、1 项 P2）。本次仅审查，未修改业务源码、执行 Git 命令、提交 GitHub review、合并或部署。

- PR：https://github.com/AnBooting/sub2api/pull/18
- Base：`f9d8bc855e9ef2a5d60974e15572880559a878cf`（develop）
- Head：`76af737525b0513db2f6268daa3a9389b31cea7f`；结束前通过 GitHub API 复核，仍为该提交且 OPEN。
- 范围：101 个变更文件；审阅生产 diff 与关键调用链，重点验证代理 IP、认证/兑换限流、统计错误和 SMTP 状态。使用独立源码快照，没有切换现有工作树。

## 1. [P1] Compose 的 loopback 默认信任列表不适用于宿主机代理到桥接容器

位置：`deploy/docker-compose.yml:187-188`（local 版本同样位于 190-191），以及 `deploy/.env.example` 的同一示例。

新默认值是 `SERVER_TRUSTED_PROXIES=127.0.0.1/32,::1/128`，并默认关闭兼容开关。但这两个 Compose 文件都采用 bridge 网络及端口映射，没有使用 host 网络。宿主机 Caddy 通过映射端口访问容器时，应用通常看到 Docker 网关地址（如 `172.18.0.1`），而非容器自己的 loopback。注释把 loopback 称为“同宿主 Caddy”的默认正确取值，会直接误导部署。

复现使用真实 `configureClientIPTrust`、`SessionBindingContext`、`GetClientIP` 和 Redis 限流器，模拟该 TCP 对端，并为 21 个不同客户端分别写入正确且不同的 X-Real-IP/XFF：

```text
trust=true  client=1  status=200 body=172.18.0.1
trust=true  client=21 status=429
trust=false client=1  status=200 body=172.18.0.1
trust=false client=21 status=429
```

后果：登录 20/min 的 IP 桶变成全站共用，审计与 IP ACL 同时失真。仅保持数据库中的兼容开关为 true 也无法完成 PR 所述“两步上线”：新 legacy 路径同样先执行可信对端闸门。

建议：按实际容器网络固定并显式配置 Caddy 对端地址（优先精确地址或受控的专用网络），或把可信代理配置设为必填并在部署前校验；同步修正两个 Compose 文件、env 示例和两步上线说明。不要用无差别信任全部地址解决问题。

证据：`docker_proxy_bucket_test.go`。这是对指定 bridge 对端的应用层复现；未启动真实 Docker/Caddy，也未核实生产当前网络拓扑。

## 2. [P1] 身份提取与登录绑定不一致，可以绕过已生效的账号锁

位置：`backend/internal/middleware/rate_limiter.go:180-201`，重点为 `json.Unmarshal` 和区分大小写的 `payload[field]`。

中间件用 map 精确匹配小写 `email`，而真实登录 handler 的 `ShouldBindJSON(&LoginRequest)` 支持字段名大小写不敏感匹配。两者对尾随 JSON 的接受规则也不同。中间件提取失败返回空串后，会跳过账号检查与失败记账，而不是拒绝请求。超过 64 KiB 的合法请求也走同一放行路径：只需增加未知填充字段或空白，便可关闭这层防护；全局生产 body 默认上限是 256 MiB。

使用真实 `RegisterAuthRoutes` → 限流中间件 → `AuthHandler.Login` → `AuthService.Login`，只用用户仓储桩返回 `ErrUserNotFound`，因此凭据错误映射及失败记账均执行生产代码。每个用例先连续失败 5 次，使普通请求返回 429，再提交以下输入：

| 输入变体 | 普通请求对照 | 变体响应 | 是否再次调用用户仓储 |
| --- | --- | --- | --- |
| `{"Email":"victim@example.com","password":"wrong"}` | 429 | 401 | 是，第 6 次，目标仍为 victim |
| 正常对象后追加 ` {}` | 429 | 401 | 是，第 6 次，目标仍为 victim |
| `email=decoy` 与后出现的 `Email=victim` 同时存在 | 429 | 401 | 是，第 6 次，目标仍为 victim |
| 正常对象增加 65536 字节 padding 字段 | 429 | 401 | 是，第 6 次，目标仍为 victim |

混合大小写还允许攻击者按请求轮换 decoy 桶，而真实 handler 始终校验 victim。IP 限流仍在，但分布式来源可以持续对同一账号试密，新加的跨 IP 账号防护实际被跳过。同一提取器还用于注册、发码和 2FA，应统一修复，不能只处理登录的一个拼写。

建议：复用相同的请求解析/校验结果构造账号键，或者统一严格拒绝有歧义、尾随数据及超限请求。64 KiB 若作为认证入口上限，必须真正拒绝超限，而不能降级为无账号维度；未知 Content-Length 也应边读边限量，避免当前先完整读入再判断。

证据：`login_account_bypass_test.go`；4 个子用例均按“锁定后仍应 429”的正确契约失败。没有把 handler 的记账逻辑复制到测试中。

## 3. [P2] 保存任意设置后，SMTP 状态会被错误改成 false

本 PR 引入位置：`backend/internal/handler/dto/settings.go:60-63` / `backend/internal/handler/admin/setting_handler.go:135-171`。遗漏的另一构造点是 `backend/internal/handler/admin/setting_handler_update.go:2267-2295`。

新增 DTO 字段 `SMTPConfigured bool` 会始终序列化，但本 PR 只在 GET 设置响应中计算它。PUT 更新设置时也构造同一 DTO，却没有填入该字段，因此所有成功 PUT 都返回零值 false。前端保存逻辑在 `SettingsView.vue:11960-11964` 把返回字段覆盖到 form，新加提示据此显示“SMTP 未配置”。

真实 GET/PUT handler 复现（只修改站点名称，SMTP host/from 保持有效）：

```text
GET status=200 smtp_configured=true
PUT status=200 smtp_configured=false
SMTP host=smtp.example.com from=noreply@example.com
```

后果：已配置邮件服务的管理员每次保存设置后都看到错误告警，刷新页面才恢复；首次保存正确的 SMTP 配置后也会立即被误报仍未配置。这里实际是错误提示，不声称现有注册页面已被阻断。

建议：GET/PUT 使用同一响应映射函数，或在 PUT 保存成功后同样调用 `EmailService.IsConfigured`。补充保存前后状态一致性回归，而不只测试 GET。

证据：`smtp_settings_consistency_test.go`，正确契约断言失败。

## 已运行验证

使用本机缓存的 Go 1.27.3，`GOTOOLCHAIN=local`，`GOMAXPROCS=4`，`-p 1`。测试均针对上面的 head 快照。

| 检查 | 结果 |
| --- | --- |
| `go build -p 1 -o <临时产物> ./cmd/server` | 退出码 0 |
| `go test -p 1 -tags=unit ./internal/pkg/ip ./internal/middleware ./internal/server/middleware` | 三包通过 |
| 认证/兑换既有路由测试 `TestAuth* / TestRedeem*` | 通过 |
| handler 定向测试：缺价、billing 文案、登录计数、用量归属、用户统计不可用 | 通过 |
| admin 定向测试：SMTP GET 字段、统计统一错误、snapshot 降级、真实错误保持 500 | 通过 |
| 新增账号绕过复现 | 4/4 正确契约断言失败 |
| 新增 Docker 对端复现 | 兼容开关 true/false 两个子用例均失败 |
| 新增 SMTP GET/PUT 一致性复现 | 失败 |

缺价红线的既有 `TestOpenAIChatCompletionsLockedPricingMissingRejectedBeforeUpstream` 的 5 个子用例全部通过；本次没有发现其再次放行上游。

未运行：全量后端测试、前端 typecheck/test/build、真实 PostgreSQL/Redis 集成、真实 CDN/Caddy/Docker 链路、生产端到端扣费。当前工作树与 PR 快照的前端锁文件不相同，没有借用本地依赖把快照的前端验证算成通过。GitHub 当前没有 status checks。

## 复现文件使用

本目录三个 Go 文件为保留的审查证据，位于 backend 模块之外，不加入常规生产测试。将它们复制到上述固定 head 的独立源码副本，并分别命名为：

- `login_account_bypass_test.go` → `backend/internal/server/routes/review_pr18_probe_test.go`
- `docker_proxy_bucket_test.go` → `backend/internal/server/review_pr18_docker_probe_test.go`
- `smtp_settings_consistency_test.go` → `backend/internal/handler/admin/review_pr18_smtp_probe_test.go`

然后从该副本 backend 目录运行：

```powershell
go test -p 1 -tags=unit ./internal/server/routes -run TestReviewPR18LoginCannotSkipLockedAccount -v -count=1
go test -p 1 -tags=unit ./internal/server -run TestReviewPR18ComposeHostProxyKeepsClientBuckets -v -count=1
go test -p 1 -tags=unit ./internal/handler/admin -run TestReviewPR18SettingsSavePreservesSMTPConfigured -v -count=1
```

在本次审查的未修复 head 上，三条命令预期均退出 1。测试复用该 head 的既有测试 helper，必须使用匹配快照。临时下载源码、diff 和构建二进制已在审查完成后清理。
