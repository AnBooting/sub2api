# Live 结算持久化深入审查（2026-10-29）

对被审分支 `audit/full-20261019`（PR #16）中 Live 结算持久化与迟到用量统计补正这批改动做的独立只读审查记录。

## 这次审查是什么

PR #16 在原有「Live 会话结算失败后靠 Redis TTL 兜底」的基础上，引入了 PostgreSQL 侧的持久结算状态机（`259_live_settlements.sql`）与迟到用量统计补正（`260_usage_stats_repair.sql`），并把 Redis 侧交接协议升到 v2。这块涉及钱和账，改动面又大，所以在合并前安排了一次独立审查。

审查方式是**静态源码审查**：只读代码与测试源码，**没有运行测试、没有编译、没有启动真实 PostgreSQL 或 Redis**。因此下面每条的「证据状态」区分得很清楚：哪些是代码反例（静态即可确定）、哪些有红测/绿测支撑、哪些仍属未验证。

## 结论摘要

共 18 条阻塞级（P1）。其中 17 条已修并验证，1 条（P1-15）按「风险待评估」记录而非代码缺陷。

最关键的一条是 **Fail 路径的 SQL 占位符错位**（P1-5 的一部分）：SQL 只用到 `$6`，却绑定 8 个实参，且 `$4`/`$5` 同时被当作两种类型。**sqlmock 只按字符串匹配 SQL，所以这类错误在 mock 下全绿，真实 PostgreSQL 会直接报绑定失败。** 这条是审查之外由 Lead 复核代码时发现的，说明仅靠 mock 测试无法覆盖这一类缺陷。

## 逐条清单

### 结算状态机（PostgreSQL）

**P1-1 ClaimDue 不返回冻结摘要与代次，Finalize 必失败**

`ClaimDue` 的 `RETURNING` 与 `Scan` 只带 id/snapshot/status/owner/claim_generation/lease_until，从未填充 `claim.Job.FrozenDigest` 与 `claim.Job.HandoffGeneration`；`FinalizeClaimed` 拿这两个零值做比较，因此**任何经生产 ClaimDue 返回的正常任务都必定报 `ErrLiveSettlementConflict`，永远无法完成结算**。同时 `snapshot.Billing == nil` 被直接拒绝，而零额与 simple_skip 模式本来就不生成 Billing，这些模式也无法落账。

修复：RETURNING 增补两列并扫描；零额/simple_skip 允许无 Billing 命令完成结算。
测试：3 条红测（ClaimDue 填充、生产链 ClaimDue→Finalize 成功、零额成功），修复前全红、修复后绿。
证据状态：已修，红→绿。

**P1-2 先扣款后查冲突，破坏账务/用量原子性**

`FinalizeClaimed` 先执行 `applyUsageBillingEffectsTx` 扣款，之后 `writeLiveUsageLogTx` 才发现历史同键 usage 冲突；遇到冲突时当前代码把 quarantine 与**已生效的扣费一起 COMMIT**，结果是「钱扣了，usage 保留旧错误行」。

修复：扣费段置于 `SAVEPOINT live_finalize_billing`，冲突时先 `ROLLBACK TO SAVEPOINT` 再提交隔离事实。
测试：TOCTOU 专测 `TestLiveSettlementFinalizeUsageWriteConflictRollsBackBilling`；变异（移除 ROLLBACK）该测试转红。
证据状态：已修，红→绿 + 变异验证。

**P1-3 Finalize 信任调用方可变快照，缺少持久状态自证**

`FinalizeClaimed` 用调用者传入的 `claim.Job.Snapshot` 构造计费命令与 usage，digest 只与**同一调用者对象**里的 `FrozenDigest` 比较；fencing SQL 只校验 id/owner/claim_generation/lease/status，不读该行的 snapshot/identity/digest/generation。如果服务误用或篡改了 Snapshot，可以**用 job X 的合法租约写出 job Y 的账本与 usage，并把 X 标为 settled**。

修复：在同一把已锁定的 PG 行内 `SELECT snapshot, status, frozen_digest, handoff_generation ... FOR UPDATE`，重算 `ImmutableDigest` 与库中值比对，再用权威快照重建命令与 usage。
证据状态：已修；属源码控制流修正，未做变异。

**P1-4 未实现 bound→active 转换，注册链断裂**

`Bind` 只把 reservation 置为 `bound`，全仓没有把它置为 `active` 的生产 SQL 或接口；而 `RegisterFrozen` 只接受 `active`/`frozen`。因此 `Reserve → Bind → ConfirmLiveHandoffCreation → RegisterFrozen` 这条正常链条**无法注册冻结**。

修复：新增 `MarkActive(proof ConfirmedLiveHandoffCreation)`，repo 实现 bound→active（active 幂等；quarantine/unknown/错代/身份不符拒绝）。
测试：经生产路径 `ConfirmLiveHandoffCreation` 构造 proof 的 3 条测试。
证据状态：已修，绿。

**P1-8 同金额不同归属被判成功，造成错账**

`writeLiveUsageLogTx` 在 `(request_id, api_key_id)` 已存在时只比对 total_cost / actual_cost / duration_ms。若旧行金额相同但 UserID/AccountID/GroupID/SubscriptionID、model、CreatedAt、RequestType、billing mode、UA/IP/endpoint 任一不同，会被当作成功并标 settled，**扣费归属与统计维度或时间桶错位，无法对账**；尤其 CreatedAt 不同会落到错误的历史桶。

修复：`liveLockedUsageLog.matches` 改为全字段等值（含归属、时间桶、UA/IP/端点、费率倍率、金额、时长），不等即隔离；先 `FOR UPDATE` 锁住被比对行防并发改写。
测试：5 个子用例（account_id / created_at / model / user_agent / inbound_endpoint）；变异（关闭预检）转红。
证据状态：已修，红→绿 + 变异验证。

**P1-9 账本归档冲突时留下伪热键**

`usage_billing_repo.go` 先 INSERT 热表 dedup，再读归档。若只存在归档且 fingerprint 不匹配，**热表的新冲突指纹已经在本事务里插入**，函数返回 `ErrUsageBillingRequestConflict`。`FinalizeClaimed` 捕获后直接 COMMIT quarantine，于是错误热键被提交，覆盖热层查询优先级——后续同一请求只会看到这条伪热键并返回「已应用」，既不读归档也不扣款。

修复：claim INSERT 置于同一 SAVEPOINT，指纹冲突先 `ROLLBACK TO SAVEPOINT` 再提交隔离。
测试：专测断言执行顺序，确认热键未被保留。
证据状态：已修，绿。

**P1-11 MarkHandoffDone 无持久授权即可调用**

`259` 迁移有 `handoff_authorized` / `handoff_authorized_at` 两列，但全仓没有任何读取或 UPDATE；`MarkHandoffDone` 只按 raw gen/digest 和 status 更新，**可在没有持久授权、没有 Redis ACK 证据的情况下被调用**，与已知的 ACK 与隔离之间的 TOCTOU 相冲突。

修复（经 Lead 确认采用方案 A）：新增 `AuthorizeHandoff`（同一行 FOR UPDATE 校验身份/摘要/代次，幂等、不可撤销、隔离拒绝），作为 ACK 成功后的持久线性化点；`MarkHandoffDone` 改为事务内先校验 `handoff_authorized`，未授权/错代/隔离一律拒绝。
测试：3 个分支（未授权拒绝、已授权成功且幂等、隔离拒绝）；变异（移除 auth gate）转红。
证据状态：已修，红→绿 + 变异验证。

**P1-12 见 P1-3**（同一问题，审查分两次提出，此处不重复计数）

### Redis v2 交接协议

**P1-5（Redis 侧）可空字段存在性无法区分，创建可重放**

可选字段（price/policy）以 nil 写入时会被存成空值，创建仍可重放；空串与 nil 无法区分。

修复：增加 presence 标记、Lua 参数循环边界 `#ARGV-2`、存在性断言，创建与 begin 双侧检查。
测试：`RejectsPricePresenceMismatch` 4 个子用例。
证据状态：已修，红→绿。

**P1-6（Redis 侧）guard 键带 TTL 仍允许确认**

guard 键即使设置了 TTL 也能通过校验，无法保证持久性。

修复：6 处入口（create 重放 / Begin / ACK / Inspect×2 / Controller claim+release）统一要求 `PTTL(guard) == -1`。
测试：`GuardTTLBlocksRemainingEntrypoints` 4 个子用例。
证据状态：已修，红→绿。

### 迟到用量统计补正

**P1-5（统计侧）脏日队列 ClaimDue 占位符缺失，真实 PG 必失败**

`ClaimDue` 的 SQL 使用 `$1` 与 `$3`，从未引用 `$2`，而 owner 作为第二个实参传入。**PostgreSQL 扩展协议在 prepare 时无法推断未使用参数 `$2` 的类型**，通常报 `could not determine data type of parameter $2`，导致每次 ClaimDue 失败、没有脏日能被补正。sqlmock 只匹配字符串，覆盖不到。

修复：`$2` 落为 `claim_owner`，并新增 `claim_fence`（每次抢占递增）、RETURNING 带出 owner/fence。
测试：8 条，含 2 条 stale owner/fence 的红→绿。
证据状态：已修，红→绿。

**P1-5b Fail 的 SQL 占位符错位（审查之外由 Lead 复核发现）**

`Fail` 的 SQL 只用到 `$6`，却绑定 8 个实参；且 `$4` 在 SET 里作「最大重试数(int)」、在 WHERE 里又作 `claim_owner(text)`；`$5` 在 SET 里作 `reason(text)`、在 WHERE 里又作 `claim_fence(int)`。**参数个数（8 绑 6 位）与类型冲突任一条都足以让真实 PG 失败**，而 Fail 是退避重试与转 quarantine 的唯一路径，坏了等于脏日永远无法失败退出、也无法隔离。

修复：SET 段改用独立编号 `$6`/`$7`/`$8`，WHERE 保持 `$4`=owner、`$5`=fence，8 个占位符与 8 个实参严格 1:1。
测试：新增 `review_queue_sql_placeholder_test.go`，用 `go/ast` 解析源码，断言每处 `QueryContext`/`ExecContext` 的最大 `$N` 等于实参个数、且 `$1..$N` 连续；并含**负向自证**——喂入历史 bug 形态必须报错。当前覆盖 3 处 SQL。
证据状态：已修，绿；负向自证覆盖检测器本身。

**P1-6（统计侧）RecomputeRange 可绕过隔离覆盖旧桶**

`quarantineUnsafeUsageRepairRange` 在发现已隔离日时 `continue` 并最终返回 `false`，`RecomputeRange` 据此继续 `recomputeRangeInTx`，**先 DELETE 再以可能不完整的 usage 重建历史**。第二次人工重算、或多日范围中仅有已隔离日时，会绕过既有 quarantine 并覆盖。

修复：范围内只要出现任一已隔离日即**整体拒绝（返回 error）**，不跳过、绝不 DELETE/重建。
测试：隔离日 + 安全日混合范围断言整体拒绝、隔离状态不倒退。
证据状态：已修，红→绿。

**P1-7 分组日桶向下取日，跨日会删除并重建不完整的首日**

`syncGroupUsageRollupsInTx` 把 `raw_safe_from` 向下取其本地日期作为 rebuildStart，而 `260` 把 `raw_safe_from` 初始化为迁移时刻（通常当天中途），该日起点早于可证明完整的边界。待跨日后，若 `closed_before` ≤ 当日，会删除并按不完整的当天明细重建该日，**破坏既有完整历史**；隔离循环也不含 rebuildStart 本日。

修复：rebuildStart 取 `raw_safe_from` 所在完整日的起点（边界恰为本地零点才用当天，否则下一天），且重算前检查既有 quarantine。
测试：`raw_safe_from`=当天 06:00、跨日、旧桶有预迁移已删除用量，断言当天不删除、不发布为完整。
证据状态：已修，红→绿。

**P1-10 统计 worker 未接入根生命周期，实际不会运行**

`service/wire.go` 定义了 provider 并放进 ProviderSet，但 `cmd/server` 的 Application/cleanup 没有依赖 `UsageStatsRepairService`，生成的 `wire_gen.go` 完全没有创建它的调用。**Wire 不会实例化未被根依赖引用的 provider，因此正常运行进程从不扫描 dirty**，即便 SQL 全部修好也没用。

修复：Application 结构体增加字段、`wire.Struct` 纳入、`provideCleanup` 增加参数与 Stop 步骤，并重新生成 `wire_gen.go`。
证据状态：已修，编译与 wire 生成通过。

**P1-13 触发器 ON CONFLICT 静默 0 行，脏标记丢失**

`260` 的触发器 `ON CONFLICT DO UPDATE ... WHERE day.day_end = EXCLUDED.day_end`：若时区规则更新或历史行损坏使同一 day_start 对应不同 day_end，写入 usage 仍成功但触发器**静默更新 0 行**，旧 dirty/clean 状态不变，事件被漏掉。

修复：`GET DIAGNOSTICS ROW_COUNT` 探测；为 0 时显式把该日置 sticky `quarantine`（`reason_code = 'day_end_mismatch'`），再探测一次，仍为 0 则 `RAISE EXCEPTION`。
证据状态：已修；真实 PG 未执行，触发器的 PL/pgSQL 行为未验证。

**P1-14 owner 为常量，无法区分实例**

`usage_stats_repair.go` 所有实例传入同一个 `usageRepairWorkerName` 常量，即使新增 owner 列，沿用该值也无法区分实例；叠加租约 2 分钟而单批最多处理 5 分钟，**租约可能在处理完成前过期，另一个 worker 能按同一日 generation 重领**；旧 worker 的失败回复仍能同 generation 执行 Fail，把 clean 改回 dirty 或推成 quarantine。

修复：每次抢占递增独立 `claim_fence`，`Ack`/`Fail` 同时校验 owner + fence + generation + state，不匹配即 `ErrUsageRepairLostLease`；service 侧增加每实例唯一 ownerID。
测试：模拟同 owner、租期已过后重领旧 worker 的用例。
证据状态：已修，红→绿。

**P1-16 隔离后仍推进水位，缺失历史被静默算作 0**

`syncGroupUsageRollupsInTx` 遇 raw 不完整日只记录 quarantine，却仍把 `closed_before` 推到今日；`getAllGroupUsageSummaryFromRollups` 不检查隔离状态。结果是：某日因保留期缺失原始明细且从未建立 group rollup，该日被隔离并跳过，水位前移后查询既不扫该日 raw、又没有该日已发布桶，**静默把缺失部分按 0 计入 total/yesterday**。

修复：水位不得越过未证实完整的日；查询层对隔离/未证实日显式返回不可用状态，而不是按 0 计算。
证据状态：已修，红→绿。

**P1-17 空删除也推进全局边界，正常历史全部失效**

`usage_cleanup_repo.go` 在 DELETE 前无条件按用户指定的 `EndTime` 推进全局 `raw_safe_from`，且删除零行也 COMMIT。管理员提交例如 2099 年的空范围，即可**零行删除却把 `raw_safe_from` 经 `GREATEST` 永久推进到 2099 年次日**，直到该日期之前所有正常写入都会被触发器视为原始历史不完整而 quarantine，历史重算停摆。

修复：人工删除不再推进全局 `raw_safe_from`（见 P1-18），空匹配/未来窗口因此不再改动任何完整性状态。
测试：新增 `TestUsageCleanupRepositoryDeleteUsageLogsBatchEmptyWindowDoesNotTouchRepairState`，未来窗口零行删除断言不触碰控制表与日表——任何多余写入都会被 sqlmock 判为未预期查询而失败。
证据状态：已修，绿。

**P1-18 实删后已发布桶仍保留被删数据**

即使所删记录所在日原本明细完整，手动清理也会先把 `raw_safe_from` 推过该日；DELETE 触发器据此把该日永久 quarantine，而 service 层先标任务 succeeded 再异步触发 RecomputeRange，**已隔离日不会被可靠重建**。结果是管理员成功删除 usage 后，已发布的 dashboard / 分组桶仍保留被删请求、金额与时长。

修复：人工删除**不推进 `raw_safe_from`**。可证明完整的日由触发器原子置 dirty，交持久脏日队列按剩余明细重算；已不可证明完整的日保持隔离，且查询显式报告不可用，不声称统计已清理完成。
测试：清理用例改为断言「不出现 repair_control / repair_days 写入」，另有 P1-17 的空窗口用例。
证据状态：已修，绿；真实 PG 上触发器的置脏行为未验证。

### 非缺陷类记录

**P1-15 触发器写放大（风险待评估，非缺陷）**

每次 usage 的 INSERT/UPDATE/DELETE 都会对同日 `usage_stats_repair_days` 行做 `ON CONFLICT DO UPDATE`，会把同一天的跨实例请求串行在单个 PG 行锁上，并在高频写入路径产生放大。由于本轮禁止真实库与负载测试，**只登记为需评估的风险，不断言实际性能回归**。

## 验证证据汇总

| 项目 | 命令 | 结果 |
|---|---|---|
| 编译 | `go build ./...` | 0 |
| 静态检查 | `go vet ./internal/repository ./internal/service ./cmd/server` | 0 |
| 全量单测 | `go test -tags=unit ./... -count=1` | EXIT=0，无 FAIL |
| 格式 | `gofmt` | 无残留 |
| 原始 review 用例 | `TestObserveLiveCallStoreOutageFallsBackToExpiryFinalize` | 0.01s 通过（修复前 455s） |
| Redis v2 回归 | `-run "^TestLiveHandoffV2"` | 24 RUN，14 顶级全 PASS，FAIL=0 |

变异验证（回退修复确认测试转红，再恢复）：P1-2 的 ROLLBACK、P1-8 的预检、P1-11 的 auth gate、P1-6 的 quarantine 守卫，各自回退后对应测试均转红。

## 未验证边界（重要）

- **真实 PostgreSQL / Redis 全程未下载、未启动、未连接、未验证。** 锁语义、`SKIP LOCKED`、SAVEPOINT、触发器 PL/pgSQL 行为均只在 sqlmock 与静态审查层面推演。
- `259_live_settlements.sql` 与 `260_usage_stats_repair.sql` **未在任何真实数据库执行过**。
- 上述「已修」均指**代码与模拟测试层面**通过，不代表真实数据库上的行为已验证。
- 超出本次范围的未完成项单独记录在 `FOLLOWUPS.md`。
