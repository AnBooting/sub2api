# PR #19 评审汇总（Lead 裁决）

- PR：https://github.com/AnBooting/sub2api/pull/19 「fix(security): 收敛用户响应字段并阻止 SMTP 未配置时发码」（作者 Akanyi）
- head：`dev/security-public-contracts-smtp-20261108` @ `849c14a7bf24c6b69582a3db6b29fb3f5be859d7`；base：`develop` @ `4a0064318`（PR #18 合并后的基线）
- 规模：3 提交 / 20 文件 / +426 −108
- 评审方式：两名新团队成员只读评审（`pr19-contract-reviewer` / `pr19-smtp-reviewer`），在独立 worktree `F:\sub2\.agents\worktrees\pr19-review-20261108` 上核对；Lead 独立复跑验证与变异。
- 证据：同目录 [REVIEW-CONTRACT.md](REVIEW-CONTRACT.md)（F-05/F-04，18.5 KB）、[REVIEW-SMTP.md](REVIEW-SMTP.md)（I-01b，26.7 KB）

## 裁决

**Approve（建议合入）**：0 Critical、0 Required。共 5 Nit、5 Optional、7 FYI，均为可后续收口项，不阻断。

| 范围 | 结论 | 关键依据 |
|---|---|---|
| F-05 `/groups/available` 用户面字段收敛 | **Pass** | 白名单 15/15 与 FIELD-INVENTORY §2.5 逐字一致；`api_key_handler.go` 仅 1 hunk；`dto.Group` 与 6 处内嵌消费方零改动；契约测试仍是 `require.JSONEq` 精确整包比对，比原版更严 |
| F-04 监控快照 `config` 白名单 | **Pass** | 用户域 config 严格只留 `refresh_interval_seconds`；admin 快照与 `/admin/channel-monitor-v2/config` 未变；其余 5 端点代码路径未改；`errors[].ignored` 仍服务端计算 |
| I-01b SMTP 未配置拒绝发码 | **Pass** | 同步/异步都在读写验证码缓存与入队之前拒绝，复用既有 `ErrEmailNotConfigured`（503 + 稳定 reason）；判据与 PR18 管理端 `smtp_configured` 同源 |

### 需要作者考虑的非阻断项

| 级别 | 条目 | 建议 |
|---|---|---|
| Nit | 用户看到英文内部短语 `email service not configured`；前端无 `EMAIL_NOT_CONFIGURED` 的 zh/en 映射（`EmailVerifyView.vue:785-790` 只特判域名配额） | 补 i18n 与可行动文案，或确认前端既有阻断已足够 |
| Nit | `frontend/src/api/channelMonitorV2.ts:201` 注释仍引用已不再下发的 `config.ignored_error_categories` | 改注释 |
| Nit | `service/channel_monitor_v2_test.go:511` 测试名 `TestSnapshotRedactsPublicConfigPolicyFields` 已名不副实 | 改名 |
| Nit | `IsConfigured` 的 reason 第二返回值在发码路径被 `_` 丢弃 | 用 reason 构造更具体文案，或去掉注释里的承诺 |
| Nit | 异步「配置齐全」用例的 `cache.reads/writes==0` 断言空转（测试未启动 worker） | 删除或改为真实断言 |
| Optional | 缺 HTTP 入口级 503 + reason 契约测试（本项目有「探针与入口无绑定」教训） | 建议补 |
| Optional | 缺「未配 → 配好 → 再清空」中途变化测试（每请求读 DB 已由 `setting_repo.go:85-99` 证明） | 建议补 |
| Optional | 缺前置错误优先级回归（注册关闭 / 邮箱已存在 不应被 SMTP 检查抢占） | 建议补 |
| Optional | SPEC 要求的其余 5 端点键集合断言未落地（代码未改，静态核对通过） | backlog |
| FYI | OAuth pending 与邮箱绑定发码仍先写码+冷却再失败；`/user/totp/send-code` 未配置仍返回 200；判据是存在性而非可投递 | 既有缺陷，下一轮收口，不属本 PR 范围 |

**事实更正（评审过程中的一次误判）**：Lead 曾怀疑「删除 `emailService == nil` 守卫会导致 nil 解引用 panic」。经源码核对**不成立**：`EmailService.IsConfigured` 首行即 `if s == nil || s.settingRepo == nil`（`email_service.go:206-209`），nil-receiver 安全；旧基线也只有同步函数带该守卫，异步本来没有。新增 `missing_service` 子用例显式传 nil 且 PASS。

## Lead 独立验证（worktree @ 849c14a7b，Go 1.27.3）

| 检查 | 结果 |
|---|---|
| `go build -p 1 -o bin/server.exe ./cmd/server` | **退出 0** |
| `go test -p 1 -tags=unit ./internal/handler ./internal/handler/dto ./internal/server ./internal/server/routes ./internal/middleware ./internal/service -count=1` | **退出 0**（6 包全 ok；handler 218.6s / service 230.9s） |
| `go test -p 1 -tags=unit -count=1 ./...`（全量） | **退出 1**：仅 `internal/repository` 3 条 `TestPgDumper*` 失败，均因本机测试环境无 `sh`（该文件未在本 PR 改动，与 PR #18 轮次同因）。其余输出包全部 ok |
| 前端 `corepack pnpm@9 exec vue-tsc -b` | **退出 0** |
| 前端 `check:i18n` | **退出 0** |
| 前端 `vitest run` | **退出 0**：435 files / 3456 tests 通过（139.5s） |
| 前端 `vite build` | **退出 0**（built in 54.2s） |
| worktree `git status --porcelain`（评审前后） | 空 |

## Lead 变异验证（自行执行，均恢复后转绿）

| 变异 | 期望 | 实测 |
|---|---|---|
| 删除 handler 的 `if !admin { SnapshotPublic ... }` 分支（`channel_monitor_v2_handler.go:110-118`） | 用户域 config 契约红 | **红**：`TestChannelMonitorV2SnapshotConfigResponseContract/user` 报 `should have 1 item(s), but has 8`；恢复后 ok |
| 删除异步 `SendVerifyCodeAsync` 的 SMTP 守卫（`auth_service.go:371-374`） | SMTP 未配置用例红 | **红**：`go test ./internal/service -run SMTP` 退出 1，日志仍 `Enqueueing verify code`；恢复后 ok |
| 删除 `UserFacingGroup` 的 `AllowHighAvailability` 字段 | 白名单契约红 | 编译期即红（`unknown field`），说明契约测试与字段强绑定 |

## 未验证边界

- 无 `docker`：未跑 `docker compose config`、容器启停、真实网络链路——本 PR 未改 deploy。
- 无 PostgreSQL/Redis：未跑 `-tags=integration`。
- 未跑 `golangci-lint` / `govulncheck`（需 CI 或本机安装；本轮未执行，不声称通过）。
- 未在真实浏览器验证前端页面（类型检查 + 单测 + 构建通过，但无 UI 实机回归）。

## 观察（与本 PR 无关，供后续）

用户面 `PUT/DELETE /api/v1/keys/:id/ha-groups/:groupID` 的 `writeHAEnablementResponse`（`api_key_handler.go:473-480`）仍下发全量 50 字段 group，属 F-05 声明范围之外的既有泄露面，建议记 backlog。
