# AnBooting/sub2api PR #11 / #12 / #13 复审

日期：2026-10-26（Asia/Singapore）。**上轮三个阻断项的复现均已通过；本轮确认 2 个新增 P2，并复现 1 个既有 P1，暂未合并任何 PR。新增 P2 是本轮合并阻断依据；既有 P1 登记后续处理。**

| PR | 本轮 head | 结论 |
|---|---|---|
| [#11 上游同步](https://github.com/AnBooting/sub2api/pull/11) | `653f4db5330ef9db5868f3cc045cc16ad02a7318` | 迁移业务修复通过；旧集成用例仍断言旧行为，暂缓 |
| [#12 审计修复](https://github.com/AnBooting/sub2api/pull/12) | `9372f3ec80de333ea04b3ec3c3e8d9bce8edc6e6` | Live 回读故障漏重试、审计信息泄露、共同集成用例问题，暂缓 |
| [#13 产品功能](https://github.com/AnBooting/sub2api/pull/13) | `4b91a93ed5f207f484262a0488d9001591c01f82` | 与 #12 共用相关 Live 修复，仍有同样问题，暂缓 |

三条 PR 的实际 base 均为 `4956d55982b134e615feb1e787e19846b1c9ddf0`。GitHub 当前显示 MERGEABLE / CLEAN，读取到的 statusCheckRollup 为空，不能将其解释为 CI 已通过。本轮按当前 base→head 增量审查：分别为 19 / 30 / 39 个路径，重点是迁移历史、Live 恢复、审计存储及用户投影。

## 已修复并验证的上轮问题

- 迁移重复对象容错现在会在持有迁移锁的连接上查询旧 filename，校验数据库记录的 checksum，并补记新文件名。缺旧记录、checksum 漂移、查询错误和补记失败均拒绝。本轮 10 个相关顶层单测通过；上轮 `TestReviewMigrationDuplicateWithoutVerifiedLegacyHistoryMustFail` 也通过。上轮迁移 P1 不再成立。
- 检查点写失败与账本故障同时发生时，原组合故障复现已通过；内存冻结状态现在在入口回读之前提取，连续检查点写失败也能使用完整重试预算。
- 失败审计已从 usage_logs 移到 ops_error_logs；上轮 TotalCost 重复累计复现通过。不能继续用上轮“0.20 变 0.40”的结论评价当前 head。

## 1. [P1，既有] 重试循环内的一次 Redis 读错误仍会丢掉全部剩余重试

影响 #12 / #13。位置：[openai_live.go:841](https://github.com/AnBooting/sub2api/blob/9372f3ec80de333ea04b3ec3c3e8d9bce8edc6e6/backend/internal/service/openai_live.go#L841)。

归属核对：当前 develop 的同一循环已经包含读错即 return，`git blame` 追溯到 `6ec185397`。因此这是该恢复路径仍未覆盖的既有缺口，**不是本轮引入**；按项目规则留作后续修复，不将它混入新增回归。

`retryPendingLiveSettlement` 的入口回读已容错，循环末尾的状态回读也已容错，但循环开头仍是 `GetLiveCall` 一旦返回错误就直接 return。这里保存着 pending 冻结金额，也有剩余预算；一次瞬时读失败不等于结算不存在，却会让真实 observer 永久退出这条重试链。

`TestReviewLiveObserverRecoversFromOneRedisReadFailure` 从真实 `observeLiveCall` 入口驱动到期会话：首次账本故障留下 pending；仅注入一次 Redis 读错误，随后 Redis 与账本立即恢复。控制组将错误放在重试入口回读，能完成结算；实验组只将错误移到循环第一次回读，Apply 总次数仍为 **1（期望 2）**，持久状态仍为 **pending（期望 settled）**。没有用手动再次调用 finalizer 代替自动恢复。

建议循环内将临时读取错误纳入有界退避，保留已冻结金额和剩余预算；单独处理明确的记录不存在/已完成状态。补充真实 observer 入口、循环回读与末尾回读的故障位置矩阵。

## 2. [P2] 新的结算审计会把内部账本诊断暴露给普通用户

影响 #12 / #13。位置：[openai_live.go:1210](https://github.com/AnBooting/sub2api/blob/9372f3ec80de333ea04b3ec3c3e8d9bce8edc6e6/backend/internal/service/openai_live.go#L1210)。

新审计把原始 `cause` 写入 ops_error_logs.error_message，同时填写 UserID 和 HTTP 500。该表不是管理员专用存储：`OpsService.ListUserErrorRequests` 按当前用户查询该表，`ToUserErrorRequest` 又将 Message 原样下发。500 会通过查询的 status>=400 守卫；用户选择 internal 分类也可以读到这条记录。

`TestReviewLiveSettlementDiagnosticIsPrivateFromUserErrorList` 调用真实 finalizer 生成审计，再调用真实用户错误列表服务和投影。存储替身只按生产 SQL 的 `error_message → Message` 映射保存/读取数据。给账本注入包含虚构内网地址 `10.23.45.67:5432` 和内部表名 `usage_billing_dedup` 的错误后，这些内容出现在用户响应 JSON 的 message 中，负例失败。

这是实际生产代码的 producer→用户 API 投影复现与仓储 SQL 核对；没有连接 PostgreSQL，也没有声称已泄露真实密码、API Key 或真实生产地址。

建议完整诊断保留在管理员可见字段/日志，用户侧只返回稳定错误码和固定公开说明；或将该类内部审计从用户错误请求查询中排除。不能仅因为使用 opsRepo，就将它当作管理员专用路径。

## 3. [P2] 旧库升级集成测试仍断言“不补记”，与当前执行器直接冲突

影响 #11 / #12 / #13。位置：[migrations_renamed_replay_integration_test.go:68](https://github.com/AnBooting/sub2api/blob/653f4db5330ef9db5868f3cc045cc16ad02a7318/backend/internal/repository/migrations_renamed_replay_integration_test.go#L68)。

当前执行器在旧历史验证通过后会 INSERT 新 filename；升级说明也已改为自动补记。但原 `TestRenamedMigrationReplayOnLegacyDatabase` 仍查询新名记录数并 `require.Zero`。正常完成本次迁移修复就必然得到 1，触发这条旧断言。

本轮从该原测试提取完整的“旧库升级→检查新历史”场景正文（保留原断言，停止于其后与本问题无关的未登记迁移场景），仅替换 build tag、函数名和 DB 变量名，用 sqlmock 重放。真实执行器确实发出新历史 INSERT，随后原断言失败：**Should be zero, but was 1**。测试是 `TestReviewExistingLegacyIntegrationContract`。这不是实际 PostgreSQL 集成执行，亦不是已观测到远端 CI 红灯。

[backend-ci.yml:44](https://github.com/AnBooting/sub2api/blob/653f4db5330ef9db5868f3cc045cc16ad02a7318/.github/workflows/backend-ci.yml#L44) 会执行 make test-integration；因此按当前配置在有 Docker 的 CI 中运行该原用例，会因这个断言失败。本机未找到可用 Docker 命令，未执行实际 integration 套件；不能把集成 harness 因环境缺失而跳过解释为通过。

建议同步断言新名恰好一条、checksum 正确、再次执行不重放，并调整旧注释及 cleanup。保留新增的旧记录缺失/漂移等拒绝测试；不要删掉集成用例以回避冲突。

## 验证边界与交付

- 最新 #13 包含共同迁移和 Live 修复。迁移 runner 三条分支 blob 都是 `a093915d01d16b7237248ce9857d2bcd5588aaed`，集成测试三条分支 blob 都是 `8c53cf59a3a8c3297ddf9af22114a0780042767b`；#12/#13 的 openai_live.go blob 均为 `5d8005ce110496652a28ee2332eac430b2723ead`。因此上述发现的分支归属经过当前对象核对。
- 第一次定向套件：44 个顶层测试通过，新增 Redis 故障位置负例失败。第二次针对性验证：上轮缺旧历史负例通过，三个本轮负例失败。精确结果及可执行脚本见 ZIP。
- 本机 Go 1.27.3 构建当前 #13 后端成功，包含新增 opsRepository 的 Wire 装配。测试/构建使用 `-ldflags=-w` 去掉验证二进制调试符号，不改变被测业务逻辑。
- 前端类型检查通过；视频任务页、视频 API 与 locale 完整性共 3 个文件 / 23 个测试通过。
- 不是全量重新审查或全量回归通过声明；真实 PostgreSQL/Redis 升级、完整 unit/Vitest、生产部署和付费上游调用未执行。
- 业务源码与现有工作分支保持原状。没有提交、推送、合并或部署；只生成报告与回归包，临时文件在交付前清理。
