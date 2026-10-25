# AnBooting/sub2api PR #11 / #12 / #13 审查

审查日期：2026-10-25（Asia/Singapore）。**本轮未合并：发现一个影响三条 PR 的迁移历史验证缺口，以及 PR #12 的两个 Live 结算问题。** 新增三个负例均调用真实生产函数并失败，测试随 ZIP 交付。

## 本次审查范围

| PR | 审查 head | 处理 |
|---|---|---|
| [#11 上游同步](https://github.com/AnBooting/sub2api/pull/11) | `98296a89c9a7efd5b8f26073022a71d285e5cfb5` | 暂缓：共同迁移问题 |
| [#12 审计修复](https://github.com/AnBooting/sub2api/pull/12) | `ca323218cf55084a0757b60f61d2e0fb41f07dc2` | 暂缓：共同迁移问题、Live 自动重试与统计问题 |
| [#13 产品功能](https://github.com/AnBooting/sub2api/pull/13) | `a7ac8e389de852abe1f3335c86451a7f1cd5ccc6` | 暂缓：共同迁移问题；视频错误脱敏源码已修复 |

本轮从明确的团队仓库地址读取 GitHub 元数据，并 fetch 当前分支。取得的 `develop` 为 `4956d55982b134e615feb1e787e19846b1c9ddf0`；PR 元数据仍显示原始 base `d5c2b5edbcb0335cb80e4cfd7371098cf8982d8c`。以实际抓取的 Git 提交和本地试合并树检查集成结果。当前工作分支和已有交付文件保持原状。

审查重点是上一轮问题的修复增量、真实 observer/finalizer、迁移执行器及用户 DTO；没有把本轮称为 600 多个文件的重新逐行全面审计。

## 1. [P1] 重复对象错误不能证明旧迁移已完整执行

影响 #11 / #12 / #13，三条 head 中两个相关源码文件的 blob 完全一致。

位置：[migrations_runner.go:272](https://github.com/AnBooting/sub2api/blob/98296a89c9a7efd5b8f26073022a71d285e5cfb5/backend/internal/repository/migrations_runner.go#L272)、[migrations_renamed_replay.go:139](https://github.com/AnBooting/sub2api/blob/98296a89c9a7efd5b8f26073022a71d285e5cfb5/backend/internal/repository/migrations_renamed_replay.go#L139)。

新逻辑在事务执行失败并回滚后，仅根据新文件名白名单、当前源码的 checksum 和 SQLSTATE，就跳过整份迁移。它没有查询数据库是否存在对应旧文件名，也没有核对数据库中旧记录的 checksum；`oldName` 实际只用于日志。

例如 251 先添加 currency，再添加同名约束，最后才添加 `usage_logs.pricing_snapshot`。若已有 currency 约束，但没有可信的完整旧迁移记录（可能是部分修复、旧版内容不同或历史漂移），第二条 SQL 的 42710 会回滚整个事务，第三条 SQL 不会执行；执行器仍返回成功。检查当前源码等于预登记 hash，不能替代检查这台数据库实际应用过什么。

`TestReviewMigrationDuplicateWithoutVerifiedLegacyHistoryMustFail` 通过真实 `applyMigrationsFS` 复现：新迁移记录不存在、SQL 报重复约束，执行器告警后返回 **nil**，负例失败。此用例采用 sqlmock 注入执行错误，**没有连接真实 PostgreSQL，也不声称已发现生产数据库存在部分 DDL**。

建议在持有迁移锁时核对旧 filename 与数据库 checksum，仅对已证明完整应用的历史迁移做映射/跳过；缺记录或 hash 不一致应拒绝。补充完整旧库升级、缺旧记录、旧 hash 不匹配和重复运行验证。正常旧库重放不再直接报 42710 的正例已经通过，但不能因此认为历史验证已经完备。

共同源码 blob：`migrations_runner.go` 为 `27b8439c429674fd82c143c9641e1b236ed9fdcf`，`migrations_renamed_replay.go` 为 `dd0c250efe9d0182e9346adca3e709e21bcf7bc8`。

## 2. [P1] 检查点与账本同时失败时，observer 仍不启动自动重试

影响 #12。位置：[openai_live.go:814](https://github.com/AnBooting/sub2api/blob/ca323218cf55084a0757b60f61d2e0fb41f07dc2/backend/internal/service/openai_live.go#L814)。属于上一轮故障恢复问题修复不完整。

observer 已补上调用 `retryPendingLiveSettlement`，但该函数先读取 Redis，看到 `latest.Settlement == nil` 就返回。后面才提取调用方内存里的 frozen checkpoint、并处理“存储没有检查点但内存有”的情形，因此真正的检查点写失败无法走到该恢复分支。

`TestReviewLiveObserverRetriesWhenCheckpointAndLedgerFailTogether` 从真实 `observeLiveCall` 入口启动已到期会话，令 SaveLiveCall 失败、账本暂时失败。期望自动重试至少两次，实际 Apply 仅 **1 次**。直接手动再次调用 finalizer 的既有测试通过，并不能证明 observer 会主动恢复；当前仍依赖外部再次触发，随后短 TTL 清理会丢失待收费用。

建议先取得可用的内存冻结状态，再决定是否进入重试；Redis 回读/检查点暂时失败应纳入重试，不能先返回。覆盖从真实 observer 入口触发的两项依赖同时故障与恢复。

## 3. [P2] 失败审计行重复累计单次会话的费用与时长

影响 #12。位置：[openai_live.go:999](https://github.com/AnBooting/sub2api/blob/ca323218cf55084a0757b60f61d2e0fb41f07dc2/backend/internal/service/openai_live.go#L999)。这是使用独立 `:pending` request_id 的修复引入的新问题。

失败时复制完整 usageLog，仅将 request_id 加上 `:pending` 并清零 ActualCost，保留了 TotalCost、DurationMs 和请求类型。补扣成功再插入正式行，两条都进入正常 usage_logs 查询和聚合。生产汇总直接对 total_cost/duration_ms 求和，并未排除 pending 审计行。

`TestReviewLiveFailedAttemptDoesNotDoubleRecordedUsage` 调用真实 finalizer 两次（第一次账本失败、第二次成功），用量仓储替身遵守生产唯一键“首次 INSERT 保留”的契约。实际余额只扣 **0.20**，两条用量行的 **TotalCost 合计 0.40**。因此不是重复扣余额，而是请求数、目录费用/账号成本回落统计和会话时长失真。

建议失败诊断使用独立错误/审计存储，或让其不参与正常请求与费用汇总；最终财务用量行保持每个会话一条。若采用更新方案，应验证 pending→settled 的持久金额和所有聚合入口，而不只断言正式 request_id 对应的 ActualCost。

## 验证与集成状态

具体执行结果见随包 `TEST-RESULTS.md`。新增失败用例为上述三个测试；测试入口和依赖替身复用 PR 自带的装配，未照抄被测业务计算。

- PR #11 的迁移白名单、定价刷新生命周期、计费时点定向回归通过。
- PR #12 自带的 21 个相关顶层回归通过；本轮两个新增负例失败。
- 上一轮 Live 的 7 个顶层生产路径复现全部通过；普通 observer 重试、手动 finalizer 恢复和正式用量金额写入已修复，本次仍失败的是组合故障与重复统计。
- PR #13 的真实 worker→用户 DTO 隐私用例、直接 DTO 隐私用例及未知公开错误码回落用例均通过。其视频错误脱敏已经验证，但共同迁移缺口仍阻止合并。
- 对 #13 与当前 develop 的干净试合并树，本机 Go 1.27.3 后端构建通过，前端类型检查通过。额外的后端完整 unit 和前端完整 Vitest 因已经出现合并阻断项而主动中止；前端生产构建尚未执行，不作全量通过声明。
- 已检查 CreateBestEffort 的真实实现：它等待批处理结果，并非入队即确认，因此没有把“异步入队未落库”误报成问题。
- 当前 develop 已包含 Go 1.27.3 和根 Dockerfile 修复；三个 PR 原 head 的 backend-security 失败指向旧 Go 1.27.2 的 GO-2026-6809。不能把旧 head 的红灯直接等同于合并后仍未修复。
- 本地试合并：#11 有一处 `billing_service_unified_test.go` 冲突；#12/#13 与抓取到的 develop 可干净试合并。GitHub 当时显示的 CONFLICTING 与此不同；本轮没有通过强制推送或绕过检查处理它。
- 没有修补或提交业务源码、推送分支、合并 PR、部署或发起付费上游请求。没有把 mock 复现描述为真实数据库升级演练。

修复上述条件后，需要重新获取最新 head 并跑复现再合并；本报告只对表中的精确 SHA 负责。
