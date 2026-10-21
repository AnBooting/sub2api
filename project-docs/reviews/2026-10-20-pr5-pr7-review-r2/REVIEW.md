# PR #5 / #7 第二次复审

审查日期：2026-10-20（Asia/Singapore）。

- PR #5：https://github.com/AnBooting/sub2api/pull/5，head `e417c3508856400888f0581dfeb46006fd3b4ecf`。
- PR #7：https://github.com/AnBooting/sub2api/pull/7，head `d52241fdbf7da8b42d6a6e180e5d5c55865a19a0`。
- 两条 PR base 均为 `d5c2b5edbcb0335cb80e4cfd7371098cf8982d8c`。

上次三个问题均已修复，原交付测试全部通过。继续检查发现 PR #5 的新 Live 结算仍存在三项问题，建议修复后再合并。PR #7 本轮未发现新的阻塞问题。

## R1 — [P1] Live 扣费未累计密钥限额

位置：`backend/internal/service/openai_live.go:906-924`。

`settleLiveCallUsage` 只构造包含 ID 的 APIKey，未传递 Quota、RateLimit5h/1d/7d，且 postUsageBillingParams.APIKeyService 为空。
统一计费的 `shouldDeductAPIKeyQuota` 和 `shouldUpdateRateLimits` 依赖这些字段，所以命令中的 APIKeyQuotaCost 与 APIKeyRateLimitCost 始终为 0。
仓储只在这两个金额大于 0 时累计密钥额度和窗口用量。结果是余额/订阅确实扣了，有限额的密钥却不消耗限额，可继续超出配置预算使用 Live。

复现：真实 finalizer 输出 BalanceCost≈0.20，两个密钥计量字段均为 0。相同 ID、配置总额度和三个窗口额度的密钥经过现有生产 builder，两个字段均为≈0.20；比较测试失败。
该比较未走真实鉴权和数据库；缺失字段及仓储条件已逐段核对。

应保留必要的计费策略快照或在结算时加载策略，传递 APIKeyService，并验证 DB 与鉴权缓存的更新。另一个同源遗漏是 Platform 未传递，标准余额模式的 user×platform 配额累加也会被跳过；该点本轮仅源码确认，未单独做数据库复现。

## R2 — [P1] 简易模式仍会实际扣款

位置：`backend/internal/service/openai_live.go:917-924`。

普通 OpenAI RecordUsage 在 RunModeSimple 下跳过财务扣费；启用简易模式的 Key 窗口功能时也只计窗口，不扣用户余额或订阅。
新的 Live 结算直接调用 applyUsageBilling，没有该模式分支，也未设置 SimpleModeKeyRateLimitOnly。

可达条件：已有 OpenAI/Composite 分组保留 AllowLive 和实时单价，例如标准模式切换到简易模式。鉴权的简易模式分支仍保留分组，Live handler 没有拒绝该运行模式，余额准入也被简易模式跳过。

复现：设置 svc.cfg.RunMode=RunModeSimple，调用真实 finalizer，仍输出 BalanceCost=0.200000。
测试还检查了带订阅 ID 的 finalizer 输入也会生成订阅扣费；正常简易模式鉴权不加载订阅，因此线上主要风险是余额被扣成负数。

应与普通网关沿用相同的简易模式计费语义；若不支持 Live，应在请求上游前明确拒绝。

## R3 — [P1] 临时扣费失败后，关闭标记阻断再次结算

位置：`backend/internal/service/openai_live.go:881-889`，关联 `:808-811` 的 MarkLiveCallClosed。

finalizeLiveCall 在调用财务仓储前已永久设置关闭标记。新增失败分支只告警并将 ActualCost 清零后写日志，既没有持久待结算任务，也没有恢复可重试状态。
数据库临时错误恢复后，同一会话再次 finalize 会因 first=false 提前返回；统一账本的 request_id 幂等性因此没有机会用于重试，已消耗的上游费用不会自动补扣。

复现：第一次 Apply 返回 temporary database outage；清除错误后再次调用真实 finalizer，Apply 总调用次数仍为 1，账单 ActualCost=0。

应将会话结束与财务结算状态分开，在调用账本前持久化冻结金额与待结算事实，失败可重试；成功后再标记已结算。重试金额应固定，不能随再次调用的时间增长。仅写零费用日志供人工对账不能替代自动补扣状态。

## 验证

- 原交付测试重跑：余额/订阅实际结算、panic 最终状态、真实 LoginView 离场导航，全部通过。
- PR #7：类型检查退出 0；新增路由出口 2 项与路由守卫 36 项通过；本轮未发现新的阻塞问题。
- PR #5：相关 Live 结算、计价、Logger、panic 日志和简易模式既有测试通过。
- 新增生产路径复现：简易模式财务扣款、失败后再次 finalize、密钥限额命令比较，均得到上述失败结果。
- 未运行全量套件、真实数据库/生产联调或收费上游请求。
- 未修改业务工作区源码、提交、推送、合并或部署；本轮只拉取提交、使用临时源码快照审查。
