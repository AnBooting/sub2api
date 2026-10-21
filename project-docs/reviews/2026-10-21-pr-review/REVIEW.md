# AnBooting/sub2api PR #5 / #7 / #8 / #9 / #10 审查

审查日期：2026-10-21（Asia/Singapore）。结论：**确认 6 项问题：4 个 P1、2 个 P2。**

状态在审查期间变化：#9/#5/#8 于 20:22 先后合并，#7 于 20:23 关闭，#10 于 20:37 合并。最终核对 develop 为 `b354fc31606e604d1fa0777cd90b4537a7b3fc06`：前五项问题涉及的 9 个源码文件与复现时的 PR head 完全一致；根 Dockerfile 也仍与 #9 一致。文件树与 blob hash 核对结果随包交付。**这些是已经合入 develop 的待修问题，不能再用“阻止合并”代替处理。** 本轮没有连接生产检查是否已经部署这些提交。

| PR | 精确 head | 结论与最终状态 |
|---|---|---|
| [#9 上游 v0.2.17 同步](https://github.com/AnBooting/sub2api/pull/9) | `b6c842d7e11123465207db544855e46634909ba0` | 已合并；旧库升级存在 P1，其迁移也已并入 #5/#8 |
| [#5 审计与 Live 结算](https://github.com/AnBooting/sub2api/pull/5) | `db822967b645fb46029abb909945ece4b90e4b6d` | 已合并；本轮新增 2 个 P1、1 个 P2，上轮三项已修复 |
| [#8 产品可见性与帮助](https://github.com/AnBooting/sub2api/pull/8) | `300eace889c14376fe7a8f0e7b02e9cfd3e32e4e` | 已合并；新增 P2 信息泄露，同时受共同迁移 P1 影响 |
| [#7 前端视觉层](https://github.com/AnBooting/sub2api/pull/7) | `d52241fdbf7da8b42d6a6e180e5d5c55865a19a0` | 已关闭未合并；head 与上轮一致，路由回归及类型检查通过 |
| [#10 Go 工具链升级](https://github.com/AnBooting/sub2api/pull/10) | `1361d5441acf3ec34361e0e27cff44848bb0a0de` | 已合并；新增 P1：遗漏根 Dockerfile 的工具链升级 |

最初四条 PR 的 base 均为 `d5c2b5edbcb0335cb80e4cfd7371098cf8982d8c`；#10 的 base 为 `973b91cb0350bf88ade5731064a7587aa8ee067b`。按源码快照去重后，#5 相对 #9 有 121 个不同路径，#8 相对 #9 有 144 个不同路径。审查重点是共同迁移、Gemini 缓存与预算护栏、Live 财务恢复、用户任务信息边界和新增前端流程；不将定向验证描述为数百文件的逐行全面安全证明。

## 1. [P1] 已部署迁移改名后会重新执行，旧库启动失败

影响：#9，以及已经并入同一迁移集合的 #5/#8。

位置：[251_native_currency_pricing.sql:2](https://github.com/AnBooting/sub2api/blob/b6c842d7e11123465207db544855e46634909ba0/backend/migrations/251_native_currency_pricing.sql#L2)、[迁移执行器:201](https://github.com/AnBooting/sub2api/blob/b6c842d7e11123465207db544855e46634909ba0/backend/internal/repository/migrations_runner.go#L201)。

将 fork 的 243–250 改名为 251–258，没有迁移历史兼容逻辑。执行器按完整 filename 查询已应用记录；数据库只有 `243_native_currency_pricing.sql` 时，新 `251_native_currency_pricing.sql` 被当作未执行，第二条 SQL 再次添加同名 `channel_model_pricing_currency_check`，返回 SQLSTATE 42710 并终止启动。旧 243 与新 251 的内容经本轮核对完全一致，但文件名不同足以触发重放。

这不是只针对假想环境：先前 PR #3 的部署聊天明确记录过迁移升至 250；本轮重新读取该聊天确认了这条历史记录，**没有连接生产数据库核实当前状态**。同步说明 §1.5 声称“尚未做过生产部署、任何环境均未执行”，与该记录冲突。文档中一句手工改名提示不能作为已部署实例的可执行升级流程。

证据：`TestReviewRenamedMigrationUpgradesExistingDevelopDatabase` 调用真实执行器与嵌入的 251 SQL。sqlmock 模拟旧库中没有新文件名、重复约束报错，执行器返回：

```text
apply migration 251_native_currency_pricing.sql:
constraint "channel_model_pricing_currency_check" ... already exists (SQLSTATE 42710)
```

建议：保留已发布迁移身份，或提供带旧名和 checksum 校验、备份、事务边界及重复执行验证的历史对齐步骤。在隔离的旧版数据库副本上验证从 250 升级后，再确定合并及部署顺序。不能仅验证空库。

## 2. [P1] observer 自己结束会话时没有启动 pending 扣费重试

影响：#5。

位置：[observeLiveCall:681](https://github.com/AnBooting/sub2api/blob/db822967b645fb46029abb909945ece4b90e4b6d/backend/internal/service/openai_live.go#L681)、[结束事件分支:699](https://github.com/AnBooting/sub2api/blob/db822967b645fb46029abb909945ece4b90e4b6d/backend/internal/service/openai_live.go#L699)。

新增自动重试只挂在“回读发现 controller 已经不是 observer”的分支。observer 直接遇到会话超时或 session ended 时，调用 `finalizeLiveCall(record)` 后立即返回。若此时账本 Apply 失败，记录虽然保留 pending，但当前 observer 已退出，重试函数不会被调用。兜底到期 finalize 路径也直接结束。对已关闭会话没有持久扫描器，待结算记录最终可随短 TTL 消失。

证据：`TestReviewLiveObserverExpiryAutomaticallyRetriesPendingSettlement` 以真实 observer 驱动一个已到期会话，并令账本暂时失败；把重试退避缩短至毫秒后，函数仍只调用 Apply **1 次**，未进入任何自动重试。

建议：让所有 finalize 出口统一交接 pending 重试，或由持久队列/扫描器接管。测试必须从 observer 生命周期入口触发，直接调用 `retryPendingLiveSettlement` 的既有测试不能证明入口已连接。

## 3. [P1] 关闭标记先落库，pending 检查点失败后再次结算被永久跳过

影响：#5；与上一项的自动重试入口遗漏是独立原因。

位置：[finalizeLiveCall:882](https://github.com/AnBooting/sub2api/blob/db822967b645fb46029abb909945ece4b90e4b6d/backend/internal/service/openai_live.go#L882)、[首次冻结与保存:899](https://github.com/AnBooting/sub2api/blob/db822967b645fb46029abb909945ece4b90e4b6d/backend/internal/service/openai_live.go#L899)、[persistLiveSettlement:975](https://github.com/AnBooting/sub2api/blob/db822967b645fb46029abb909945ece4b90e4b6d/backend/internal/service/openai_live.go#L975)。

流程先持久化 closed 标记，再冻结金额并保存 pending；保存失败只告警。若关闭成功后 Redis 保存持续失败，同时财务 Apply 未成功，恢复后从存储回读的记录为 closed 且没有 Settlement。第二次 finalize 得到 `first=false`、`retrying=false`，在第 888 行直接返回，补扣不可达。仅保留内存中的 pending 对跨副本/进程恢复无效；关闭与首次检查点之间的崩溃也存在同一状态缺口。

证据：`TestReviewLivePendingSurvivesCheckpointWriteFailure` 注入上述保存与账本错误，恢复两个依赖后从持久记录重新调用真实 finalizer，Apply 次数仍为 **1**，预期可恢复的第二次调用没有发生。

建议：将冻结金额、pending 和关闭状态原子写入；检查点失败必须保留可重新取得的结算责任。不要先写入会阻断重试的不可逆状态，再忽略恢复所需状态的写入失败。

## 4. [P2] 补扣成功后，用户用量日志仍永久显示零费用

影响：#5。

位置：[首次失败写零费用:937](https://github.com/AnBooting/sub2api/blob/db822967b645fb46029abb909945ece4b90e4b6d/backend/internal/service/openai_live.go#L937)、[成功后再写入:941](https://github.com/AnBooting/sub2api/blob/db822967b645fb46029abb909945ece4b90e4b6d/backend/internal/service/openai_live.go#L941)、[生产仓储的去重 INSERT:1208](https://github.com/AnBooting/sub2api/blob/db822967b645fb46029abb909945ece4b90e4b6d/backend/internal/repository/usage_log_repo_insert.go#L1208)。

第一次 Apply 失败时，先写同一 request_id 的 `ActualCost=0` 日志。重试成功后仍调用 Create/CreateBestEffort 写同一个键；生产仓储使用 `ON CONFLICT (request_id, api_key_id) DO NOTHING`，best-effort 最近键缓存也可能直接忽略重复写入。零费用行不会更新，但 Settlement 已标记 settled 和 UsageLogWritten，余额扣费与用户账单、汇总统计不一致。

证据：`TestReviewLiveRetryUpdatesDurableUsageAmount` 调用真实 finalizer，日志替身仅模拟生产唯一键的“首次写入保留”契约。账本补扣 **0.20**、状态 settled，但保留的日志仍为 **0**。这是服务层确定性复现与生产 SQL 核对，**未运行真实 PostgreSQL 测试**。

建议：把失败审计与最终财务用量行分开，或对 pending→settled 的同一行做显式条件更新；只有确认最终金额持久化成功，才能设置 UsageLogWritten。

## 5. [P2] 用户视频任务列表原样暴露内部 worker 错误

影响：#8。

位置：[用户 DTO:46](https://github.com/AnBooting/sub2api/blob/300eace889c14376fe7a8f0e7b02e9cfd3e32e4e/backend/internal/handler/dto/video_task.go#L46)、[查询 last_error:234](https://github.com/AnBooting/sub2api/blob/300eace889c14376fe7a8f0e7b02e9cfd3e32e4e/backend/internal/repository/video_task_repo.go#L234)。

新列表虽然省略 account_id/upstream_task_id 等独立字段，却把 `task.LastError` 原样下发。已有 worker 在读取原承载账号失败时保存 `load carrying account <内部账号ID> failed: <仓储错误>`；传输失败还会保存包装后的底层错误。用户查询自己的任务即可读到后台内部诊断信息，字段白名单因此没有达到其声明的隔离效果。

证据：`TestReviewVideoTaskWorkerErrorIsPrivateInUserResponse` 运行真实 worker，再经真实用户 DTO 序列化，响应包含：

```json
{"last_error":"load carrying account 99 failed: database temporarily unavailable"}
```

另有独立 DTO 负例。当前实测确认内部账号 ID 与仓储诊断泄露；**没有声称已复现 API Key 或密码泄露**。

建议：用户侧仅返回稳定错误码和可公开说明，完整 last_error 保留在管理员排障路径。

## 6. [P1] 工具链升级遗漏根 Dockerfile，默认镜像构建入口仍为 Go 1.27.2

影响：审查期间合入的 #10。

位置：[go.mod:3](https://github.com/AnBooting/sub2api/blob/1361d5441acf3ec34361e0e27cff44848bb0a0de/backend/go.mod#L3)、[合并后根 Dockerfile:11](https://github.com/AnBooting/sub2api/blob/b354fc31606e604d1fa0777cd90b4537a7b3fc06/Dockerfile#L11)。

#10 把 go 指令、backend/Dockerfile、deploy/Dockerfile 和 workflow 断言改为 1.27.3，但漏掉根目录 Dockerfile 的 `GOLANG_IMAGE=golang:1.27.2-alpine`。该入口的 backend-builder 在复制 go.mod 后执行 `go mod download`。官方 Go 镜像默认设置 `GOTOOLCHAIN=local`，禁止自动升级工具链；依据见 [docker-library/golang 官方模板](https://github.com/docker-library/golang/blob/master/Dockerfile-linux.template)。因此默认 `docker build .` 的后端阶段版本不满足模块最低要求。

证据：下载 #10 的实际 go.mod，使用与根镜像一致的本机 Go 1.27.2 及 GOTOOLCHAIN=local 执行 `go mod download`，实际返回：

```text
go: go.mod requires go >= 1.27.3 (running go 1.27.2; GOTOOLCHAIN=local)
```

另有可运行的 `review_toolchain.py` 直接核对根 Dockerfile 与模块最低版本。本轮未运行完整 Docker 镜像构建；故复现范围是实际 Go 最低版本检查与默认镜像配置核对。建议同时更新根 Dockerfile 的默认 builder 镜像，并验证根目录构建入口。

## 旧问题复验与验证范围

- #5 上轮的密钥总额度/窗口计量、简易模式误扣款、显式再次 finalize 不可重试，原三个顶层复现测试均通过。实际 Live command 的 quota 与 rate_limit 成本都为约 0.20，与标准计费 builder 一致。注意“手动再次 finalize 可重试”不等于 observer 自动恢复已正确接线。
- #5 更早的基础余额/订阅结算与 panic 最终访问日志用例通过；相关既有 Live/中间件测试通过。
- #7 路由相关 **6 个文件 / 63 项**通过，含真实 App + LoginView 回归；类型检查通过。
- #8 相关前端 **13 个文件 / 113 项**通过；类型检查通过。后端视频任务、定价控制生命周期、HTTP 与仓储定向回归均通过。
- #9 Gemini 缓存锁价、请求预算与迁移编号定向回归通过。
- #9 前端类型检查也通过。
- #5/#8/#9 后端 `go build -p 3/4 ./...` 均退出 0。
- 前五项新增失败测试共 **6 个顶层 Go 用例**；视频信息边界有 worker→DTO 和单独 DTO 两条负例。第六项另有失败的构建版本契约检查及实际 Go 命令证据。

交付 runner 已完整执行：PR #5/#8/#9 按上述缺陷预期退出 1，#7 路由复现退出 0，并确认临时测试自动删除。#10 工具链检查单独执行。

前端验证复用了本机现有 node_modules：#7 锁文件与本机一致，#8/#9 锁文件已有变化，**没有按新锁文件重新安装依赖**，所以相关通过结果属于本机依赖环境下的验证，不是新锁文件的干净安装或供应链验收。

前端生产构建**未通过**：#7/#8 的 Vite checker 子进程无法解析 `vue-tsc`，通过项目指定的 `corepack pnpm@9 run build` 重试时，脚本子进程又无法解析 `pnpm`。直接 Node 类型检查和 Vitest 均能运行；这次命令解析故障单独记为本机环境限制，未归因于 PR 业务改动，也没有关闭 checker 或改源码绕过验证。

未运行全量后端/前端测试套件、真实 PostgreSQL/Redis 集成、生产数据库升级、真实计费上游请求。未修改当前业务源码、执行 Git 命令、提交、推送、合并、部署或在 GitHub 发布评论。测试代码仅加入隔离源码快照，交付后清理。

复现包提供 manifest、README、runner 与测试源文件。测试预期在这些 SHA 上失败；本轮不包含修复补丁，修复及变异验证需在后续授权任务完成。
