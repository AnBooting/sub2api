# PR #5 / #7 复审

日期：2026-10-20（Asia/Singapore）。两条 PR 当前均 OPEN，base 均为 `d5c2b5edbcb0335cb80e4cfd7371098cf8982d8c`。

- PR #5：https://github.com/AnBooting/sub2api/pull/5，head `d6a990f60b97b5d270382d8fddcda35ef0da8496`。
- PR #7：https://github.com/AnBooting/sub2api/pull/7，head `08924854d77c05b6d70bc29e66716cf496a61f58`。

结论：发现 2 个 P1 和 1 个 P2，建议修复后再合并。本次为审查，未修改业务源码、提交、推送或部署。

## R1 — [P1] PR #5 Live 账单写入费用，但没有财务结算

位置：`backend/internal/service/openai_live.go:866-878`，`finalizeLiveCall`。

新增逻辑计算并填入 TotalCost/ActualCost 后，仅调用 `writeUsageLogBestEffort`。
该函数只持久化用量日志，假定调用方已经完成扣费；这里未调用 `applyUsageBilling` 或等价结算流程。
因此配置实时语音价格的用户会看到非零账单，余额、订阅额度及相关扣费状态却不更新。

复现：调用真实 finalizer，单价 0.10/分钟、持续约 2 分钟；分别给余额和订阅模式注入扣费仓储探针。
两次均写出 ActualCost=0.200000，但 billing Apply=0、balance deduction=0、subscription increment=0。
应在写日志前接入可幂等的真实结算，并覆盖失败处理与缓存更新。当前测试仅核对最基本的结算调用，不能代替这些完整验收。

与基线的区别：基线明确记录零费用；本 PR 新增的是“有费用但不结算”的不一致。基线已有免费 Live 的策略问题不单独列为本轮新增缺陷。

## R2 — [P1] PR #7 多根节点页面离场后目标页不渲染

位置：`frontend/src/App.vue:170-173`。

新的 `Transition mode="out-in"` 直接包裹路由组件。LoginView 的根节点是 AuthLayout 与条件渲染的 TotpLoginModal，属于 Fragment。
真实 Vue 运行时警告 `Component inside <Transition> renders non-element root node that cannot be animated`。
离开该页面时 out-in 的离场过程无法正常完成，路由地址改变后目标组件仍未渲染；登录成功跳转和登录页导航因此可能卡住。

复现：从实际 App.vue 读取 RouterView 模板，挂载真实 LoginView，再 router.push 到简单目标页；断言目标页存在失败。
仅撤掉本 PR 新加的过渡、恢复 `<RouterView />` 后，同一测试通过；恢复受审原文后保留失败结果。
应给 Transition 一个稳定的单元素根节点，或仅对满足该约束的页面启用过渡，并覆盖实际登录导航。

## R3 — [P2] PR #5 panic 访问日志提前记录成 200

位置：`backend/internal/server/middleware/logger.go:26-36`。

Logger 新增 defer，但生产中 Recovery 位于 Logger 外层。panic 展开时，Logger 的 defer 先读取 writer.Status()，Recovery 随后才写入 500。
因此新增访问日志会把尚未写响应的 panic 请求记为 200，污染错误率和按 HTTP 状态聚合的监控。

复现：按生产相同的 Recovery → Logger 顺序挂载路由并触发 panic。最终 HTTP 响应=500，`http request completed` 日志 status_code=200。
应确保访问日志在 Recovery 写入最终状态之后记录，并在测试中同时断言响应与日志状态以及日志条数。
基线没有这条 panic 访问日志；本轮问题是新增日志错误地报告成功。

## 已修复项与验证边界

- PR #5 原 SVG 片段图标清空问题已通过旧复现测试 4/4；impact 的 Git 参数顺序已修正，前序继续审查的独立索引复验通过。
- PR #7 原侧栏宽度、外部文档链接、选中筛选底色三项旧复现测试，本轮重跑 3/3 通过；弹窗定位在上一段继续审查中已用浏览器确认恢复。
- 上一段继续审查记录：PR #5 前端相关 58 项、PR #7 前端相关 59 项通过，两者类型检查通过；PR #5 相关 Live/RefreshToken 测试、后端构建通过，PR #7 前端构建成功。
- 本轮重新运行新复现测试，确认上述三个问题；PR #7 路由问题还完成了移除新增过渡的对照验证。
- 未运行全量套件、真实数据库/生产联调或收费上游请求；以上通过项不代表完整合并验收。
