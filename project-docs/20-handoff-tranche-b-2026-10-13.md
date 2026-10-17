# 交接文档：Tranche B 完成（Agent Team，2026-10-13）

> 本文件是快照；事实以 Git、源码和 F:\sub2--evidence\trancheB\ 原始日志为准。
> 开工基线：F:\sub2 @ 0fe6de512、F:\sub2--wt--ts02-policy @ 217deda1b。

## 1. 本次交付（全部已提交本地，未推送、未合并 develop、未部署）

| 提交 | 分支 / 工作树 | 内容 |
|---|---|---|
| 48ca1fd7e | feat/TS-01-request-locked-pricing (F:\sub2) | 六项产品契约决策写入 90 号 + C-5 keepalive/attempt_count 措辞 |
| aa2c1f49e | 同上 | C1 S5：媒体/按次维度锁价消费者（冻结分派，禁止回源取价） |
| 6131a91bb | 同上 | review F1/F2/F3：独立图片/视频倍率、注释与 WARN、视频口径差异声明 |
| 90f5865bd | 同上 | C1 S3：native-anthropic wire 测试（recon 3.4 五条） |
| b18c0e499 | 同上 | review G1-G5：gofmt、去 unused、fail-closed 断言、分支鉴别力、legacy 期望值 |
| a29d13407 | feat/TS-02-policy-retry-budget (F:\sub2--wt--ts02-policy) | TS-02 S5：C-5 FailureClass 五分类 + Classify() |
| a0aae8915 / 1487fe9b8 | 同上 | TS-02 S3b：预算挂到 E1-E6 与 E8-E10、embeddings/alpha-search；媒体/codex 豁免 |
| a1f94e1d7 | 同上 | review F1-F4：预算出口 503 no_available_group、账号归因隔离、WSv2 计数、退避钳制 |
| 5abc8ef93 | 同上 | review F1-a/F1-b/F2/F4：退避耗尽保留预算 reason、E3/E8 带 code、健康守卫、调度等待钳制 |
| b3d8a084f | 同上 | review R1：非预算错误不再输出空 code 字段 |
| 41afb567d | 同上 | 合并 TS-01（媒体消费者 + native 测试）进 TS-02 |
| 42929b7aa / 97ce76944 | feat/TS-02-ws-locked-pricing (F:\sub2--wt--ts02-ws) | C1 S4：WS 每 turn 锁价（重冻、账号绑定、图生回退、ingress 口径同源）+ F1/F2 修复 |
| e3251f380 | ts02 工作树 | 合并 WS 分支进集成分支 |

## 2. 已验证（证据原文在 F:\sub2--evidence\trancheB\）

| 验证 | 结果 | 日志 |
|---|---|---|
| 集成分支 gofmt / build / vet | 全绿 | integrated-build.log / integrated-vet.log |
| 集成分支全量 unit（-p 2） | **exit 0 全包 ok** | integrated-fullunit.log |
| ts02 分支全量 unit（合并前） | exit 0 | ts02-fullunit-v1.log |
| TS-01 分支全量 unit | exit 1，仅 TestInflightEstimate_AccountMappingNoDBAndBoundedMemory；隔离复跑 exit 0（高负载假失败，与 19 号文档记录的同类 flaky 一致） | c1-fullunit-final.log / ts01-flaky-isolate.log |
| C1 媒体消费者 | 定向 25 pass / 0 fail；冻结后改实时价仍按冻结价；独立倍率 0.25 vs 0.5、0.07 vs 0.35 | c1-rework-tests.log / c1-rework-indeprate.log |
| C1 native wire | 5 测试全 PASS；requestID 绑定冻结 bundle | native-v1.log |
| WS 锁价 | 四套回归全绿；身份映射换模型 served-but-unbilled 已修并有回归用例 | ws-*.log |

## 3. 独立审查结论

所有切片均由未参与实现的 reviewer 只读复审后 Approve：
- C1 媒体消费者 + native wire：reviewer-c1 两轮（第二轮 Approve）。
- TS-02 S3b + S5：reviewer-ts02 三轮（b3d8a084f 最终 Approve，明确写『S3b 可提交/可验收』）。
- C1 WS 锁价：reviewer-ts02 收口 Approve（明确写『WS 锁价切片可验收』）。

审查过程中发现并修复的真实缺陷（均以回归用例钉死）：
1. 媒体锁价丢分组独立图片/视频倍率 → 实扣 2 倍超收（F1，High）。
2. E1-E6/E8 预算耗尽出口是 502 + Ops 上游错误，而非 503 no_available_group（F1，High）。
3. embeddings/alpha-search 无 ShouldRetryNextAccount 判断，预算拒绝仍在换号并记账号健康失败（F2，High）。
4. E7 的 WSv2 上游传输完全不计预算（F3，Medium）。
5. 退避吃满预算后的终止仍透出上一次真实上游错误（F1-a，Medium）。
6. 新增 WithCode writer 给约 50 条非预算错误响应插入空 code 字段（R1，Medium）。
7. WS 出站模型回填用连接级 ctx → passthrough + 身份映射 + turn>=2 换模型时 served-but-unbilled（F1，High）。

## 4. 六项产品契约决策（已写入 90 号 Q-10 / 11 号 C-5）

1. WS 预算每 turn 独立 16/600，连接内重连 5s 与 600s 取小。
2. 客户端更早 deadline 本期不实现（只 start+600s）。
3. keepalive/心跳不计入“累计首字节不重放”。
4. 媒体 handler 不挂预算；文本类 embedding/alpha-search 仍挂。
5. Gemini 参与组链、SystemOne 不参与。
6. attempt_count = budget.Used()，含 service 隐藏重试。

## 5. 未完成 / 未验证边界（必须说清）

1. **未跑 integration / E2E**：需 PostgreSQL + Redis，本地未起；所有结论限于 unit 层。
2. 未推送远端；未合并 develop；未部署。
3. TS-02 S6 组循环、S7 C-6 落库、S8 三策略、S9 HA、前端管理页字段：未开工（本轮范围外）。
4. 每 turn 一次定价目录全量克隆（pricing_catalog_snapshot.go）未做耗时基准，长连接高频 turn 的 CPU/GC 开销未量化。
5. 换号重绑与 worker 异步传递 bundle 只有 service 层用例，缺 handler 级端到端断言。
6. 预热写帧计入 16 次额度（默认 prewarm_generate_enabled=false）未写进契约文字。
7. native-anthropic 锁价消费路径仍受 PlatformOpenAI 准入门限制（CN 平台准入未扩，属独立产品决策）。
8. E8 流式（streamStarted=true）预算出口不带 C-7 code，已在代码注释写明理由（Anthropic SSE 帧不含 code）。

## 6. 工作树状态

| 工作树 | 分支 | 状态 |
|---|---|---|
| F:\sub2 | feat/TS-01-request-locked-pricing | 干净（另有用户未跟踪文件，非本会话产物） |
| F:\sub2--wt--ts02-policy | feat/TS-02-policy-retry-budget | 干净，含全部 TS-02 + 合并进的全部 TS-01 + WS |
| F:\sub2--wt--ts02-ws | feat/TS-02-ws-locked-pricing | 干净，已合入 ts02 |
| F:\sub2--wt--ts02-ui | feat/core-ui-integration | 未改动 |

## 7. 下一步建议

1. 由用户确认六个契约决策是否需要调整（每项都是独立小切片，可单独回退）。
2. 起 PostgreSQL + Redis 跑 `go test -tags=integration ./...` 与其他集成面。
3. 决策是否推送特性分支 / 合并 develop / 部署。