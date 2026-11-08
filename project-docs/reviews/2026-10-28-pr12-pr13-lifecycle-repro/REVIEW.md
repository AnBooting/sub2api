# AnBooting/sub2api PR #11 / #12 / #13 复审

日期：2026-10-28（Asia/Singapore）。**#11 已合并；#12/#13 因新增 P2 暂缓。** 本轮针对 10 月 26 日复审后的修复增量进行审查，三条分支分别新增 12 / 18 / 18 个变动路径。上轮迁移断言、Live 诊断隐私和瞬时读错恢复均已修复；本轮发现现有生命周期测试遗漏隔离新增结算退避，实际使完整后端单测超时。

| PR | 审查 head | 本轮处理 |
|---|---|---|
| #11 上游同步 | `b3cfa54af867e731b2d526f2682a2ce51078b697` | 独立后端验证通过，已合并 |
| #12 审计修复 | `ed3029b71d27c96fa3db11d80d361461e6530407` | 暂缓：下述 P2 |
| #13 产品功能 | `24dd8af2e3378d2104405cb71d4ad80538f8d966` | 暂缓：下述 P2 |

审查开始时 develop 为 `4956d55982b134e615feb1e787e19846b1c9ddf0`。#11 是 #12/#13 的祖先；#12 与 #13 可干净试合并，所得树为 `31cac6b5fabc88e8492bc0a433c4e8aace3f24f6`，与本轮实际验证的 #13 树完全一致。没有把 GitHub 的空检查列表解释为 CI 成功。

## [P2] 隔离现有生命周期用例新增的结算退避，避免默认 unit 超时

影响 #12/#13，归属新增提交 `70592f234f56423ee80e75d092175758e1cbb598`。生产位置：`backend/internal/service/openai_live.go:829–830`；应同步的既有测试：`backend/internal/service/openai_live_lifecycle_test.go:508`。

新增的入口回读恢复循环会在 Redis 持续读错、调用方没有 pending 冻结状态时，依次请求 `5s / 30s / 2m / 5m` 的真实退避。既有 `TestObserveLiveCallStoreOutageFallsBackToExpiryFinalize` 只把 `liveObserverStoreRetryInterval` 改成 1ms，没有替换 `liveSettlementRetryInterval`。其“GetLiveCall 持续报错”子场景因此进入 **455 秒**的真实等待。

这不是根据耗时推测：本轮原样运行完整 `go test -p 2 -tags=unit -ldflags=-w ./... -count=1`，该原测试耗时 **455.17s**。service 包从 21:44:42 开始执行，最终在默认 10 分钟限制处实际出现 `panic: test timed out after 10m0s`，包结果为 **FAIL / 602.735s**。超时时正在运行的是后面的 WebSocket 回退测试，不能据此误判为 WebSocket 业务回归；此前多出的 455 秒消耗了共享的包级时间预算。

快速复现 `TestReviewExistingLifecycleCaseMustNotUseProductionSettlementBackoff` 直接调用这条既有测试，仅截获生产退避函数请求的时长、将实际睡眠变为零；不照抄 observer 或结算逻辑。实测记录 `[5s 30s 2m0s 5m0s]`，断言失败。随后仅通过 Go overlay 在原测试入口缩短结算时钟并注册 cleanup，同一复现转绿；生产重试逻辑没有变化。

建议在原生命周期测试中同时隔离结算退避并恢复全局函数，保留真实的故障注入和租约/用量断言；不要削减生产重试预算、删除用例或单纯增大整个单测超时来掩盖问题。修复后重新运行完整 service/unit 套件。

## 已验证的修复和验证范围

- #11 独立完整后端 unit **退出码 0**：62 个测试包通过，52 个无测试包，12,873 个顶层测试通过、18 个跳过；后端构建退出码 0。service 包耗时 218.246s，上述生命周期用例只耗时 0.01s。初次快照缺根目录构建文件造成的两项契约测试失败已通过补齐文件并完整重跑排除，未当成 PR 缺陷。
- #13 的迁移、Live、视频隐私关键回归共 **91 个顶层测试通过**，覆盖 3 个包。
- 迁移补记断言、checksum 护栏、旧历史缺失和目标列缺失回归通过。三条分支相关迁移文件相同。
- Live 原组合故障、费用/时长重复统计、用户列表和详情隐私、管理员完整诊断以及 Redis 瞬时读错恢复回归通过。#12/#13 的相关生产源码相同。
- 三项 Go overlay 变异：移除目标列验证、公开原始账本诊断、循环读错立即返回，对应三个顶层测试均失败；撤除 overlay 后全部重新通过。源码 blob 与审查 head 一致，未用变异源码构建交付物。
- #13 后端使用本机 Go 1.27.3 构建成功；完整 unit 结果因上述超时为失败，不能宣称后端全绿。
- #13 前端类型检查成功，完整 Vitest **427 文件 / 3416 测试通过**（含 i18n 完整性），Vite 生产构建成功。直接使用项目已有的本地工具，未使用全局 pnpm 11。
- 本机没有可用 Docker，未执行真实 PostgreSQL/Redis 集成升级；迁移负例采用真实执行器与 sqlmock，不能替代实际数据库演练。

## 最终合并与交付

#11 于 **2026-10-28 22:11:22（Asia/Singapore）** 通过带精确 head 检查的正常 merge 合入 AnBooting/sub2api 的 develop，合并提交为 `35803be54009ef6f6633447ad00cd04754c2fc37`。合并后重新 fetch 验证：develop 的 tree 为 `cfdff2b7860dcc893d2dc2ffa0ac7d5f4e40f753`，与独立构建、测试的 #11 head 完全一致。#12/#13 仍为 OPEN，head 未变化。

本轮审查和验证在独立快照进行；未修改现有业务源码，未切换当前工作分支，未部署或调用付费上游。快速复现随 `sub2api-pr-review-tests-20261028.zip` 交付；脚本已端到端实跑并验证 finally 删除注入文件。包内包含测试时钟的三行建议补丁，仅通过临时 overlay 验证，未应用到任何分支。临时快照、二进制、日志和 junction 在交付前清理，既有前端依赖与历史交付保留。
