# 测试站上线前审计：验证证据（PR #22）

> 2026-11-11，独立审计轮（Lead 独立完成；成员委派通道经 7 次实测崩溃后弃用）。
> 本文件自包含：结论、逐项证据（命令 + 退出码）、未修项与理由。

> 2026-11-12 复审修正：原 N-03 对 Responses 的 `input` 误设为必填；F2 的处理与本轮验证见文末 `5。下列首轮测试结果是历史记录，不能代替本次 head 的验证结果。

## 1. 结论

| 项 | 结果 |
|---|---|
| 交接文档定性未修缺陷 | N-03 / I-02 / R-01 已修复并变异验证；D-3 核实已在当前源码修复（无需改动） |
| 已合并整改的真实性 | F-01/F-01b/F-03/F-05/F-04/F-07、I-01b、N-02、D-02 逐项在当前源码复核存在 |
| 本地门禁 | `pre-push-check.ps1 -Mode full` **12/12 通过（1 项 WARN=已知环境失败），退出码 0** |
| lint | golangci-lint v2.14.0（touched 包定向）**0 issues**（修复前 14 issues） |
| 前端 | vue-tsc / check:i18n / vitest(3457) / vite build 全绿；15 处 v-html 全部核验安全 |
| deploy 安全 | 无真实凭据入库；compose 加固到位；EdgeOne 网段等 3 项人工事项保持待办 |

## 2. 逐项证据

### N-03 空 messages/input 计费（PR #22 commit 1）

- 根因：网关 6 个入口均无 `messages`/`input` 非空校验，`messages: []` 被转发上游并按 ~35 prompt tokens 计费（线上复现 4 次，交接文档 §3.4）。
- 修复：`backend/internal/handler/gateway_payload_guard.go`；接线：gateway_handler.go、gateway_handler_chat_completions.go、gateway_handler_responses.go、openai_chat_completions.go、openai_gateway_handler.go（各 1-2 行）。
- 测试：`gateway_payload_guard_test.go` 六入口 ×（缺字段/空数组/非数组/空串/全空白串）断言 400 + `upstream.callList()` 空 + `ledger.commands` 空。
- 变异验证：守卫改为恒放行 → `go test -tags=unit -run TestGatewayEmpty ./internal/handler/` **7/7 子测试红**，响应体为 `upstream_error`（证明请求到达上游）；恢复后全绿。
- 连带修正：`validClaudeCodeBodyJSON()` 补 `messages`（原探针体不满足 Anthropic 契约，导致 `TestSharedMessagesNormalGroupClaudeCodePolicy/restricted-client` 在修复后失败——按「不放宽护栏、修夹具」处理）。

### I-02 /setup/status 硬编码（commit 2）

- 修复：`server/routes/common.go` 改读 `setup.NeedsSetup()`（配置/安装锁文件判定，与 setup 向导同源）；只暴露布尔 + step 枚举。
- 测试：`common_setup_status_test.go` 4 场景（fresh→true/welcome；config 文件→false/completed；install lock→false/completed；SKIP_SETUP→false/completed）。
- 变异验证：布尔硬编码 false → `TestSetupStatusReportsRealInstallState/fresh_data_dir_needs_setup` 红；恢复后绿。

### R-01 service 层 429 缺 Retry-After（commit 3）

- 修复：`ApplicationError.WithRetryAfter` + `errors.RetryAfter` 访问器（internal/pkg/errors/errors.go）；`response.ErrorFrom` 统一落 `Retry-After` 头；`redeem_service.go` 附带 600s 上界提示（窗口常量在 repository，反向依赖无法共享，已注释说明）。
- 测试：errors 元数据往返/包裹错误/非法值；response 落头/不落头；service 限额命中携带正值。
- 变异验证：丢元数据 → service 测试红；头值 +1 → response 测试红。

### D-3 GetTrustedClientIP 空身份

- 核实：`internal/pkg/ip/ip.go:297-308` 已回落 `unknownClientIP` 并带说明注释（交接时点后已在 PR #18 修复）。未改动。

### 已合并整改真实性复核（对抗「审查者读到过期工作树」纪律）

- F-01 信任反转：ip.go GetClientIP/GetTrustedClientIP 信任方向 + 未知回落 ✓
- F-01b 账号维度限流：middleware/rate_limiter.go `AccountKey`/`FailureOnlyAccount`（注释点名 account lockout DoS 设计）✓
- F-03 保留域：24 处命中 ✓；F-05/F-04：dto/group_user_facing.go + channel_monitor_v2_public.go ✓
- F-07 越权同码同体：usage_handler.go:397-408（注释完整）✓；I-01b：auth_service.go:332 ✓；N-02：usage_stats_incomplete ✓；D-02：setting_public.go injectedVersion + Dockerfile 单一 ldflags ✓

### lint 债务（commit 4）

- 修复前 14 issues（errcheck 8 / gofmt 1 / unused 5）→ 修复后定向复跑 **0 issues**。
- 注意：首跑超时产生的 `advanceToBoundary` unused 为**构建标签误报**（定义在无标签文件、唯一调用方在 unit 标签文件），按移动修复而非删除；`usageCleanupWindowEnd` 全仓零调用，真死代码，删除。

### 全量套件与已知失败

- 全量 `go test -tags=unit -count=1 ./...` 失败清单 = 恰好 3 条 TestPgDumper*（Windows 无 sh，AGENTS.md §E 已记载）。
- 已知满载 flaky（`TestInflightEstimate_AccountMappingNoDBAndBoundedMemory`、`TestOpenAICompatibleScannerRequestContract`）隔离 `-count=3` → 双双通过。首次门禁 FAIL 即满载并发触发该 flaky；无并发复跑门禁后 WARN 分类正确命中。
- 前端：vue-tsc -b → 0；check:i18n → 0；vitest run → 435 files / 3457 tests 全过；vite build → 0。

### deploy / 凭据扫描

- git ls-files 仅 `deploy/.env.example` 被跟踪（.env 已忽略）；API key/私钥模式全仓扫描 clean。
- 生产 compose：no-new-privileges、固定 172.30.18.0/29 + 网关 /32 信任、DB/Redis 不暴露端口 ✓；Caddyfile.edgeone 占位符与信任链顺序与交接文档一致 ✓。
- 人工事项（非代码）：EdgeOne 回源网段填写、两步上线、测试数据清理 —— 见 ACTION-ITEMS.md A/B/C 节。

## 3. 本轮刻意未修（与理由）

| 项 | 理由 |
|---|---|
| F-06 未知模型 404 细分 | 曾整体回退：单行改动会破坏「缺价不得放行」红线；须单独 PR 且严格分离路由判定与缺价判定（交接文档 §3.2 硬要求） |
| P3-01 分页越界 400 | 92 个端点的契约变更，需前端同步，不宜混入上线前修复轮 |
| O-01 PUT /user 冷路径 5.0s | 交接文档明确「取证前不改代码」；需测试站运行时证据 |
| D-2 账号锁定残余 DoS | 需产品决策（锁定期内正确凭据是否放行） |
| PR #7 前端重设计 | CLOSED 未合入，需独立 rebase 轮次 |
| composite 深层计费一致性 | 到设计边界确认（S5 边界有意为之 + 冻结束完善 + 既有测试矩阵绿）；未发现新缺陷 |

## 4. 委派通道记录

spawn_teammate ×3（首轮+唤醒各一轮）与一次性 subagent 探测共 7 次，全部「failed before finished / 无收尾消息」，判定运行时故障；经用户确认额度后改为独立完成，三个残留成员保持休眠。

## 5. 2026-11-12 处理外部 review F2

- 复现来源：用户提供的 `E:\Downloads\REVIEW.md`，审核 head 为 `ac49e0b80`。
- F2 确认成立。先添加 `TestOpenAIResponsesInstructionsOnly`，真实 HTTP handler 返回 400、上游零调用；退出码 1。官方 Responses Create 契约允许可选 `input`，参见 https://developers.openai.com/api/reference/resources/responses/methods/create 。
- 修复：输入缺省/null/空字符串/空数组时，非空的 instructions、previous_response_id、conversation（字符串或带 id 的对象）、prompt.id 都可以提供上下文；仅在确实没有输入或上下文时拒绝。显式非法 input 类型仍拒绝。Messages/Chat 的非空数组规则不变。
- 回归全部直接调用生产 HTTP handler：8 种合法上下文场景均断言 200、上游一次、账本一笔；空白/无效上下文拒绝；额外覆盖「instructions-only 缺价」与「其他用户 previous_response_id」仍在上游前失败。
- 变异验证：移除其他上下文分支后，instructions-only 与其余 7 场景全部按原缺陷失败；恢复修复后定向套件退出 0。

### 本次 head 验证结果

- `go test -tags=unit -count=1 ./internal/handler/`：退出码 0；使用 JSON 结果另行保存完整用例事件。
- `golangci-lint run --timeout 5m --new-from-rev ac49e0b80 ./internal/handler/...`：0 issues，退出码 0。
- 后端构建通过。`pre-push-check.ps1 -Scope backend -Mode full` 本轮退出 1，不能沿用首轮的全绿结论：全量单测失败为 3 条 TestPgDumper*（本机缺少 sh）以及 3 条 TestAliyunCaptchaVerifier*（本机假端点 2s 超时）。
- 验证码三条在后端未经本轮改动的 #23 worktree 同样失败；隔离运行、设置本机代理旁路与移除进程代理环境后仍失败。当前仅能证明是两条分支共有的基线失败，尚未定位根因；未放宽测试或改动验证码业务代码。
- 没有运行 PostgreSQL/Redis 集成环境、真实模型计费或部署验收；本轮仅修复 review 指出的 Responses 契约回归。
