# PR #18 复审修复记录（2026-11-01）

## 结论

[原始复审](REVIEW.md) 的两项 P1 与一项 P2 已按当前源码逐项修复。工作由 Agent Team 分工完成，Lead 对集成结果重新验收。原始复审和三个复现探针原样保留在本目录。

| 复审项 | 根因 | 修复 | 回归/变异证据 |
|---|---|---|---|
| P1：Compose 代理网段使客户端 IP 坍缩 | bridge 端口映射下应用看到网关而非 loopback；默认兼容开关也已切换 | 两个 Compose 固定 `172.30.18.0/29`、网关 `172.30.18.1`，默认仅信任 `172.30.18.1/32`；同步 env、EDGE 安全契约、两步上线与 DB 优先回滚 | `TestComposeGatewayKeepsClientBucketsAndRejectsOtherPeers` 覆盖 legacy true/false、21 个独立桶及非受信对端；成员回退 loopback 后应用与静态测试分别失败，恢复后通过 |
| P1：认证身份解析绕过账号锁 | map 精确小写取值、`json.Unmarshal` 接受尾随值，超限/解析失败退化为无账号维度 | 认证身份 JSON 严格解析；合法大小写同桶，重复/歧义/尾随/非法 400，超过 64 KiB 413；未知 Content-Length 有界读取；Abort 后不访问 Redis 或业务 handler | 中间件和路由级回归；大小写、第二次 Decode、重复检测、读取上限、已知长度预检、promo code 六项变异均回退失败并恢复通过 |
| P2：保存设置后 SMTP 状态误报 | GET 计算派生字段，PUT 构造同一 DTO 时遗漏赋值 | 提取 `smtpConfigured` 辅助函数，GET/PUT 共用 `EmailService.IsConfigured` | `TestAdminSettingsSavePreservesSMTPConfigured` 修复前 PUT false；移除 PUT 赋值变异失败；恢复后已配置/未配置两组均通过 |

### 同轮接线修正

优惠码端点的 DTO 必填字段是 `code`，原路由却复用 `email` 提取器。严格入口必须同时改为 `authPromoAccountKey()`，否则正常优惠码请求会缺账号字段；现已补路由回归。

## Lead 集成验证

| 检查 | 结果 |
|---|---|
| Go 环境 | `go version go1.27.3 windows/amd64`，`go env GOROOT=F:\tools\go1.27.3\go`；用户 PATH 已移除 1.27.2 并加入 1.27.3，旧目录保留回退。宿主旧进程通过每条命令显式继承新用户环境 |
| `go test -p 1 -tags=unit ./internal/middleware ./internal/server/routes ./internal/server ./internal/pkg/ip ./internal/handler/admin ./internal/handler -count=1` | **退出 0**，6 包全部通过 |
| `go build -p 1 -o bin/server.exe ./cmd/server` | **退出 0** |
| `go test -p 1 -tags=unit -count=1 ./...` | **退出 1**：仅 `internal/repository` 的 3 条 `TestPgDumper*` 失败，均因本机 Go 测试环境无 `sh`；该测试文件本轮未改，属已记录 Windows 环境限制。其余已输出包通过，无新增 flaky |
| `go test -p 1 -tags=unit ./internal/handler -run TestOpenAIChatCompletionsLockedPricingMissingRejectedBeforeUpstream -count=1 -v` | **退出 0**，5/5 子用例通过，缺价仍在上游前拒绝 |
| `git diff --check` | **退出 0**，无空白错误 |
| Git Bash `deploy/tests/docker-compose-security-test.sh` | **退出 0** |
| Python/YAML 解析两个 Compose 并比较固定网络 | **退出 0**，subnet/gateway 与契约一致 |
| SMTP 修复前复现 → 修复后通过 → 删除赋值变异失败 → 恢复通过 | 全部实测：修复前 PUT false，修复后 true/false 两组均正确 |

## 未验证边界

- 本机无 Docker 命令，未执行真实 `docker compose config`、容器启停、network/container inspect、Caddy/DNAT、真实 Redis 或防火墙链路。应用代理桶回归使用生产代码与 miniredis，静态 YAML/Shell 检查不能替代真实部署。
- 未运行真实 PostgreSQL/Redis integration，也未部署生产。两步上线仍须先保持兼容开关 true，再同步切换 env 与后台 DB 为 false。
- 本轮没有修改前端、ent、wire 或迁移，未宣称重新运行前端套件或生成物校验。
- 未处理本评审之外的 F-04/F-05/F-06、生产数据清理或 EdgeOne 真实网段填写。
