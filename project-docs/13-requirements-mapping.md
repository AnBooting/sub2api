# 六项需求 × 现有代码 映射分析

> 目的：回答「哪些需求已经有实现、哪些要我们做」。
> 全部结论基于本仓库真实代码（分支 `develop` = AnBooting fork `0e6f5efb`）。
> 取证时间：2026-10-10。

## 0. 结论速览

| # | 需求 | 现状 | 归属 | 我们的动作 |
|---|---|---|---|---|
| 1 | Sub 价格获取，国模 RMB、Claude/GPT 美元 | ✅ **fork 已实现**（commit `11109acd`） | 别人在做 | **验证 + 补漏**，不重做 |
| 2 | 调度：部分账号不并发 / 并发过大 | ❌ 未做 | **我们** | 侦察中（task-2）→ 出方案 |
| 3 | 卡网嵌入网站、自动化充值 | ⚠️ 部分基础已有，卡网对接未做 | **我们** | 侦察中（task-4）→ 出方案 |
| 4 | 兜底分组（默认兜底 AWSB 分组） | ⚠️ **机制已存在**，配置与默认值待确认 | **我们** | 侦察中（task-4）→ 澄清需求 |
| 5 | DeepSeek 峰谷价 | ✅ **fork 已实现**（峰谷 schedule） | 别人在做 | **验证 + 补漏**，不重做 |
| 6 | 图片/视频生成不兼容（尤其视频） | ⚠️ 上游已有视频路由与计费字段，兼容性待查 | **我们** | 侦察中（task-3）→ 出方案 |

**核心结论：需求 2、3、4、6 归我们；需求 1、5 已由 fork 实现，我们只做验证与补漏。**

---

## 1. 需求 1：国模 RMB / Claude·GPT 美元计价 — ✅ 已实现

**证据**：commit `11109acd` "feat(pricing): add native CNY prices and credit billing controls"
（103 文件，+2219/-228）

**实现要点**（来自 `backend/resources/model-pricing/README.md` 与源码）：

| 项 | 实现 |
|---|---|
| 开关 | 系统设置 → 功能开关 → 模型价格与额度计费 → 「使用国模人民币价格源」；配置键 `pricing.native_cny_enabled`，**默认关闭** |
| 计价规则 | 国模按原始 CNY 单价，Claude/GPT 等按 USD 单价；**¥1 或 $1 都对应 1 额度，不换汇**，分组倍率继续生效 |
| 币种列 | 迁移 `251_native_currency_pricing.sql` 给 `channel_model_pricing` 加 `currency`（USD/CNY） |
| 快照 | `usage_logs.pricing_snapshot`（jsonb）记录实际采用的价格 |
| 主数据源 | `https://basellm.github.io/llm-metadata/api/all.json`（CNY 字段 / `currency_options.CNY`） |
| 补齐源 | `https://raw.githubusercontent.com/szp2005/llm-prices-cn/main/prices.json`（LLM Abacus，CC-BY-4.0） |
| 识别国模 | `isNativeCNModel()`：deepseek / qwen / qwq / qvq / glm / kimi / moonshot / minimax / doubao / ernie / hunyuan / spark / mimo / baichuan / step / longcat / bailing / sensenova |
| 缓存文件 | `pricing.data_dir/model_pricing_cny.json` |
| **缺价行为** | **拒绝转发**（符合我们的红线）；上游返回未定义缓存计量时**计费报错**，不记为免费 |
| 远程拉取开关 | 关闭后停止启动拉取/定时拉取/哈希校验/手动刷新，取消在途请求并拒绝迟到结果；开关存数据库 |
| 账号成本统计 | **仍用 USD**，人民币售价不直接累加为美元成本 |

**相关文件**：
- `backend/internal/service/pricing_native_cny.go`（481 行，新增）
- `backend/internal/service/pricing_controls.go`（127 行，新增）
- `backend/internal/service/pricing_snapshot.go`（34 行，新增）
- `backend/internal/handler/admin/pricing_controls_handler.go`（新增）
- `backend/migrations/251_native_currency_pricing.sql`

> **迁移号提示**：迁移号已随 0.2.17 上游同步整体重编号（原 243–250 → 现 **251–258**），映射表见 [上游同步说明](上游同步说明.md)；本 fork 下一个新迁移从 **259** 起。

**我们要做的**：不重做。做**验证与补漏**：
- [ ] 端到端验证：国模走 CNY、Claude/GPT 走 USD，账本数值正确
- [ ] 验证峰谷、上下文阶梯、缓存价、手动价卡优先级
- [ ] 验证国模缺价确实拒绝转发
- [ ] 检查 `pricing_snapshot` 落库内容是否完整
- [ ] 核对与国模移植包（`_reference/domestic-provider-kit`）的**价格口径冲突**——见下方 §7

### ⚠️ 重要：缺价行为按币种分裂（已核实源码）

| 路径 | 缺价时的行为 | 证据（已核实） |
|---|---|---|
| **国模（CNY）** | **fail-closed**：直接 `return err`，拒绝转发 | `service/openai_gateway_usage.go:253-258`；转发前还有 `ValidateNativePricingRequest`（`handler/gateway_inflight_reservation.go:76-82`，**即使余额预留开关关闭也无条件执行**） |
| **USD（Claude/GPT 等）** | **fail-open**：记 `pricing_missing_record_zero_cost` 警告后**按零成本放行** | `service/openai_gateway_usage.go:259-272` |

**这意味着**：项目红线「缺价不得放行」目前**只对国模成立**。USD 模型缺价会以**零成本**放行并只记一条 WARN 日志。

→ 这是**上游既有行为**，不是我们引入的。是否要改成 fail-closed，需要你决策（见 §9 问题 11）。

---

## 2. 需求 2：调度与并发 — ❌ 归我们（**根因已定位**）

**问题陈述**（你的原话）：「部分账号不并发，部分账号并发过大」

**根因（已核实源码）**：存在**「两个数」的结构性分裂**。

| 用途 | 使用的字段 | 代码位置 |
|---|---|---|
| **真实抢槽硬上限**（决定能不能并发） | `account.Concurrency` | `service/gateway_scheduling.go:177,369,483,569,754,801` |
| **选号负载率的分母**（决定会不会被选中） | `account.EffectiveLoadFactor()` | `service/gateway_scheduling.go:441` |

```go
// backend/internal/service/account.go:169  （已核实）
func (a *Account) EffectiveLoadFactor() int {
    if a.LoadFactor != nil && *a.LoadFactor > 0 { return *a.LoadFactor }  // 优先 load_factor
    if a.Concurrency > 0 { return a.Concurrency }                          // 回落 concurrency
    return 1
}
```

```go
// backend/internal/repository/concurrency_cache.go  （已核实）
loadRate := 0
if ac.maxConcurrency > 0 {
    loadRate = (currentConcurrency + waitingCount) * 100 / ac.maxConcurrency
}
```

**后果**：
- `load_factor > concurrency` → LoadRate 虚低 → 被优先选中 → **并发贴顶**
- `load_factor < concurrency` → LoadRate 虚高 → 被 `LoadRate < 100` 过滤掉（`gateway_scheduling.go:730`）→ **表现为「不并发」**

**另外两个已核实的关键事实**：

| 事实 | 证据 |
|---|---|
| `concurrency <= 0` 表示**完全不限制**（直接返回 Acquired，不写 Redis） | `service/concurrency_service.go:343-349` |
| `concurrency` 默认值 **3**；`load_factor` 是 `Optional().Nillable()`，**无默认值** | `ent/schema/account.go`、`migrations/001_init.sql:68` |

**其他加剧因素**（已核实）：
1. **粘性是流量集中的第一大结构性原因**：粘性 TTL 默认 3600s；高级调度器权重 `previous_response=5.0`、`session_sticky=3.0`，而 `load` 仅 **1.0**（`config.go:2494-2503`）。
2. **WS 长连接持续占用并发**：抢槽时 `count = ZCARD(普通槽) + ZCARD(Live 租约)`（`concurrency_cache.go:71-107`），Live 租约 TTL 60s。
3. **槽位泄漏最长占位 30 分钟**：槽位 TTL 默认 30 分钟（`config.go:2548`）。
4. **快照秒级延迟**：账号变更走 `scheduler_outbox`，默认 1s 轮询、5s 滞后告警、10s 强制重建（`config.go:2569-2571`）。**「改了并发数没生效」要先排除这层。**

**并发超限不是直接拒绝，是四段式**：换账号 → 指数退避等待（100ms→2s，±20% 抖动）→ 排队（sticky 3 / fallback 100）→ 最后才 429。

**管理入口已存在**：账号编辑 / 批量编辑里有「并发数」与「负载因子」两个字段（`BulkEditAccountModal.vue`），
文案是「提高负载因子可以提高对账号的调度频率」——**这正好印证了两者语义不同**。

**产出**：`project-docs/architecture/02-scheduling.md`（543 行，含 20 条「不并发」+ 14 条「并发过大」排查清单）

**⚠️ 注意**：以上是**代码层面的根因**。你的线上实际情况需要现场数据确认（Redis 槽位计数、账号字段实值）。

---

## 3. 需求 3：卡网嵌入网站、自动化充值 — ⚠️ 部分

**现状**：支付系统上游已内置（易支付/支付宝/微信/Stripe/Airwallex），**卡网对接未做**。

**已知相关**：
- `backend/internal/payment/`（provider 目录）
- `docs/ADMIN_PAYMENT_INTEGRATION_API.md`（对外集成 API）
- `docs/PAYMENT_CN.md`（支付配置指南）
- 兑换码机制（redeem codes）
- 幂等：`affiliate_ledger_operation_id`（迁移 240）

**待澄清**（需要你回答）：
1. 「卡网」指哪种？是**发卡平台**（如独角数卡、异次元发卡）还是**卡密批发站**？
2. 「嵌入网站」是要把 Sub2API 的充值页 iframe 嵌进你的站，还是反过来？
3. 「自动化充值」是**用户自助充值**（已有），还是**卡密自动发货 → 自动入账**？
4. 需要哪种对接方式：回调（webhook）/ 主动轮询 / 共用数据库？

**产出**：task-4 → `project-docs/architecture/04-payment-fallback.md`

---

## 4. 需求 4：兜底分组（默认兜底 AWSB 分组）— ⚠️ 机制已存在，但语义与直觉不同

**重要发现**：上游 `group` 表**已经有兜底分组字段**，但**触发条件与字段名暗示的不一样**（已核实源码）：

| 字段 | **真实触发条件** | 证据（已核实） |
|---|---|---|
| `fallback_group_id` | **非 [CC] 客户端**请求降级 | `service/gateway_scheduling.go:922-949`（`resolveGatewayGroup` 循环，带环检测） |
| `fallback_group_id_on_invalid_request` | **仅上游返回 prompt too long 时** | `handler/gateway_handler.go:1005-1054` |

⚠️ **字段名里的 "invalid request" 不是「分组 ID 无效」**。源码中**没有**「请求带不存在的分组 → 兜底」这条路径。

**所以「兜底分组」不是要新建的功能，而是配置问题。** `AWSB` 在代码里**没有任何字面量**（`git grep -i awsb` = 0 命中），说明它是**你部署里的一个分组名称**（可能是 AWS Bedrock 的缩写）。

**四种可能的语义**（需你确认是哪一种）：

| 语义 | 现状 | 需要做什么 |
|---|---|---|
| (a) 非 CC 客户端降级 | ✅ 已支持 | **纯配置**：给分组设 `fallback_group_id` |
| (b) 上游 prompt too long 兜底 | ✅ 已支持 | **纯配置**：给分组设 `fallback_group_id_on_invalid_request` |
| (c) 用户**没有分配任何分组**时 | ❌ 无全局默认，直接落 `PlatformAnthropic`（`gateway_scheduling.go:63-66`） | **需写代码**：加全局默认分组设置 |
| (d) 分组内**无可用账号**时兜底到别处 | ❌ 无（`gateway_handler.go:683-707` / `1070-1072` 直接失败） | **需写代码** |

**待澄清**：
1. 「默认兜底」是 (a) (b) (c) (d) 中的哪一种（或哪几种）？
2. 「AWSB」是什么的缩写？是分组名还是账号来源？
3. 要**全局默认**还是**每分组各自配置**？

**产出**：task-4 报告 + 需求澄清后出方案

---

## 5. 需求 5：DeepSeek 峰谷价 — ✅ 已实现

**证据**：`backend/internal/service/pricing_native_cny.go` 的 `NativeCNYCard.ratesAt()`

**实现能力**：

```go
type NativeWindow struct {
    NativeRates
    Weekdays   []int    // 星期几生效
    HourRanges []string // 时段，如 "00:30-08:30"
    Tiers      []NativeTier
}
type NativeSchedule struct {
    Timezone string  // 时区
    Windows  []NativeWindow
}
```

**已支持**：
- ✅ 按**星期**（`Weekdays`）
- ✅ 按**时段**（`HourRanges`）
- ✅ **跨天**时段：`(start > end && (minute >= start || minute < end))`
- ✅ **上下文阶梯**叠加（`Tiers`，按 `contextTokens` 匹配）
- ✅ **时区**（`time.LoadLocation`）

**另有一套分组级峰谷倍率**（不同机制，别混淆）：
`group.peak_rate_enabled` / `peak_start` / `peak_end` / `peak_rate_multiplier`
→ 作用于**分组倍率**，且注释明确「**不支持跨天**」（`backend/ent/schema/group.go:54-65`）。

**我们要做的**：验证 DeepSeek 的峰谷数据源是否正确解析、时段是否正确生效（尤其**跨天**与**时区**边界）。

---

## 6. 需求 6：图片/视频生成兼容性 — ⚠️ 归我们

**重要发现**：上游**已经有视频路由和视频计费字段**，不是从零开始——**但视频被硬编码为 Grok 专属**。

**已有的视频路由**（`backend/internal/server/routes/gateway.go:270-281`）：
```
POST /v1/videos
POST /v1/videos/generations
POST /v1/videos/edits
POST /v1/videos/extensions
GET  /v1/videos/generations/:request_id
GET  /v1/videos/generations/:request_id/content
GET  /v1/videos/:request_id
GET  /v1/videos/:request_id/content
...
```

**已有的视频计费字段**（`backend/ent/schema/group.go:134-156`）：
```
video_rate_independent      视频生成是否使用独立倍率
video_rate_multiplier       视频生成独立倍率
video_price_480p            480p 每秒价格
video_price_720p            720p 每秒价格
video_price_1080p           1080p 每秒价格
video_model_prices          按模型族和分辨率覆盖视频每秒价格
```

**已有图片能力**：
- 同步：`POST /v1/images/generations`、`/v1/images/edits`
- 异步：`/v1/images/generations/async`、`GET /v1/images/tasks/:task_id`
- 批量：`/v1/images/batches` 全套（提交/列表/查询/下载/取消/删除）
- 分组开关：`allow_image_generation`、`allow_batch_image_generation`
- 文档：`docs/ASYNC_IMAGE_TASKS.md`、`docs/BATCH_IMAGE_MVP.md`、`docs/seedance-api.md`

### ⚠️ 视频的四个致命缺口（已核实）

| # | 缺口 | 证据（已核实） |
|---|---|---|
| **V1** | **视频被硬编码为 Grok/xAI 专属**，非 Grok 分组的视频价被清空 | `migrations/220_clear_non_grok_video_generation_config.sql:1`：`-- Videos are Grok/xAI-only.` |
| **V2** | **无独立 `allow_video_generation` 开关**，复用 `allow_image_generation` | `migrations/220:3`：`-- ...not a separate allow_video_generation flag which was never applied on this branch.` |
| **V3** | **无本地任务表 + 被动结算**：只有客户端轮询到 `done+video.url` 才扣费 | `service/grok_media.go:431-440`（仅 Redis pending/billed 快照）；`docs/seedance-api.md:41` 原文：「不查询的任务不会自动结算」 |
| **V4** | **未知视频模型回落到 2K 图片价** | `billing_service.go:2385` `getDefaultVideoPrice` → `s.getDefaultImagePrice(model, ImageBillingSize2K)` |

**现有视频供应商**（只有两家，都是「原生协议透传 + 硬分支」）：

| 供应商 | 路由 | 计费口径 |
|---|---|---|
| Grok / xAI | `routes/gateway.go:270-281`（12 条） | 按秒 × 时长 × 数量（`CalculateVideoCost`） |
| Seedance / 火山 Ark | `routes/gateway.go:367-371` | 按上游 `usage.completion_tokens`（`service/seedance.go:150`） |

**对比图片**：图片有**三条完整链路**（同步 / 异步 Redis 24h TTL / 批量 PostgreSQL 三表 + Worker），
视频**一条都没有**——没有统一抽象、没有任务持久化、没有主动结算、没有对象存储。

**国模媒体链路无法从移植包摘录**：`_reference/domestic-provider-kit/core` 只有 4 个 Go 文件
（provider 身份 + 定价），**零媒体能力**。需独立设计。

**所以「视频兼容性不如 new」需要精确定位**：到底缺什么？

**待侦察**（task-3）：
- 视频支持的**上游厂商覆盖度**（Sora? Veo? Seedance? Grok? 可灵?）
- 各家**协议格式**差异（异步任务、轮询、内容获取）
- **流式/长任务**处理
- **存储与转存**（视频文件体积大）
- **前端**是否有视频生成界面
- 计费是否覆盖按秒/按时长

**产出**：task-3 → `project-docs/architecture/03-media.md`

---

## 7. ⚠️ 关键冲突：fork 的 CNY 计价 vs 国模移植包的价格政策

这是我们**必须先解决的架构冲突**：

| | fork（已实现，commit `11109acd`） | 国模移植包（`_reference/domestic-provider-kit`） |
|---|---|---|
| 国模计价 | **原始 CNY 单价**，¥1 = 1 额度 | 国内人民币标价数字**直接当 USD 数字**（1 元 = 1 美元） |
| 币种标识 | 新增 `currency` 列（USD/CNY） | 无币种概念，统一 USD |
| 价格来源 | 远程 CNY 目录（basellm + llm-prices-cn） | 源码内置快照目录 |
| 厂商维度 | 按**模型名前缀**识别（`isNativeCNModel`） | 按**分组/账号的 `domestic_provider` 字段** |
| 缺价行为 | 拒绝转发 | 拒绝（待定价状态） |
| 峰谷 | 支持（含跨天、星期、时区） | 支持（上海时区，内置规则） |

**冲突点**：
1. **两套「国模」识别机制**：一个按模型名，一个按分组字段。同时开启会打架。
2. **两套计价口径**：¥1=1额度 vs 1元=1美元当USD数字。数值上不同（除非汇率恰好为 1 的设定被有意统一）。
3. **两套价格数据源**：远程目录 vs 内置快照。
4. **迁移编号冲突**：fork 已占 **251–258**（原 243–250，已随 0.2.17 上游同步重编号）；上游占 243/244/245，**246–250 留空**，移植包的 196/197/198 需重编号到 **259+**。

**必须由你决策**（这是需求 1/5「有人在做」的核心问题）：

| 选项 | 含义 | 影响 |
|---|---|---|
| **A. 只用 fork 的 CNY 方案** | 放弃移植包的定价部分，只移植**分组/账号的厂商 UI 与校验** | 工作量小；移植包的价格目录作废 |
| **B. 只用移植包的方案** | 放弃 fork 的 CNY 计价 | 需要回退 fork 的 2 个提交，与「有人在做」冲突 |
| **C. 融合** | fork 的 CNY 引擎 + 移植包的**厂商维度**（把 `domestic_provider` 作为价格命名空间的一维） | 工作量最大，但语义最完整 |
| **D. 两者独立并存** | CNY 引擎管计价，`domestic_provider` 只管 UI 分类与绑定校验 | 中等；需明确「谁决定价格」 |

**我的建议**：**D 或 C**。理由：
- fork 的 CNY 引擎已经在跑，且有远程数据源，维护成本低；
- 移植包的独特价值是**厂商维度的分组/账号管理与绑定校验**（这正是上游没有的）；
- 两者职责可以切分：**定价归 CNY 引擎，分类与绑定归 `domestic_provider`**。

---

## 8. 建议的工作顺序

| 阶段 | 内容 | 前置 |
|---|---|---|
| **P0.5** | 完成 5 份架构侦察文档 + 架构图 | 进行中 |
| **P1** | 澄清需求 3/4 的细节问题（见 §3、§4） | 需要你回答 |
| **P2** | 决策 §7 的价格口径冲突 | 需要你拍板 |
| **P3** | 需求 2（调度并发）方案与实现 | P0.5 完成 |
| **P4** | 需求 4（兜底分组）配置/扩展 | P1 澄清 |
| **P5** | 需求 3（卡网对接）方案与实现 | P1 澄清 |
| **P6** | 需求 6（视频兼容）方案与实现 | P0.5 完成 |
| **P7** | 需求 1/5 的验证与补漏 | P2 决策 |
| **P8** | Docker 本地验证 → 生产部署 | 全部 + 服务器信息 |

---

## 9. 需要你回答的问题（汇总）

**关于需求 3（卡网）**：
1. 「卡网」是发卡平台还是卡密批发站？具体是哪一家？
2. 「嵌入网站」的方向？（Sub2API 嵌进你的站 / 你的站嵌进 Sub2API）
3. 「自动化充值」是卡密自动发货入账，还是别的？

**关于需求 4（兜底分组）**：
4. 「默认兜底」针对哪个场景？（无效分组 / 非 CC 请求 / 用户无分组）
5. 「AWSB」是什么？是分组名吗？
6. 要全局默认还是每分组配置？

**关于需求 6（视频）**：
7. 你说的「new」是指哪个项目/版本？（作为兼容性对标基准）
8. 主要用哪些视频模型？（Sora / Veo / Seedance / 可灵 / Grok / 其他）
9. 当前具体报什么错或不兼容？（有原始报错最好）

**关于价格口径（§7）**：
10. 选 A / B / C / D 哪个？

**关于缺价行为**：
11. USD 模型缺价目前是**零成本放行**（只记 WARN）。要不要改成和国模一样**拒绝转发**？（改了对线上是破坏性变更：任何价格目录没覆盖的模型都会开始报错）
