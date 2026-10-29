# 2026-10-29 Live 结算持久化深入审查

本目录四份文档，按时间顺序：

| 文档 | 内容 | 结论 |
|---|---|---|
| `REVIEW.md` | 持久化层原语的设计级审查 | 18 条 P1，17 条已修并验证 |
| `FOLLOWUPS.md` | 超出本次范围、留待后续的项 | 原语层就绪但生产链路未接入 |
| [`REVIEW-23fe28acb.md`](REVIEW-23fe28acb.md) | 同事后续提交 `23fe28acb fix(stats)` 的增量审查 | 方向正确；1 条阻塞（PG 回归在 CI 全跳过）、2 处修补无测试锁住、1 处需产品确认 |
| [`RESPONSE-eb9c2012b.md`](RESPONSE-eb9c2012b.md) | B1/B2 的 CI 与无库回归补强；按用户决定实现部分可用 | 本机 PG 13 项零跳过，前后端验证通过；未推送 |


## 范围

被审内容：PR #16（`audit/full-20261019`）中 Live 结算持久化与迟到用量统计补正这批改动，涉及：

- `backend/migrations/259_live_settlements.sql`、`260_usage_stats_repair.sql`
- `backend/internal/service/live_settlement_store.go`、`live_handoff_protocol.go`、`usage_stats_repair.go`、`openai_live.go`
- `backend/internal/repository/live_settlement_repo.go`、`live_settlement_reservation_repo.go`、`live_call_handoff.go`、`usage_stats_repair_queue.go`、`dashboard_aggregation_repo.go`、`custom_group_usage_rollup_repo.go`、`usage_cleanup_repo.go`、`gateway_cache.go`、`usage_billing_repo.go`
- `backend/cmd/server/wire.go`（根生命周期）

## 审查方式与边界

**静态源码审查**：只读代码与测试源码，没有运行测试、没有编译、没有启动任何真实 PostgreSQL 或 Redis。

因此结论分两类：

- **代码反例**（静态即可确定，例如「SQL 只到 `$6` 却绑 8 个实参」「RETURNING 不返回某字段，而下游用它做比较」）——这类判断不依赖运行；
- **已验证修复**（有红测→绿测或变异验证支撑）——但仍限于模拟层面。

**没有验证**：真实 PostgreSQL 的锁/SAVEPOINT/触发器行为、`259`/`260` 在真实库的执行、真实 Redis 的 TTL 与复制语义、并发调度下的 worker 行为。详见 `REVIEW.md` 末节与 `FOLLOWUPS.md` 第九节。

## 与历史轮次的关系

`project-docs/reviews/` 下已有 6 轮记录。本轮不同于以往：以往是对「已完成功能」做复审，本轮是对**新增的持久化层**做设计级审查，因此问题集中在事务边界、幂等性、跨层一致性这类静态可判定但运行难复现的地方。

其中 **Fail 路径的 SQL 占位符错位**（详见 `REVIEW.md` P1-5b）是审查之外由 Lead 复核代码时发现，说明 mock 测试对这类缺陷是盲的——这也是为什么本轮补了 `review_queue_sql_placeholder_test.go` 这个基于 `go/ast` 的静态断言测试。

## 如何复现验证

全部为单元测试，**不需要真实数据库**：

```powershell
cd backend
$env:GOROOT="F:\tools\go1.27.3\go"; $env:GOTOOLCHAIN="local"; $env:GOPATH="F:\tools\gopath"

# 全量单元测试
go test -tags=unit ./... -count=1

# 结算状态机
go test -tags=unit ./internal/repository -run "^TestLive(Reservation|Settlement)" -count=1 -v

# Redis v2 交接协议
go test -tags=unit ./internal/repository -run "^TestLiveHandoffV2" -count=1 -v

# SQL 占位符静态校验（含负向自证）
go test -tags=unit ./internal/repository -run "TestQueueSQLPlaceholders" -count=1 -v

# 原始 review 用例（修复前 455s，现在应为 0.01s）
go test -tags=unit ./internal/service -run "^TestObserveLiveCallStoreOutageFallsBackToExpiryFinalize$" -count=1 -v
```

需要真实数据库的 `-tags=integration` 套件本轮未运行。注意：其 `TestMain` 在缺少 Docker 时 `os.Exit(0)`，**退出码 0 不代表通过**。

## 相关文档

- `project-docs/08-runbook.md` — 「Live 结算协议切换」运维章节（排空、对账、隔离规则）
- `project-docs/管理员使用手册.md` — 「扣了费但用量记录缺行」已改为不再承诺自动恢复
