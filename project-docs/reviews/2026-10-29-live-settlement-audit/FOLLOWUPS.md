# Live 结算持久化：未完成项与后续方向（2026-10-29）

对应审查记录见 `REVIEW.md`。

本文件记录 PR #16 **刻意没做**或**没做完**的部分。写下来是因为：PR #16 新增了表、状态机、协议原语与 worker，如果只读代码，很容易误以为整条链路已经交付。以下每项都说明「做到哪一步、为什么停在这、下一步怎么接、风险是什么」。

> 一句话概括：**原语层已就绪并有模拟测试覆盖，但生产链路尚未接入。** 新版 Live 结算目前不会因为合并本 PR 而对现网行为产生变化（没有调用方），这在升级时是安全的，但也意味着它还没真正生效。

## 一、服务入口未接入（最关键）

**现状**：`259` 建立了 `live_session_reservations`（provisioning→bound→active→frozen，含 unknown/quarantine）、`live_settlements`（结算 job）、`live_settlement_conflicts`（冲突证据）、`live_session_bindings` 四张表；`LiveSettlementStore` 接口提供了 `RegisterFrozen`/`ClaimDue`/`FinalizeClaimed`/`MarkActive`/`AuthorizeHandoff`/`MarkHandoffDone`/`Lookup`/`ListHandoffs`。仓储层与协议层都有测试。

**但没有生产调用方**：`openai_live.go` 的创建与结算仍走旧路径（Redis 快照 + 失败重试），没有任何代码调用上述接口。搜索全仓，`ConfirmLiveHandoffCreation`、`RegisterFrozen` 只被测试调用。

**为什么停在这**：接入需要在 Live 请求生命周期的三个位置插入持久化调用（建立 reservation、收到首个有效载荷后 bind+activate、SDP 协商完成后冻结），这会改变现网 Live 请求的时序与失败模式，属于必须单独验证的改动，不适合与「修复审查问题」混在一次提交里。

**下一步**：
1. 在 Live 会话创建处调用 `Reserve`（生成 operation_id 幂等键）；
2. 首个有效载荷后 `Bind` → 拿到 `ConfirmedLiveHandoffCreation` → `MarkActive`；
3. SDP 完成后 `RegisterFrozen`；
4. 启动 worker 循环调用 `ClaimDue` → `FinalizeClaimed`（provider 已就绪，见下文）；
5. 旧路径保留一个开关，灰度切换。

**风险**：在接入完成前，现网的 Live 结算失败仍然是「靠 Redis TTL 兜底」的旧行为——即本 PR 描述里想解决的那个「不持久、可能漏账」的问题**依然存在**。不要因为合并了本 PR 就认为漏账问题已解决。

## 二、worker 已装配但无调用方数据

`UsageStatsRepairService` 已接入应用根生命周期（构造时 Start、cleanup 时 Stop），会按周期从 `usage_stats_repair_days` 领脏日并补正。这部分是通的。

但 Live 结算侧的 worker（`ClaimDue`/`FinalizeClaimed` 循环）**尚未实现**——接口有了，没有调度器。原因同第一条：没有调用方写入 job，跑起来也是空转。

## 三、APIKeyService 未注入，标准模式缺价未 fail-closed

**现状**：`SetAPIKeyService` 方法已存在，但在 Live 结算路径上没有调用；`liveBillingCommandFromWhitelist` 从快照白名单还原计费命令，不加载实时计费策略。

**风险**：标准余额模式下，若快照里缺少 `APIKeyService` 相关策略（配额、限速档），结算可能按不完整策略执行。项目红线要求「缺价不得放行」，这条在生产接线前必须闭合。

**下一步**：在 `FinalizeClaimed` 的计费命令构造前注入 `APIKeyService`，标准模式缺价时 fail-closed 转隔离，而不是按零价或默认策略结算。

## 四、平台额度在 Live 恢复后不是严格幂等

**现状**：平台额度仍走原 Redis 增量实现，配后台任务把绝对快照刷回 PG。这次**没有**改成 PG 迁移（用户明确要求不改原实现）。

**风险**：Live 结算从持久状态恢复后重放时，平台额度的增量可能**少计**，或被旧快照覆盖。也就是说：账本与 usage 是幂等的，但平台额度这一路不是。

**下一步**：如需严格一致，要把额度扣减也纳入 `FinalizeClaimed` 的同一事务（或引入幂等键）。这是一次独立设计，不是小修补。

**已在运维文档标注**：`project-docs/08-runbook.md` 的「Live 结算协议切换」章节写明了恢复时需人工对账、不能依赖自动重试。

## 五、容量、保留期与敏感字段清理

**现状**：
- `live_settlements` 的 `snapshot` 是 JSONB，含身份与请求元数据（可能含 PII），目前**没有保留期与清理任务**；
- `live_settlement_conflicts` 只增不减；
- `usage_stats_repair_days` 的 quarantine 行会长期留存；
- Redis 侧 guard 键是**持久键（无 TTL）**，没有容量上限与回收机制。

**风险**：长期运行会积累敏感数据与表膨胀；guard 键无回收在极端情况下占满内存。

**下一步**：为上述表加保留期清理任务（可复用现有 cleanup 框架），guard 键加按 callHash 的引用计数或定期对账回收。

## 六、Redis Cluster 跨槽不支持

**现状**：v2 协议用 Lua 脚本同时操作多个键（call hash、guard、frozen 快照等），**未做 hash tag**。

**风险**：在 Redis Cluster 下这些键可能落在不同槽，脚本会被拒绝执行（CROSSSLOT）。

**下一步**：要么给相关键统一加 hash tag（`{callHash}`），要么文档明确「不支持 Cluster 模式」。当前部署是单实例，暂不影响。

## 七、旧实例排空与混合版本

**现状**：新协议要求新旧版本不能同时处理 Live 会话，运维文档已写明「切换前停止新建 Live 请求、隔离新版流量、确认旧实例排空后才切换」。

**缺口**：**没有强制的版本闸门**。如果运维没按文档操作，新旧实例同时运行会互相覆盖共享 Redis 状态。

**下一步**：在 Redis 中写协议版本标记，旧版本启动时检测到标记即拒绝处理 Live；或提供启动期自检。

## 八、全量对账扫描器缺失

**现状**：`ListHandoffs` 存在，但没有常驻任务定期扫描「已关闭但未结算」的历史记录并告警。

**风险**：Redis 快照过期后，异常会话会失去对账依据（运维文档已写明需在过期前人工对账）。

**下一步**：加一个低频扫描任务，对超期未结算记录告警并写入冲突表。

## 九、从未验证的边界（不是缺陷，是缺证据）

以下内容在本轮**全部未验证**，任何「已修复」的说法都不覆盖它们：

| 项目 | 状态 |
|---|---|
| 真实 PostgreSQL 上的锁 / SKIP LOCKED / SAVEPOINT 行为 | 未验证（仅 sqlmock） |
| `259` / `260` 迁移在真实库执行 | **从未执行** |
| 触发器的 PL/pgSQL 行为（含 P1-13 的 ROW_COUNT 兜底） | 未验证 |
| 真实 Redis 的 TTL / 复制 / 故障语义（miniredis 无法证明） | 未验证 |
| 触发器写放大（P1-15）的实际性能影响 | 未测 |
| 并发调度下的脏日 worker 实际行为 | 未验证（仅单机时序模拟） |
| `group_usage_rollup_trigger_integration_test.go` | 保留旧语义未改，属遗留未验证项 |

## 十、集成测试的状态

仓库中需要真实数据库的集成测试（`-tags=integration`）本轮**没有运行**。`TestMain` 在缺少 Docker 时 `os.Exit(0)`，**退出码 0 不代表通过**——这一点在 CI 上要特别注意：如果 CI 环境没有 Docker，集成测试会「静默成功」。

## 优先级建议

若排下一轮，建议顺序：

1. **服务入口接入**（第一条）——不接，其余都是空转；
2. **APIKeyService fail-closed**（第三条）——触及项目红线，优先级高于一般优化；
3. **真实库执行 259/260 并验证触发器**（第九条）——迁移一旦上生产再发现问题代价高；
4. **旧实例版本闸门**（第七条）——防止运维失误导致账务互相覆盖；
5. 容量/保留期清理（第五条）与对账扫描器（第八条）；
6. 平台额度幂等（第四条）与 Redis Cluster（第六条）——按实际部署形态决定是否做。
