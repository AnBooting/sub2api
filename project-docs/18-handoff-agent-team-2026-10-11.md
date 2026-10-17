# 交接文档：新会话组建 Agent Team 继续实施（2026-10-11 23:45）

> 新会话先读本文件和 `AGENTS.md`，再按第 6 节组建团队。
> 本文件是快照，事实以 Git、源码和 `F:\sub2--evidence\` 原始日志为准；开工前用第 7 节命令复核。

## 1. 现状摘要

- ARTEX 专项：已完成，独审通过。没合入 develop，没推送，没部署。
- TS-01 C1 请求锁价：生产者已提交 3 个提交。第一个 HTTP 消费者切片（普通 OpenAI Responses 文本）已实现，v7 验证通过，但**还没提交**。另有 1 个 P1 漏扣费缺口没修，阻断 task-7 完成。
- TS-02 / TS-05 / 前端：部分成果在各自特性分支，没合入 develop。TS-02 策略与重试预算（task-6）、TS-04 HA（task-8）还没开工。
- 远端 origin（AnBooting/sub2api）上本团队的特性分支一个都没推。远端只有 develop、main、docs/baseline，以及同事的 fix/pricing-review-hardening（354a178d）。

## 2. 新会话用哪个分支

| 用途 | 目录 | 分支 | HEAD |
|---|---|---|---|
| 主线（C1 消费者、P1 修复） | `F:\sub2` | `feat/TS-01-request-locked-pricing` | `bc014ea56` + 未提交工作树 |
| ARTEX（已完成） | `F:\sub2--wt--artex-defense` | `feat/security-artex-specific-defense` | `cc0d8e10f`，干净 |
| 前端集成（已完成） | `F:\sub2--wt--ts02-ui` | `feat/core-ui-integration` | `df7f5fe2a`，干净 |

新会话工作目录：`F:\sub2`，分支 `feat/TS-01-request-locked-pricing`。
不要切 develop/main，不要 stash、reset、checkout .，否则会丢掉未提交的消费者实现。

## 3. Git 全景

### 3.1 远端

- `origin` https://github.com/AnBooting/sub2api.git：团队仓库，可推特性分支（推送前要用户同意）
- `upstream` https://github.com/Wei-Shaw/sub2api.git：上游，只读

### 3.2 本地分支（都从 `develop @ 6288d0ed4` 分出）

| 分支 | HEAD | 领先 develop | 内容 | 状态 |
|---|---|---|---|---|
| `develop` | `6288d0ed4` | — | 集成基线，等于 origin/develop | |
| `main` | `0e6f5efb7` | — | 本地 main 和 origin/main（6288d0ed4）不同，不要动 | 红线 |
| `fix/pricing-safety` | `65570a2f9` | 2 | CNY 卡完整性、DeepSeek 日历保留（task-11） | 内容已包含在 TS-01 链 |
| `feat/TS-02-group-chain-failover` | `e47345ca9` | 5 | 物理并发容量、组链持久化/鉴权/缓存（task-4/5） | 已包含在 TS-01 链 |
| `feat/TS-01-request-locked-pricing` | `bc014ea56` | 10 | 上面两条 + C1 生产者 3 个提交 | **主线，有未提交工作树** |
| `feat/security-artex-specific-defense` | `cc0d8e10f` | 10 | ARTEX 识别拦截 + 文档 | 完成 |
| `feat/TS-02-group-chain-ui` | `15fa1f20e` | 3 | 组链前端 | 已并入 core-ui-integration |
| `feat/TS-01-credits-usage-ui` | `e376e0947` | 3 | Credit 账务前端 | 已并入 core-ui-integration |
| `feat/TS-05-deepseek-calendar-ui` | `9902232fb` | 6 | DeepSeek 日历前端 | 已并入 core-ui-integration |
| `feat/core-ui-integration` | `df7f5fe2a` | 8 | 前端集成（task-16） | 完成 |
| `docs/baseline` | `0681f4141` | 基于 main | 脱敏基线文档 | 已在 origin |

TS-01 主线提交链（旧到新）：

```
f8659a021 fix(scheduling): enforce physical account concurrency capacity
b346273ed docs(prd): freeze group-chain concurrency and approved decisions
66d5afdf3 feat(keys): persist revisioned group chains and nullable routing audit
237f36e28 fix(keys): refresh group responses and isolate cached RPM pointers
e47345ca9 test(protocols): isolate catalog fixtures ...        <- TS-02 分支头
0c6d18465 fix(pricing): reject incomplete CNY cards and preserve DeepSeek periods
e2728ff2d fix(pricing): retain current holiday calendar ...    <- pricing-safety 内容
3e0d0da4a feat(pricing): freeze request-owned customer price catalogs and mappings
7583719b5 fix(pricing): reject absent price sources while preserving explicit zeros ...
bc014ea56 fix(pricing): retain context tier fallback for unmatched image sizes   <- HEAD
```

### 3.3 `F:\sub2` 未提交工作树（必须保留）

生产代码 11 个：

- `backend/internal/handler/gateway_handler.go`：`ErrModelPricingUnavailable` 映射为 503 `pricing_unavailable`
- `backend/internal/handler/openai_gateway_handler.go`：L571-590 `lockedText` 锁价门，已去掉 `!nativeV2` 绕过
- `backend/internal/service/billing_inflight_reservation.go`
- `backend/internal/service/gateway_request_pricing.go`
- `backend/internal/service/openai_gateway_forward.go`
- `backend/internal/service/openai_gateway_passthrough.go`
- `backend/internal/service/openai_gateway_responses_anthropic_native.go`
- `backend/internal/service/openai_gateway_responses_chat_fallback.go`
- `backend/internal/service/openai_gateway_usage.go`
- `backend/internal/service/openai_profit_control.go`
- `backend/internal/service/pricing_account_mapping_snapshot.go`

测试：3 个旧 fixture 修改 + 2 个新文件：

- `backend/internal/handler/openai_gateway_handler_test.go`：pool 3 个测试注入 scheduler snapshot + 四维价
- `backend/internal/handler/openai_responses_astra_pro_failover_test.go`：四维价
- `backend/internal/handler/openai_responses_failover_cancel_test.go`：scheduler snapshot + BillingService + 四维价
- 新 `backend/internal/handler/openai_responses_locked_pricing_test.go`（含 `usageOverride` 字段）
- 新 `backend/internal/service/gateway_request_pricing_consumer_test.go`

合计 14 个已跟踪文件 +417/-144，另 2 个新测试。快照：`F:\sub2--evidence\20261011-core\pricing-c1-http-consumer-working-tree-v7.diff`。

不能提交的未跟踪项：

- `ARTEX/`：嵌套的 ARTEX 源码仓库（只读分析对象，HEAD `e6ec5699`）。不要 git add、clean 或运行。
- `.tmp-investigate-session.jsonl`（约 300KB，20:29 生成，像会话转录）：来源没确认。不要提交，是否删除由用户决定。

## 4. 已完成与已验证

### 4.1 ARTEX 专项（task-19/20/21）

- 拦截条件：system/developer/instructions 中出现完整的 ARTEX 代码内置尾部，**并且**同一角色族 3 个工具声明都匹配（名称、schema 类型、required 集合）。W/P/M 三族，HTTP 和 WS 都覆盖。
- 配置 `gateway.artex_guard.mode` = off / monitor / enforce，默认 enforce。
- 文档（在 ARTEX 分支）：`project-docs/security/ARTEX-targeted-defense.md`、`ARTEX-defense-report.md`。
- 证据：`F:\sub2--evidence\20261011-artex\`。
- 没覆盖：没有真实 ARTEX 线上抓包；没有 Norma SDK 原始包；不保证识别改造变体；不承诺零误伤；没在生产启用。

### 4.2 C1 生产者（已提交，独审通过）

- 请求开始时冻结价格版本、来源、倍率、账号映射、长上下文开关；重试复用同一份数据。
- 缺价拒绝；显式零价保留；四维价格（input/output/cache_read/cache_write）都必须显式存在，nil 不算免费。
- 证据：`C1-producer-validation.md`、`pricing-c1-required-final-*-v3.log`。

### 4.3 C1 HTTP 消费者第一切片（未提交，v7 已验证）

范围：普通 OpenAI `/responses` 文本请求（normal、raw-chat fallback、passthrough）。native-anthropic 分支只有静态接线，没有专项 wire 测试。

| 验证项 | 结果 | 证据 |
|---|---|---|
| 定向回归（原 7 个失败 + 5 个新消费者测试） | 全过 | `pricing-c1-http-consumer-handler-regression-v7.log` |
| `./internal/handler` 全包 | ok 46.6s | `pricing-c1-http-consumer-handler-full-v7.log` |
| `go vet -tags=unit ./...` | exit 0 | `pricing-c1-http-consumer-vet-v7.log` |
| `go build ./cmd/server` | exit 0 | v7 记录（前台运行，无单独日志） |
| 全量 `go test -tags=unit ./...` | **仍有失败**，已归因见 4.4 | `pricing-c1-http-consumer-full-unit-v7.log` |

已证明的行为：8 种缺价情况都在上游前返回 503，上游调用 0 次；native-v2 带 `compaction_trigger` 也绕不过锁价；显式零价能正常转发；两账号重试保持冻结的请求 ID、版本、开始时间和模型，只扣一次（0.0024）；新请求用新价格（0.048）。

汇总记录：`F:\sub2--evidence\20261011-core\C1-http-consumer-validation-v7.md`。

### 4.4 全量 unit v7 剩余失败

| 失败 | 归因 | 处理 |
|---|---|---|
| `TestPgDumper*` 3 个 | 本机 PATH 没有 `sh`；加 `E:\Git\usr\bin` 后通过 | 跑测试时 PATH 带 Git usr/bin |
| `TestIsDeepSeekModel` | 已提交链里的既有失败（`0c6d18465` 一带）：`nativeModelKey` 去掉连字符后 `deepseekcoder` 也匹配，和测试期望冲突；干净 HEAD 也复现 | **要修**，见 5.1-B |
| WS 两个测试（StoreEnabled…、Forward_WSv2_ImageGeneration…） | 全量高负载下超时，单独跑通过 | flaky，复跑留证据 |
| `TestServerTimingConnector…`（v4 出现） | 负载敏感，v7 通过 | flaky |

因此现在**不能**说全量 unit 通过。

## 5. 接下来做什么（按优先级）

### 5.1 P0：收口 C1 消费者第一切片并提交

**A. 修 P1 漏扣费（阻断项）**

- 现象：`lockedText` 请求带图片输入时，上游 usage 里的 `input_tokens_details.image_tokens` 被解析成 `ImageInputTokens`（`openai_gateway_response_handling.go:1548-1551`），`openai_gateway_usage.go:212-213` 按"非文本 usage"拒绝计费。结果：客户端拿到 200，上游已经服务，ledger 没有扣费命令。v7 已在运行时复现。
- 已核实：`billing_service.go:1697-1729` 的 `computeTokenCost` 本来就支持 `ImageInputTokens` / `ImageOutputTokens` 计价，这个拒绝没有必要。
- 建议修法：把 212 行的拒绝条件收窄到按次计数类（`ImageCount`、`VideoCount`、`AudioUsage`、`WebSearchCalls`、`SearchCount`），token 类图片维度放行进锁价计算。
- 同时要处理：按次计数类如果只在响应后拒绝，同样会出现"已服务、零扣费"。要么在 admission 阶段（上游前）排除，要么给出明确风险说明并登记。
- 必补测试：`newLockedHTTPFixture` + `usageOverride={"input_tokens":1000,"output_tokens":500,"input_tokens_details":{"image_tokens":200,"cached_tokens":0}}` + 带 `input_image` 的 body，断言 ledger 实际扣费且金额正确。

**B. 修 `TestIsDeepSeekModel` 既有失败**

- 位置：`isDeepSeekModel` / `nativeModelKey`。先按 PRD-05 确认 `deepseek-coder` 是否属于峰谷计费范围，再决定改实现还是改测试期望。不能直接删断言。

**C. 收口验证后提交**

1. `go test -p 2 -tags=unit ./... -count=1`（PATH 带 `E:\Git\usr\bin`），除已确认的 flaky 外全部通过；flaky 单独复跑留证据。
2. `go build ./cmd/server`、`go vet -tags=unit ./...` 都 exit 0，日志写入 `F:\sub2--evidence\`。
3. reviewer 只读审这次 diff 和原始日志，给出 Approve。
4. 逐个路径 `git add` 本切片文件，不用 `git add .`，不带 `ARTEX/` 和 `.tmp-investigate-session.jsonl`。
5. 提交后在 `F:\sub2--evidence\20261011-core\` 写新的验证记录。

**D. 测试改进（登记，不阻断）**

- `fakeSchedulerCache` 不按分组过滤，浅拷贝共享 Credentials/Extra，和生产序列化快照不完全一样。建议做按分组过滤的 fake。
- 覆盖缺口：`BillingModelSource` 的 Requested/Upstream/Response 分支、真实 composite 分组、订阅分组、有价无分组、显式零价时 ledger cost=0 的断言。

**E. 运营提醒（写进发布文档）**

- 只配了 input/output、没配 cache 价的旧分组，在新消费者下会直接 503。上线前需要管理员补全四维价格，或准备检查脚本。

### 5.2 P1：锁价消费者扩展到其它入口（task-7 剩余）

每个入口都要满足：请求开始锁价；缺价在上游前拒绝；重试复用冻结数据；按实际账号结算，只扣一次。

1. OpenAI WS `/responses`：每个 turn 开始时建立冻结数据。
2. Anthropic `/v1/messages`（`gateway_handler.go`）。
3. Chat Completions 入口。
4. Gemini 入口。
5. native-anthropic 分支补专项 wire 测试。
6. legacy compact。
7. 媒体 / 非文本专用锁价消费者（和 task-18 一起做）。
8. v2 快照 + C6：使用记录和扣费在同一事务里可靠落地。
9. C2 价格同步。

### 5.3 P1：TS-02 策略与重试预算（task-6，等 task-7）

- 分组策略配置；全局安全重试预算：最多 16 次尝试、600 秒总预算。
- 每次切到别的分组都要重新检查权限、订阅和额度限制。
- 跨组开放必须和"请求开始价 + 实际组一次结算"一起验收。

### 5.4 P2：TS-04 用户主动授权 HA 兜底（task-8，等 task-6）

后端 HA 兜底分组 + 前端用户授权界面。

### 5.5 P2：媒体通用显式报价与缺价门（task-18，等 task-7）

### 5.6 被外部资料阻塞（不要编造接口或价格）

- TS-03 云猫充值中心：缺 Q-4。
- TS-06 图片：缺 Q-7；真实视频：缺 Q-8。

### 5.7 集成与合并（最后做，要用户确认）

- 建议顺序：`feat/TS-01-request-locked-pricing`（已包含 TS-02 后端和 pricing-safety）→ `feat/security-artex-specific-defense` → `feat/core-ui-integration`，合到 develop。ARTEX 分支和 TS-01 分支都改了 `openai_gateway_handler.go`，合并时要处理冲突。
- 同事成果（`origin/fix/pricing-review-hardening @ 354a178d`，以及之前提到的 `11109acd9` / `0e6f5efb7`）拿到具体分支/SHA 后做三方合并，不能互相覆盖。
- 推送特性分支、开 PR、合并 develop 都先问用户。
- 没有 Docker/PG/Redis 时 `-tags=integration` 无法验证，部署前要补。

## 6. 团队编排建议

### 6.1 角色与写域

| 成员 | 职责 | 写域 |
|---|---|---|
| Lead（主会话） | 拆任务、跑所有重型 Go 命令、提交、最终验收 | 提交、`F:\sub2--evidence\` |
| `pricing-impl` | 5.1-A P1 修复，然后 5.2 其它入口 | `openai_gateway_usage.go`、`gateway_request_pricing*.go`、`billing_*`、`pricing_*`、`openai_gateway_*`、`handler/openai_*` |
| `deepseek-fix` | 5.1-B `TestIsDeepSeekModel` | DeepSeek 识别相关文件，开工前 grep 确定，不能和 pricing-impl 重叠 |
| `scheduler-impl` | 5.3 TS-02 策略/预算（等 task-7 提交后） | 独立 worktree，例如 `F:\sub2--wt--ts02-policy`，从 TS-01 新提交分出 |
| `reviewer` | 每个切片提交前只读审 | 只读 |

### 6.2 工作规则

- 同一工作树只有一个写者；要并行就开独立 worktree。
- 子代理只改代码、跑 gofmt 和 `git diff --check`；`go test` / `go build` / `go vet` 都由 Lead 跑，一次只跑一个重型作业。
- 每个切片：实现 → 定向测试 → 全包 → 全量 → reviewer 只读审 → Lead 提交。
- 失败日志原样保留，不能用后面的成功覆盖前面的失败。
- 旧会话任务板（task-1…21）不会带到新会话，按第 5 节重新建任务。

### 6.3 新会话开场提示词

```
读 F:\sub2\project-docs\18-handoff-agent-team-2026-10-11.md 和 F:\sub2\AGENTS.md。
工作目录 F:\sub2，分支 feat/TS-01-request-locked-pricing，保留所有未提交改动。
先跑交接文档第 7 节复核命令，确认状态和文档一致。
然后组建 Agent Team，按第 5 节优先级执行：先修 P1 图片 token 漏扣费和 TestIsDeepSeekModel，
收口验证并经独审后提交 C1 消费者第一切片；再推进其它入口锁价消费者和 TS-02 策略/重试预算。
遵守第 8 节红线。
```

## 7. 开工前复核命令

```powershell
cd F:\sub2
git branch --show-current     # feat/TS-01-request-locked-pricing
git rev-parse --short HEAD    # bc014ea56（有新提交就以实际为准）
git status --short            # 14 个 M + 2 个新测试 + ARTEX/ + .tmp-investigate-session.jsonl
git diff --stat               # 14 files, +417/-144
git worktree list             # 3 个 worktree

$env:GOROOT='F:/tools/go1.27.2/go'; $env:GOPATH='F:/tools/gopath'; $env:GOTOOLCHAIN='local'
$env:GOPROXY='https://goproxy.cn,direct'
$env:PATH='F:/tools/go1.27.2/go/bin;E:/Git/bin;E:/Git/usr/bin;'+$env:PATH
cd backend
go test -p 2 -tags=unit ./internal/handler -run 'TestOpenAIResponsesLockedPricing' -count=1
```

## 8. 红线

1. 不在 main 提交；不推送、不部署、不合并 develop，除非用户明确同意。
2. 缺价模型必须在请求上游之前拒绝（503 `pricing_unavailable`）；不按零价放行，不用 GPT 兜底价。
3. 客户端不能有任何方式绕过锁价门（`nativeV2` / compact 只保留原有传输行为，不免价）。
4. 显式零价不能被跳过；nil 不算免费。
5. passthrough 账号不能加普通 `model_mapping`；`SelectedAccountID` 只记录实际执行的账号。
6. 不新造价格引擎，不做双重价格门，不用复制 Group 代替锁价。
7. 不提交 `.env`、`data/`、凭据；不读真实配置里的秘密。
8. `F:\sub2\ARTEX` 只读：不运行、不扫描、不装依赖、不 git add、不 clean。
9. 不 stash / reset / clean 用户的脏工作树；不整文件覆盖上游共享文件。
10. 同事修复等拿到具体 SHA/分支再三方合并。
11. 原始失败日志（v4/v5/v6 等）保留；没跑过的检查不说通过。

## 9. 文档索引

- 需求：`project-docs/prd/`（00 路线、PRD-01…06、12 技术规格、14 测试计划、20 执行计划、90 评审问题）
- 架构：`project-docs/architecture/01-request-billing.md`、`02-scheduling.md`、`03-media.md`
- 流程：`project-docs/04-git-workflow.md`、`11-collaboration-norms.md`、`12-worktree-strategy.md`、旧交接 `15-handoff.md`
- 证据：`F:\sub2--evidence\20261011-core\`（C1）、`F:\sub2--evidence\20261011-artex\`（ARTEX，含 `git-recovery/`）
