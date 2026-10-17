# 交接文档：新会话继续 TS-02（2026-10-12 20:15）

> 新会话先读本文件和 `AGENTS.md`，再按第 6 节开场。
> 本文件是快照；事实以 Git、源码和 `F:\sub2--evidence\20261012-ts02\` 原始日志为准；开工前用第 5 节复合命令复核。

## 1. 里程碑（本会话已提交 4 个切片）

| 提交 | 分支 / 工作树 | 内容 |
|---|---|---|
| `23a4e24b8` | feat/TS-02-policy-retry-budget | 合并 TS-01（compact 锁价 `b25d002f3`）进 ts02 worktree，无冲突 |
| `d0ba74ea9` | feat/TS-02-policy-retry-budget | **S4** 16/600 配置化（settings 键 + 管理 API 扁平字段 + 进程内缓存 + `GatewayRequestAttemptBudgetLimits` 访问器） |
| `217deda1b` | feat/TS-02-policy-retry-budget | **S2+S3** 发送钩子接线（13 个 service 文件 19 处发送点）+ 按契约拦截 + FailoverState/E7 决策接入 + 退避钳制 |
| `7fb07a7c4` | **feat/TS-01-request-locked-pricing**（F:\sub2） | **A4** plaza 展示价定时定点修复（ContextPricingScheduleInput 增 PricingAt +标准价剥壳 standardPriceResolved） |

任务板全 complete：task-1=A1、task-2=A3、task-3=A4、task-4=A2。每切片经独立只读审 Approve。

## 2. 已验证（证据原文在 F:\sub2--evidence\20261012-ts02\）

| 验证 | 结果 | 日志 |
|---|---|---|
| ts02 最终 build / vet | exit 0 | final-build-v1.log / final-vet-v1.log |
| ts02 全量 unit（含全部 4 切片） | exit 0，全包 ok | final-fullunit-v1.log |
| handler/admin+handler+service 三包 | exit 0（service 附 2 个 flaky） | post-guard-pkg-v1.log |
| flaky 归因子（TestInflightEstimate…Memory / TestFilterGrokFreeQuota…OAuth） | 隔离复跑 PASS，exit 0；两次运行间 service 包源码零改动 → 高负载 flaky | flaky-rerun-v1.log |
| 基线对照（无本会话改动的 23a4e24b8） | 全量 service exit 0 | attr-baseline-fullpkg-v2.log |
| A4 在 F:\sub2 的 build/vet/定向 | 全绿 exit 0 | a4-build-v2 / a4-vet-v1 / a4-directed-v2 |
| compact 锁价与本切片合并后强度 | openai_responses_locked_pricing_test.go:399-408 硬不变量未被放宽 | 同 final-fullunit-v1 |

### 已知行为变化（写进 S2+S3 提交说明）
- `OpenAI /v1/responses` 挂 16/600 预算（默认从 S1 常量；配置了 settings 覆盖后走配置）。预算耗尽 / 超窗 → 503 `no_available_group` + WARN（budget_used/budget_max/budget_deadline）。
- 预算被挡下的账号不记健康失败、不记调度结果、不记上游错误（账号无责）。
- 退避钳制生效，所余时间不足时立刻终止（不同于换号的 sleep）。

## 3. 未达成的边界（必须说清）

1. **只有 OpenAI E7 真的装了预算**。Anthropic（E1-E6）、Gemini/Antigravity/Bedrock/SystemOne、以及 OpenAI 的其他 12 输出发送点，现在仍是 nil 预算 = 不计数不拦截。这 13 个文件的钩子接线已完成但惰性，等 S3b 在各 handler 挂预算后才生效。
2. **OpenAI WS /responses（GET 长连接，handler :2480）没挂预算**——它经 openai_ws_http_bridge.go:552 → doOpenAIUpstream 真实发送，但 handler 未调 EnsureRequestAttemptBudget。属 C-5 已知缺口，等 §8.1 #1 的 WS 预算归属决策。
3. **前端管理页没动**：`gateway_max_attempts_per_request` / `gateway_request_budget_seconds` 只有 API，无 UI。登记为后续小切片（`frontend/src/views/admin/SettingsView.vue` 改动面大）。
4. **PUT /admin/settings 无契约测试**（api_contract_test.go 只覆盖 GET）；PUT 行为仅由新增的 handler 测试覆盖。

## 4. 下一步（按原计划的 Tranche B）

依赖链：S2+S3 + S4 已并入 ts02（`217deda1b` 头），A4 已在 TS-01 主线上。

1. **S3b**：预算挂载/决策检查补到 E1-E6（Anthropic FailoverState 的 4 个 handler——gateway_handler.go / gateway_handler_responses.go / gateway_handler_chat_completions.go / gemini_v1beta_handler.go / gateway_systemone.go）与 E8-E12（openai_gateway_handler.go :1201 / openai_chat_completions.go / WS / images / embeddings / alpha-search / grok_media / codex models）。helper 复用，工作量主要在 handler。
2. **`TS-02 S5 分类器`**：在 UpstreamFailoverError 出口加 `FailureClass` 五分类 + `Classify()`。**门禁：§8.1 六项定稿**。
3. **C1 消费者续推**：native-anthropic wire 测试 → 媒体消费者（S5）→ WS 锁价（S4，与 S5 同批验收）。**门禁：§8.1 #1/#3/#4**。
4. **TS-02 S6 组循环**：`NormalGroupIDs()` 驱动外层组循环 + 换组重查权限/订阅/额度。**门禁：E1/E2/E3 先接锁价（task-7）**。
5. **C-6 落库**（S7）：failover/attempt_count/requested_group_id/served_group_id 写入 RecordUsage。

### §8.1 六个产品契约开放点（**未定**，仍等用户拍板；建议见计划）

| # | 决策点 | 我的建议 |
|---|---|---|
| 1 | WS 预算归属：每 turn 独立 16/600 vs 每连接累计 | 每 turn 独立；连接内重连预算 5s 与 600s 取小 |
| 2 | 客户端更早 deadline 读取面 | 本期不实现；只在 start+600s |
| 3 | keepalive/ping 与 C-5:102「累计首字节不重放」的冲突 | 以现有实现为准：keepalive 不计入"已提交"，把 C-5:102 改为"不含 keepalive 心跳"写回契约 |
| 4 | 媒体入口是否纳入 16 次 | 不纳入；媒体 handler 不挂预算（与 D1 天然一致），按 PRD-06 单独处理 |
| 5 | SystemOne / Gemini 是否参与组链 | Gemini 参与（已有更小上限）；SystemOne 不参与（PRD-02 未列） |
| 6 | attempt_count 口径 | 含 service 隐藏重试（即 budget.Used()） |

### §8.2 本会话己拍板的新增决策
- D5：预算耗尽复用 `no_available_group`（503），不新增 C-7 码。
- D6：不加"只计不拦"开关；S2 天然只计不拦，S3 直接按契约拦截。
- D7：S4 不含前端管理页字段。

## 5. 开工前复核命令

```powershell
cd F:\sub2
git branch --show-current    # feat/TS-01-request-locked-pricing
git log --oneline -2          # 7fb07a7c4（A4 plaza）→ b25d002f3（compact）
git worktree list             # 4 个：sub2 / __wt--artex-defense / __wt--ts02-policy / __wt--ts02-ui

cd F:\sub2--wt--ts02-policy
git log --oneline -4          # 217deda1b（S2+S3）→ d0ba74ea9（S4）→ 23a4e24b8（合并）→ b25d002f3
git branch --show-current     # feat/TS-02-policy-retry-budget

$env:GOROOT='F:/tools/go1.27.2/go'; $env:GOPATH='F:/tools/gopath'; $env:GOTOOLCHAIN='local'
$env:GOPROXY='https://goproxy.cn,direct'
$env:PATH='F:/tools/go1.27.2/go/bin;E:/Git/bin;E:/Git/usr/bin;'+$env:PATH
cd F:\sub2--wt--ts02-policy\backend
go build ./...                # 应 exit 0
go test -tags=unit ./internal/handler -run 'TestOpenAIResponsesLockedPricing' -count=1  # 应全绿，含 budget.Used()==2
```

## 6. 工程流程教训（本次实际踩过的）

| 教训 | 证据 |
|---|---|
| 同一工作树的两个子代理并行写时，中间态会让另一个的验证不可信（A3 中途写 setting_update.go 导致 A1 的定向测试编译失败） | a13-directed-v1.log |
| **repo 里 `parseSettings` 会写 xai 进程级全局态**；新增 _test.go 文件的字母序会影响同包其他用例 | attr-pollution-proof.log / attr-pollution-fixed.log（修法已复用 account_wildcard_test.go 先例，加入 gateway_request_attempt_budget_settings_test.go 与 setting_handler_gateway_budget_test.go 双守卫） |
| 接线完整性测试的排除清单要按**本包**文件列（claude_usage_service.go 在 repository/ 不在 service/，按清单它会报"文件不存在"） | a13-directed-v1.log |
| A2 的写域漏了"它必然要改写的 A1 S2 断言文件"（预算从只计不拦改拦截后，5 个用例必然失败） | team-message-b68a3a27 发现 3，已扩 task-4 写域 |
| 预算钳制必须显式判 nil（`Remaining()` 对 nil 返回 0，直接 min 会把无预算请求的退避压成 0） | failover_loop.go:79-88 / gateway_forward.go:68-78 |
| 时序测试 **预算窗口至少 500ms**，否则会因机器铃断而假失败 | failover_loop_test.go 4 处放宽 |
| 预算耗尽**不**报账号健康失败/**不**报上游错误（账号从未被调用） | openai_gateway_handler.go:954-966 / :3606-3633 |

## 7. 红线（本轮接续，不得吞掉）

1. 不在 `main` 提交；不推送、不部署、不合并 develop。
2. 缺价模型必须在请求上游之前拒绝（503 `pricing_unavailable`）；不按零价放行，不用 GPT 底价。
3. 显式零价不能被跳过；nil 不算免费。
4. 不提交 `.env`、`data/`、凭据；不读真实配置里的秘密。
5. `F:\sub2\ARTEX` 只读：不运行、不扫描、不装依赖、不 `git add`、不 clean。
6. 不 stash / reset / clean 用户的脏工作树；不整文件覆盖上游共享文件。
7. 原始失败日志保留；没跑过的检查不说通过。
8. 不使用任何非本机的 SSH 构建机。
9. `F:\sub2` 里还有别的未跟踪/已修改文件（README.md、SKILL.md、tools/repo-*.mjs、project-skills/repository-navigation/、PR2-review-2026-10-12.md、.tmp-investigate-session.jsonl）——非本会话产物，一律不碰。

## 8. Teammate 进程状态（供参考）

budget-hook / settings-config / plaza-time / failover-decision / reviewer 都是本会话创建的 Agent Teammate，本会话结束后如需重新组建即可，不特殊。reviewer 进程在最终复核时崩溃过两次（infra），但不要误判为它的审查错了——它发出的四份 Approve 报告都在，且证据与其静态推导一致（全量绿 + 归因完整）。
