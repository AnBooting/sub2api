# 交接文档：Tranche C 会话快照（2026-10-13，停止点）

> 本文件是当前会话的收尾快照，供下一会话无痕继续。事实以 Git、源码与 F:\sub2--evidence\trancheC\ 原始日志为准。
> 集成分支：F:\sub2--wt--ts02-policy @ 989821397（feat/TS-02-policy-retry-budget），所有改动已提交本地，未 push、未合并 develop、未部署。

## 1. 本轮完成并已提交

| 分支 / 工作树 | 提交 | 说明 | 状态 |
|---|---|---|---|
| feat/FE-budget-settings (F:/sub2--wt--fe-budget) | 53cbfb4aa | 管理页补 gateway_max_attempts_per_request / gateway_request_budget_seconds + zh/en i18n | 已提交 |
| feat/S8-scheduling-strategy (F:/sub2--wt--s8) | 96976cc42 + fa072c1ff | C-4 三策略配置骨架 + 迁移 245 + 缓存版本 25→26 | **已独立复审 Approve** |
| feat/C1-shared-lock (F:/sub2--wt--c1-shared) | c977cd8f9 → 7aaaa8292 → 1efafa82c | Anthropic Messages 共享入口锁价；F1-F5 全修 | **已独立复审 Approve**（1efafa82c） |
| feat/C1-shared-lock-cc-gemini (F:/sub2--wt--c1-cc-gemini) | 87906721a + bca4734de | ChatCompletions(两分支)/Gemini/Responses 锁价 + F6/F7 thinking 同源修复 | 已提交（C1 复审后 bca4734de 尚未二审） |

集成分支 989821397 已含上述四分支，且：gofmt 空、go build ./... 0、go vet -tags=unit ./... 0、go test -p 2 -tags=unit ./... 0（零 FAIL）。证据：F:\sub2--evidence\trancheC\final-*.log。

## 2. 审阅与已修问题

- S8 独立复审 Approve（含 MEDIUM 映射方向、LOW 两处，已在 fa072c1ff 修复）。
- C1 共享锁价独立复审 Approve（1efafa82c，F1 Antigravity thinking、F2 fallback 候选组、F3 warmup 豁免、F4 response_model 守卫、F5 AccountTypeUpstream 透传 + websearch 剥离）。
- bca4734de 的 F6/F7：
  * F6（Gemini 原生 thinkingLevel）—— Lead 用探针实测确认并修复，但 handler 级回归用例未合并（当前是同文件 TODO 注释，见 gateway_locked_thinking_parity_test.go 头部）。
  * F7（ChatCompletions / Responses 经 Antigravity 兼容链的 reasoning.effort=medium + 裸 gemini 名）—— 探针实测修复前 ledger=0、修复后 ledger=1，且新增 4 条正式回归用例已全绿（TestSharedChatCompletionsLockedPricingReasoningEffortMatchesOutbound / TestSharedResponsesLockedPricingReasoningEffortMatchesOutbound）。

## 3. 下一步（下一会话继续）

1) **对 bca4734de 做独立复审**（当前只有 Lead 亲验，未被 reviewer-c1-shared 终审）。重点：F6/F7 的冻结侧是否与出站逐字同源；为何 Gemini 原生 handler 级回归用例未合并；以及既有断言是否被改动。
2) 补 F6 的 Gemini 原生 handler 级回归用例（当前是 TODO）。
3) 将 bca4734de 合入集成分支后重跑最终全量 gate（当前集成分支只到 989821397，尚未包含 bca4734de 之后的 F6/F7 提交——注意：我已把 bca4734de 合并进 ts02-policy？请核对：上面 merge 记录 feature/C1-shared-lock-cc-gemini => 0，最后提交 989821397 就是 merge。因此集成分支已含 bca4734de，不需再合。但上述最终 gate 是在 989821397 上跑的，已包含。）
4) 之后推进 S6 组循环（依赖 C1 四入口，现已全部就绪）→ S7 C-6 落库 → S9 HA。
5) integration/E2E 仍需本机 PostgreSQL + Redis（当前无 Docker/PG/Redis，未跑 go test -tags=integration）。

## 4. 明确边界

- 未 push、未合并 develop、未部署；所有工作保留在本地特性分支与集成分支。
- integration 未跑；单测与探针覆盖见 evidence/trancheC。前置验收以 unit 层为准。
- 本轮遇到多次 teammate 轮次被宿主停止，我作为 Lead 已接管关键路径（F6/F7 直接写码）。下一会话建议：能用 Lead 直接收口的小修复就不派长时 teammate；复用现有 14 个成员，不再新建。
- 暂无临时文件/探针留在工作树（zz_*.go 已删除；git status 干净）。