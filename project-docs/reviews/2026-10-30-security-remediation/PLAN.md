# 整改计划：测试站 sub2.lansn.icu 功能/前端验证 + 安全修复

- 日期：2026-10-30
- 分支：`fix/security-remediation-20261030`（从 `develop` @ `f9d8bc855` 切出，**不碰 main**）
- 证据：同目录 `EVIDENCE.md`（线上实测）、`VERIFICATION.md`（修复验证记录）
- 输入：外部安全评估报告（262 行）+ Lead 线上复测

## 0. 结论摘要

1. **前端已按仓库 HEAD 部署**（产物级比对通过），「看起来没部署」的两个真实原因：PR #7 前端重设计从未合并（线上仍是上游 Teal）、本地内嵌 dist 是 10-21 陈旧产物。
2. **唯一高危已复现**：全站 IP 限流可被伪造 `X-Real-IP`/`CF-Connecting-IP` 绕过（登录、注册、发码、刷令牌、优惠码、支付校验全部失效）。
3. 报告其余 6 条中 5 条成立、F-06 部分不成立但暴露新缺陷 N-01（未知模型返回 503 internal error）。
4. 权限结论与报告一致：普通用户无法越权，管理员边界正常。

## 1. 缺陷清单与优先级

| 编号 | 级别 | 描述 | 根因位置 | 责任人 |
|---|---|---|---|---|
| F-01 | **P0 高危** | 转发头信任方向反转，IP 限流可绕过 | `pkg/ip/ip.go:104-148`、`middleware/rate_limiter.go:139`、`config/config.go:2130`、`service/setting_service.go:352-359` | sec-ip |
| F-02 | P1 中危 | `/redeem` 无独立限流（可判定 oracle） | `server/routes/user.go:134-138` | sec-ip |
| N-01 | P1 中危 | 未知/未定价模型统一 503 `internal error`，与 `/v1/models` 口径矛盾 | `handler/gateway_handler.go:2541-2548` | api-surface |
| F-03 | P1 中危 | `admin@sub2api.local` 等保留标识可注册 | `service/auth_service.go:1404` | api-surface |
| I-01 | P1 中危 | `send-verify-code` 假成功，运维无法发现邮件不可用 | `service/auth_service.go:345-394`、`service/email_queue_service.go:80-100` | api-surface |
| F-04 | P2 低危 | `/channel-monitor-v2/*` 对普通用户泄露监控配置 | `handler/channel_monitor_v2_handler.go`、`server/routes/user.go:157-167` | api-surface |
| F-05 | P2 低危 | `/groups/available` 泄露配额与单价 | `handler/dto/types.go`（GroupFromService） | api-surface |
| F-07 | P2 低危 | `/usage/:id` 403/404 差异泄露存在性 | `handler/usage_handler.go:378-404` | api-surface |
| I-02 | P3 信息 | `/setup/status` 硬编码 | `server/routes/common.go:21-31` | api-surface |
| I-04 | 接受 | 认证体长度指纹 | — | 不修 |
| D-01 | P2 运维 | 本地内嵌 dist 陈旧导致误判部署 | `backend/internal/web/dist/`（gitignore 产物） | deploy-ops |
| D-02 | P2 运维 | `__APP_CONFIG__.version` 空串 | `service/setting_public.go` | deploy-ops |
| D-03 | P2 运维 | Caddyfile 是直连基线，CDN 拓扑下会把所有人坍缩成一个 IP | `deploy/Caddyfile` | deploy-ops |

## 2. 改动方案

### 2.1 P0 转发头信任模型（sec-ip）

**核心原则：信任方向必须由「谁写的头」决定，而不是「头里的值是不是公网」。**

1. `pkg/ip/ip.go`
   - 新增受信代理网段快照（`server.trusted_proxies`）；未配置时默认只信任 `127.0.0.0/8`、`::1/128`（同机反代是唯一安全默认）。
   - `GetClientIP` legacy 分支：先判直连对端 `c.RemoteAddr` 是否受信；**不受信则完全忽略所有转发头**，返回对端地址。
   - 「非私网优先」只保留为受信代理之后的链选择。
   - 保持 `SetForwardedIPSettings`/`GetSecurityClientIP`/`GetTrustedClientIP` 签名不变（65 处调用点零改动）。
2. `service/setting_service.go:352-359`：删除自动把开关改回 `true` 的逻辑，改为 WARN。
3. `middleware/rate_limiter.go`：`RateLimitOptions` 增 `AccountKey`、`SkipPrivateClientIP`；键 = `key:ip` + 可选 `:acct:<sha256(email)[:16]>`（`httputil.PrereadBody` 读体并回填，不落明文）。
   **账号维度是主防线**：即使部署侧 IP 解析仍坍缩，按账号限流依然生效；IP 维度为次防线。这样把「部署配置错误 → 全站 429」的风险降到可控。
4. `routes/auth.go`：login / login-2fa / register / send-verify-code / validate-promo-code 挂账号维度。
5. `routes/user.go`：`/redeem` 独立限流 + 按用户失败计数。
6. `server/middleware/panel_rate_limit.go`：私网不再直接跳过计数（改计数 + 一次性 WARN）。

### 2.2 P1/P2 API 面（api-surface）
见任务板 task-2；要点：保留邮箱域、用户面向 DTO 白名单、监控用户域剔除 `config`、`/usage/:id` 同码同体、未知模型 404 而未定价仍 503（**缺价不得放行**）、发码前校验 SMTP 配置、setup/status 反映真实状态。

### 2.3 前端（frontend）
字段消费清单 → 契约适配 → 缺字段降级 → 设置页 SMTP 阻断提示 → 组件测试 → Playwright E2E（用户端 9 页 + 管理端 7 页，截图与 console/network 断言）。

### 2.4 部署运维（deploy-ops）
`deploy/Caddyfile.edgeone`（`trusted_proxies static <EdgeOne 回源段>` + `trusted_proxies_strict` + `client_ip_headers EO-Connecting-IP X-Forwarded-For`，`header_up X-Real-IP {client_ip}` 覆盖客户端值）；compose 透传 `SERVER_TRUSTED_PROXIES`、`SECURITY_TRUST_FORWARDED_IP_FOR_API_KEY_ACL=false`；版本注入；`tools/verify-deployed-build.ps1`；文档三处更新。

## 3. 验收标准

| # | 验收项 | 判据 |
|---|---|---|
| 1 | 后端构建 | `go build -o bin/server.exe ./cmd/server` 退出 0 |
| 2 | 后端单测 | `go test -tags=unit ./...` 全绿（记录通过数/总数） |
| 3 | 前端类型 | `vue-tsc -b` 退出 0 |
| 4 | 前端单测 | `vitest run` 退出 0（记录通过数） |
| 5 | 前端构建 | `corepack pnpm@9 run build` 成功（含 check:i18n） |
| 6 | 变异验证 | 每个修复点回退后对应测试变红（逐条记录） |
| 7 | 部署一致性 | `tools/verify-deployed-build.ps1` 输出三行结论并退出 0 |
| 8 | 线上回归（部署后） | 轮换 `X-Real-IP` 25 次第 21 次 429；`/redeem` 第 6 次 429；`/usage/:id` 同码同体；`admin@sub2api.local` 被拒；未知模型 404 / 未定价 503；SMTP 未配置时发码报错 |

## 4. 上线顺序（关键：分两步，避免全站 429）

1. **第一步（可观测，不改变判定）**：合入限流账号维度、`/redeem` 限流、API 面收敛、发码真实性、部署工具与文档。此时 IP 解析行为不变，线上零风险。
2. **你确认 `deploy/Caddyfile.edgeone` 已按 EdgeOne 回源段配置并生效**（`header_up X-Real-IP {client_ip}` + `trusted_proxies`）。
3. **第二步（改变判定）**：合入 `pkg/ip/ip.go` 的信任方向反转 + `trust_forwarded_ip_for_api_key_acl=false`。上线后立即执行 §3 第 8 项回归。
4. 回滚：每步都是独立提交，`git revert` 单提交即可；无迁移、无 schema 变更。

## 5. 不在本轮范围
- PR #7 前端视觉重设计（CLOSED，跨 105 文件）：独立一轮 rebase 后单独 PR。
- 线上脏数据清理（攻击者创建的 uid 5–9、`probe-*` 密钥）：需你授权删除，单列待办。
- 并发竞态、SSRF、兑换码熵值评估：需要有效兑换码/生图分组，本轮不具备条件。
- 开启邮箱验证/注册额度：做成配置项默认关闭（SMTP 当前未配置，先修 I-01）。
