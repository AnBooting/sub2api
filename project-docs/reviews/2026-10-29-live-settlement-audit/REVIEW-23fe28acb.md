# PR #16 增量审查：23fe28acb fix(stats)（2026-10-29）

对同事提交 23fe28acb25ca6bbf40db2312a1060a9d3dccf1c 的独立审查记录。上一轮针对持久化层原语的审查见同目录 REVIEW.md；超出范围、留待后续的项见 FOLLOWUPS.md。

## 被审范围

提交说明：fix(stats): preserve quarantine and complete publication boundaries，声称「Preserve the group publication boundary at new gaps; reject incomplete summaries in consistent snapshots; prevent repair Fail from undoing quarantine. Include production PostgreSQL regressions and public trend error propagation. Validated with build, vet, full unit suite, 11 PostgreSQL regressions and six mutation checks.」

改动 13 个文件，+507/−26：

| 文件 | 变化 |
|---|---|
| usage_stats_completeness.go | 新增（44 行）：快照读取 + 完整性判定 |
| usage_stats_completeness_postgres_test.go | 新增（220 行）：PG 回归 |
| review29_pg_test.go | 新增（134 行）：PG 测试脚手架 |
| dashboard_aggregation_repo.go | 13 行：安全日照常提交但仍返回错误 |
| custom_group_usage_rollup_repo.go | 26 行：gap 处停在水位边界、摘要完整性 |
| usage_log_repo_trend.go | 31 行：聚合失败不再回退原始日志 |
| usage_log_repo_dashboard.go | 10 行：仪表盘走快照 + 完整性检查 |
| usage_stats_repair_queue.go | 4 行：Fail 增加 state = dirty 守卫 |
| usage_stats_repair.go | 4 行：新增 ErrUsageStatsIncomplete |
| 其余 4 个测试文件 | 断言适配 |

## 总体结论

**方向正确，实现质量高，但有三个必须处理的问题。** 其中第一个是「提交说明声称已完成、实际不会执行」的证据缺口，属阻塞级。

## 阻塞级问题

### B1. 11 条「生产 PostgreSQL 回归」在 CI 里 10 条被跳过

提交说明声称用 11 条 PostgreSQL 回归验证。实测：

```
SKIP=10  PASS=1  FAIL=0
--- SKIP: TestReview29GroupWatermarkMustNotCrossNewlyQuarantinedGap
--- SKIP: TestReview29GroupSummaryMustRejectQuarantinedTail
--- SKIP: TestReview29UpgradeDayMustNotSilentlySkipDashboardAggregation
--- SKIP: TestReview29FullMigrationRunner
--- SKIP: TestReview29DashboardContinuesAfterUpgradeGap
--- SKIP: TestReview29DashboardReadersRejectQuarantine
--- PASS: TestReview29GroupSummaryCompleteness     <- 唯一真正执行的一条
--- SKIP: TestReview29QueueFailPreservesRetentionQuarantine
--- SKIP: TestReview29RepairTriggerAndManualDeletion
--- SKIP: TestReview29QueueReclaimFencesOldWorker
--- SKIP: TestReview29GroupContinuousPrefixAndExplicitReconciliation
```

机制：review29DB(t) 读取 REVIEW29_PG_DSN，为空即 t.Skip()：

```go
dsn := os.Getenv("REVIEW29_PG_DSN")
if dsn == "" {
    t.Skip("set REVIEW29_PG_DSN to a disposable loopback PostgreSQL instance")
}
```

**决定性证据**：在仓库中检索 REVIEW29_PG_DSN，只命中 review29_pg_test.go 自身（第 22/24/111/113 行）；.github/workflows/ 下检索 postgres 与 services: **零命中**。也就是说 CI 永远不会注入该变量，这 10 条测试在 CI 上恒定跳过。

测试内容本身写得扎实（用真实迁移文件、真实 SQL、真实交错时序），问题不在质量而在**接入**。这与 AGENTS.md 记载的 TestMain os.Exit(0) 陷阱同类：**退出码 0 不代表通过**。

另需注意：TestReview29FullMigrationRunner 是唯一真正验证 259/260 能在真实库上完整执行的用例，它同样被跳过。而 259/260 迄今从未在任何真实数据库执行过（见 FOLLOWUPS.md 第九节）。

### B2. 两处修补无任何测试锁住（变异验证为绿）

按项目验证纪律第 3 条做变异验证：回退修复，确认测试转红。

| 修补位置 | 变异内容 | 回退后测试结果 | 判定 |
|---|---|---|---|
| usage_stats_repair_queue.go Fail | 移除 AND state = dirty | 全绿（EXIT=0） | **无覆盖** |
| usage_log_repo_trend.go | 移除 aggregatedErr 提前返回 | 全绿（EXIT=0，FAILS=0） | **无覆盖** |
| dashboard_aggregation_repo.go AggregateRange | 移除 incomplete = ErrUsageStatsIncomplete | **2 条转红** | 覆盖良好 |

第三项变异会触发：

```
--- FAIL: TestUsageStatsRepair_ScheduledSkipsUnsafeClosedDayButRefreshesOpenDay
--- FAIL: TestUsageStatsRepair_AggregateSkipsAlreadyQuarantinedDay
```

前两条修补的唯一断言位于那些会 SKIP 的 PG 测试中（TestReview29QueueFailPreservesRetentionQuarantine、TestReview29DashboardReadersRejectQuarantine）。**后果**：在没有 REVIEW29_PG_DSN 的环境里（包括 CI），把这两处改回去不会被任何测试发现。

## 需要产品确认的设计取舍

### D1. 仪表盘采用「全历史区间」完整性检查

fillDashboardUsageStatsAggregated 用 epoch 到 9999-01-01 做完整性判定：

```go
if err := snapshot.requireUsageStatsAvailable(ctx,
    time.Unix(0, 0).UTC(),
    time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
    return err
}
```

而 requireUsageStatsAvailable 的判定是：范围内存在任一 quarantine 日即返回 ErrUsageStatsIncomplete。

**后果**：只要历史上任意一天被隔离，GetDashboardStats 整体失败，继而影响：

- 管理端仪表盘（dashboard_handler.go:82、dashboard_snapshot_v2_handler.go:169）
- 支付统计（payment_handler.go:39、payment_stats.go:19）
- 账号用量（account_usage_service.go:47）

由于 260 迁移把 raw_safe_from 初始化为迁移时刻，**升级当天很可能自动产生隔离日**（升级前的历史无法证明完整）。因此最可能的现实后果是：**升级后仪表盘整体不可用**，直到人工对账清除隔离。

代码注释表明这是有意为之（"This endpoint includes all retained daily buckets, not only today"），技术上自洽——宁可报错也不给错数字。但这属于产品行为决策，需确认是否可接受；若不可接受，应按查询区间判定，或降级为「部分数据不可用」的显式标记而非整体失败。

## 实现质量确认

以下改动经核对正确，值得保留：

1. **readUsageStatsSnapshot 用 REPEATABLE READ 只读事务**（usage_stats_completeness.go:18），保证水位、隔离状态、统计值读自同一快照。这抓住了真正的异步一致性问题：三次独立查询可能落在不同快照上，出现「水位说完整、隔离表说缺失」的矛盾。
2. **AggregateRange 跳过隔离日但仍提交安全日并返回错误**：避免升级日把后续所有天永久卡死，同时不谎报成功。变异验证证明这条有测试。
3. **syncGroupUsageRollupsInTx 在 gap 起点直接 return nil**：不推进水位、不改动既有桶，只提交隔离事实。逻辑正确。
4. **区间重叠判断 day_start < $3 AND day_end > $2**：标准写法，正确。
5. **移除 day.Before(today) 条件**：当日同样受 raw_safe_from 约束，是语义修正而非退化。
6. **Fail 增加 state = dirty**：防止 worker 在保留期清理已把该日置 quarantine 后，用过期 lease 把 quarantine 改回 dirty 或推成 quarantine(attempts)。这个竞态考虑得对（只是缺测试）。

## 建议

按优先级：

1. **把 PG 回归接进 CI**：在 test workflow 加 services: postgres 并注入 REVIEW29_PG_DSN。否则 354 行 PG 测试是死代码，259/260 仍无任何真实库证据。
2. **为 B2 的两处修补补 sqlmock 层测试**，使它们在无真实库时也能被锁住。
3. **确认 D1 的产品行为**：升级后仪表盘整体不可用是否可接受；若不可接受，改为按查询区间判定或返回部分可用标记。

## 附：复现命令

需要 Go 1.27.3（GOROOT=F:\\tools\\go1.27.3\\go、GOTOOLCHAIN=local），在 backend/ 下执行。

```powershell
# 1. 确认 PG 测试跳过比例
go test -tags=unit ./internal/repository -run "TestReview29" -count=1 -v 2>&1 |
  Select-String -Pattern "^--- (SKIP|PASS|FAIL)"

# 2. 确认 CI 未注入 DSN（只命中测试文件自身，workflows 零命中）
Get-ChildItem -Recurse -Include *.go,*.yml -File | Select-String "REVIEW29_PG_DSN"
Select-String -Path .github/workflows/*.yml -Pattern "postgres|services:"

# 3. 全量单元验证（本次实测 BUILD=0 / VET=0 / TEST=0）
go build ./... ; go vet ./internal/repository ./internal/service
go test -tags=unit ./internal/repository ./internal/service -count=1
```

变异验证的做法：用文本替换回退目标修补 -> 重跑上述测试 -> 确认结果 -> 还原文件。本次三项变异的实测结果见 B2 表格。

## 与上一轮审查的关系

REVIEW.md 记录的 18 条 P1 中，本轮同事的改动响应了其中几条的残留风险：

- P1-13（触发器静默 0 行）→ 由 260 的 GET DIAGNOSTICS 兜底（上一轮已修）
- P1-16（隔离后静默按 0）→ 本轮 getAllGroupUsageSummaryFromRollups 增加完整性检查，正面响应
- P1-17/P1-18（人工删除语义）→ 本轮 TestReview29RepairTriggerAndManualDeletion 断言删除后应为 dirty 而非 quarantine，与上一轮的语义修正一致（但该测试当前 SKIP）

本轮未引入回归：全量 unit 套件 BUILD/VET/TEST 均为 0。
