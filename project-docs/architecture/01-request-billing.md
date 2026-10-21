# 01 · 请求链路与计费定价（后端侦察）

## 状态

| 项 | 值 |
|---|---|
| 状态 | **已完成（只读侦察）** |
| 侦察对象 | `F:\sub2`，分支 `develop`，HEAD = `9150a7a86`（baseline on fork v0.2.15） |
| 代码基线 | `3a6fd1c9`（上游 Wei-Shaw/sub2api v0.2.15）+ `11109acd` + `0e6f5efb` 两个魔改提交 |
| 是否修改源码 | **否**。仅新增本文件 `project-docs/architecture/01-request-billing.md` |
| 证据来源 | 源码 `read`/`grep`、`git show <sha> -- <path>`、`git log`、`backend/resources/model-pricing/README.md` |
| 未确认项 | 见文末「未确认项」章节（7 条） |

> 命名说明：本文出现的 Go 标识符按源码真实拼写书写（例如 `OpenAIGatewayService`、`OpenAIRecordUsageInput`、
> `isOpenAIResponsesCompatibleGatewayPlatform`）。工具输出中该类标识符可能被显示层改写，**以文件为准**。

---

## 1. 一次 API 调用的完整链路

以 `POST /v1/messages`（Anthropic 兼容入口，走 Claude 网关）为例，逐段列出真实路径与函数名。

### 1.1 路由注册与中间件链

| 步骤 | 位置 | 关键函数 / 对象 |
|---|---|---|
| 1. 注册 | `backend/internal/server/routes/gateway.go:22` | `RegisterGatewayRoutes(r, h, apiKeyAuth, apiKeyService, subscriptionService, opsService, settingService, compositeResolver, cfg)` |
| 2. 体积限制 | `routes/gateway.go:33` | `middleware.RequestBodyLimit(cfg.Gateway.MaxBodySize)`；文本端点用 `cfg.Gateway.TextMaxBodySize`（:34） |
| 3. 客户端请求 ID | `routes/gateway.go:35` | `middleware.ClientRequestID()` |
| 4. Ops 错误日志 | `routes/gateway.go:36` | `handler.OpsErrorLoggerMiddleware(opsService)` |
| 5. 入口端点归一化 | `routes/gateway.go:37` | `handler.InboundEndpointMiddleware()` |
| 6. **API Key 鉴权** | `routes/gateway.go:185` | `middleware.NewAPIKeyAuthMiddleware`（`server/middleware/api_key_auth.go:21`）→ `apiKeyAuthWithSubscription`（:34） |
| 7. 分组模型白名单 | `routes/gateway.go:187` | `middleware.GroupModelAllowlist()`（`server/middleware/group_model_allowlist.go:32`） |
| 8. Composite 目标平台解析 | `routes/gateway.go:188` | `compositeTargetPlatformMiddleware`（`routes/gateway.go:543`） |
| 9. 未分组 Key 拦截 | `routes/gateway.go:189` | `middleware.RequireGroupAssignment(settingService, middleware.AnthropicErrorWriter)`（构造于 :42） |

`/v1` 组在 `routes/gateway.go:180-189` 建立；`GET /v1/sub2api/billing` 挂在白名单中间件之前（:186），
即余额/倍率查询不做分组模型白名单准入。

### 1.2 Handler 内主流程（`GatewayHandler.Messages`）

`backend/internal/handler/gateway_handler.go:123` `func (h *GatewayHandler) Messages(c *gin.Context)`

| 顺序 | 行号 | 动作 |
|---|---|---|
| 1 | :125 | `middleware2.GetAPIKeyFromContext(c)` 取鉴权结果（中间件已写入 context） |
| 2 | :131 | `GetAuthSubjectFromContext(c)` 取用户主体 |
| 3 | :146 | `readLenientJSONRequestBodyWithPrealloc` 读请求体 |
| 4 | :164 | `service.ParseGatewayRequest(bodyRef, domain.PlatformAnthropic)` 解析 model / stream / max_tokens |
| 5 | :191 | `h.gatewayService.ResolveChannelMappingAndRestrict(ctx, apiKey.GroupID, reqModel)` 解析渠道级模型映射 |
| 6 | :214 | `service.WithGatewayTokenRequestPricing(ctx)` 钉住本次请求的计费时点 `pricingAt` |
| 7 | :247 | `h.concurrencyHelper.AcquireUserSlotWithWait(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted)` 用户级并发槽位 |
| 8 | :260 | `h.billingCacheService.CheckBillingEligibility(ctx, user, apiKey, group, subscription, service.QuotaPlatform(ctx, apiKey))` 余额/订阅/配额预检 |
| 9 | :271 | `reserveInflightBalance(...)` → `handler/gateway_inflight_reservation.go:45` 在途余额预留（含国模定价前置校验，见 §6） |
| 10 | :292 | `h.gatewayService.GenerateSessionHash(parsedReq)` 粘性会话 hash |
| 11 | :317 | `GetCachedSessionAccountID` 查询粘性绑定的账号 |
| 12 | 之后 | `SelectAccountForModelWithExclusions` / `SelectAccountWithLoadAwareness` 选账号并抢占账号槽位 |
| 13 | 之后 | 上游转发（`service/gateway_forward.go` 等） |
| 14 | :599 | `h.submitUsageRecordTask(ctx, func(ctx){ h.gatewayService.RecordUsage(ctx, &service.RecordUsageInput{...}) })` 异步落账 |

> 各入口 handler 的 `CheckBillingEligibility` / `reserveInflightBalance` / `RecordUsage` 调用点分布见 §1.4。

### 1.3 上游 → 用量记录 → 计费

| 阶段 | 位置 | 关键函数 |
|---|---|---|
| 选账号 | `service/gateway_scheduling.go:34` / `:100`；`service/openai_gateway_scheduling.go:1129` | `SelectAccountForModelWithExclusions` / `SelectAccountWithLoadAwareness` / `selectAccountWithLoadAwareness` |
| 抢账号槽位 | `service/gateway_scheduling.go:1181`；`service/openai_gateway_scheduling.go:1532` | `tryAcquireAccountSlot` |
| 上游转发 | `service/gateway_forward.go`、`service/openai_gateway_forward.go`、`service/gateway_upstream_request.go`、`service/gateway_upstream_response.go` | `ForwardRequest` 系列 |
| 用量落账（Claude 网关） | `service/gateway_usage_billing.go:648` | `func (s *GatewayService) RecordUsage(ctx, input *RecordUsageInput) error` → `recordUsageCore`（:764） |
| 用量落账（OpenAI 网关） | `service/openai_gateway_usage.go:154` | `func (s *OpenAIGatewayService) RecordUsage(ctx, input *OpenAIRecordUsageInput) error` |
| 费用计算 | `service/billing_service.go:1516` | `CalculateCostUnified(input CostInput) (*CostBreakdown, error)` |
| 落库 + 扣费 | `service/gateway_usage_billing.go:353` | `applyUsageBilling(ctx, requestID, usageLog, p, deps, repo)` → `repo.Apply(billingCtx, cmd)` |
| 降级兜底扣费 | `service/gateway_usage_billing.go:146` | `postUsageBilling`（仅 unified repo 为 nil 时，注释见 :143-145） |
| 扣费后缓存同步/通知 | `service/gateway_usage_billing.go:392` | `finalizePostUsageBilling`（订阅/余额缓存、平台配额、低余额通知） |
| 写 usage_logs | `repository/usage_log_repo_insert.go` | `prepareUsageLogInsert` / `createSingle` / `buildUsageLogBestEffortInsertQuery` |

`RecordUsage` 返回错误时的行为（`openai_gateway_usage.go:526-530`）：把 `usageLog.ActualCost` 置 0 后仍尽力写一条用量记录，再把错误向上抛。
这意味着**「上游已成功但计费失败」会留下一条 0 费用的 usage 行 + 一个错误**。

### 1.4 各入口的计费钩子分布（`grep CheckBillingEligibility`）

| 入口文件 | CheckBillingEligibility | reserveInflightBalance | RecordUsage |
|---|---|---|---|
| `handler/gateway_handler.go` | :260、:1031（fallback key）、:2209 | :271 | :600、:966 |
| `handler/gateway_handler_chat_completions.go` | :143 | :154 | :364 |
| `handler/gateway_handler_responses.go` | :147 | :158 | :347 |
| `handler/openai_gateway_handler.go` | :609、:1258、:2667、:2692 | :620、:1269、:2675 | :831、:1425、:3165 |
| `handler/openai_chat_completions.go` | :139 | :150 | :296 |
| `handler/openai_images.go` | :136 | :147 | :408 |
| `handler/grok_media.go` | :166 | :177 | :761 |
| `handler/grok_audio.go` | :38、:195 | :53、:228 | :348 |
| `handler/gemini_v1beta_handler.go` | :383 | :394 | :729 |
| `handler/gateway_web_search.go` | :82 | :92 | :244 |
| `handler/openai_x_search.go` / `gateway_systemone.go` | — / :102 | — / :112 | — / :262 |
| `handler/openai_embeddings.go` | :101 | :112 | :275 |
| `handler/openai_alpha_search.go` | :103 | :113 | :298 |
| `handler/openai_gateway_count_tokens.go` | :76、:252 | —（count_tokens 不预留） | — |
| `handler/openai_live.go` | :84 | — | — |

---

## 2. 网关入口清单

所有网关路由集中在 `backend/internal/server/routes/gateway.go` 的 `RegisterGatewayRoutes` 内。

| 入口 | 路由前缀 / 路径 | 路由文件行号 | 鉴权中间件 | Handler |
|---|---|---|---|---|
| Anthropic / Claude 兼容 | `/v1/*`（messages / models / usage / count_tokens / responses / chat/completions / embeddings / web_search / x_search） | `gateway.go:180-335` | `apiKeyAuth`（:185） | `h.Gateway.*`（`handler/gateway_handler.go`） |
| OpenAI 兼容（Responses / Chat / Images / Live / Embeddings / AlphaSearch / Seedance） | `/v1/*` 同名路径，按分组平台分流 | `gateway.go:192-334` | 同上 | `h.OpenAIGateway.*`（`handler/openai_gateway_handler.go`、`openai_chat_completions.go`、`openai_images.go`、`openai_live.go`、`openai_embeddings.go`、`openai_alpha_search.go`、`seedance.go`） |
| Gemini 原生 | `/v1beta/models`、`/v1beta/models/:model`、`/v1beta/models/*modelAction` | `gateway.go:338-352` | `middleware.APIKeyAuthWithSubscriptionGoogle`（:343） | `h.Gateway.GeminiV1BetaListModels` / `GeminiV1BetaGetModel` / `GeminiV1BetaModels`（`handler/gemini_v1beta_handler.go:36/220/283`） |
| Grok（xAI） | 复用 `/v1`：`/v1/images/*`、`/v1/videos*`、`/v1/tts`、`/v1/stt`、`/v1/custom-voices*`、`/v1/realtime`、`/v1/web_search`、`/v1/x_search` | `gateway.go:80-81、92-107、283-334` | 同 `/v1` | `h.OpenAIGateway.GrokImages` / `GrokVideoGeneration` / `GrokVoice` / `GrokRealtime`；`h.Gateway.WebSearch` / `XSearch`（`handler/grok_media.go:21/30/54`、`handler/grok_audio.go:24/185`、`handler/gateway_web_search.go:30`、`handler/openai_x_search.go:26`） |
| 根路径别名（无 `/v1` 前缀） | `/api/v3/...`、`/v3/...`、`/v1/...`、`/...`（`contents/generations/tasks`、`responses`、`chat/completions`、`images/*`、`videos/*`、`tts`、`stt`、`web_search` 等） | `gateway.go:364-482` | `rootRoute` 统一挂 `apiKeyAuth + groupModelAllowlist + compositeTarget + requireGroupAnthropic`（:364-366） | 同上 |
| Codex 直连 | `/backend-api/codex/*` | `gateway.go:381-393` | `apiKeyAuth`（:382） | `h.OpenAIGateway.Live` / `LiveSideband` / `Responses` / `ResponsesWebSocket` / `AlphaSearch` / `CodexModels` |
| Antigravity（Claude 协议） | `/antigravity/v1/*`、`/antigravity/models` | `gateway.go:485-502` | `middleware.ForcePlatform(PlatformAntigravity)` + `apiKeyAuth` | `h.Gateway.Messages` / `CountTokens` / `AntigravityModels` / `Usage` |
| Antigravity（Gemini 协议） | `/antigravity/v1beta/*` | `gateway.go:504-517` | `ForcePlatform` + `APIKeyAuthWithSubscriptionGoogle` | `h.Gateway.GeminiV1Beta*` |

**平台分流规则**（`routes/gateway.go:49-52、530-541`）：

- `isOpenAIResponsesCompatibleGatewayPlatform(c)` = `domain.UsesOpenAIGateway(getGroupPlatform(c))`，决定 `/v1/messages`、`/v1/responses`、`/v1/chat/completions` 走 `h.OpenAIGateway` 还是 `h.Gateway`。
- `getGroupPlatform(c)`（:530）在 `PlatformComposite` 时读 `service.ResolvedTargetPlatformFromContext(ctx)`，即先由 `compositeTargetPlatformMiddleware` 改写模型并解析目标平台。

---

## 3. 计费链路：文件职责与调用关系

### 3.1 职责表

| 文件 | 职责 | 关键函数 / 类型 |
|---|---|---|
| `service/billing_service.go` | 计费**计算内核**：定价解析入口、token/按次/图片/视频/音频/搜索成本计算、服务档倍率、长上下文阶梯、缓存分档、CNY 分支 | `ModelPricing`(:98)、`CostBreakdown`(:205)、`CalculateCostUnified`(:1516)、`calculateTokenCost`(:1572)、`computeTokenBreakdown`(:1633)、`calculatePerRequestCost`(:1794)、`getModelPricingAt`(:1339)、`getUSDModelPricingAt`(:1346)、`GetModelPricingWithChannel`(:1414)、`applyChannelTokenPriceOverrides`(:1450) |
| `service/gateway_usage_billing.go` | Claude 网关的**落账编排**：倍率解析、计费模型选定、扣费命令构造与原子应用、缓存同步、通知 | `GatewayService.RecordUsage`(:648)、`recordUsageCore`(:764)、`calculateRecordUsageCost`(:934)、`calculateTokenCost`(:1127)、`buildUsageBillingCommand`(:288)、`applyUsageBilling`(:353)、`finalizePostUsageBilling`(:392)、`postUsageBilling`(:146)、`QuotaPlatform`(:115) |
| `service/openai_gateway_usage.go` | OpenAI 网关的**落账编排**：候选计费模型循环、response_model 计费、Free-Fast 按 Standard 重估、Codex 额度快照、CNY fail-closed | `OpenAIGatewayService.RecordUsage`(:154)、`calculateOpenAIRecordUsageCost`(:566)、`calculateOpenAIRecordUsageTokenCost`(:718)、`calculateOpenAIImageCost`(:747)、`calculateOpenAIVideoCost`(:804)、`hasIdentifiedOpenAIResponsePricing`(:542)、`filterCNProviderBillingModelCandidates`(:938) |
| `service/model_pricing_resolver.go` | **统一定价解析器**，解析链 `Group → Channel → LiteLLM(catalog) → Fallback`（注释 :49） | `Resolve`(:73)、`resolveConfiguredPricing`(:132)、`matchGroupModelPricing`(:151)、`resolveBasePricing`(:175)、`lookupChannelPricingNormalized`(:195)、`applyTokenOverrides`(:232)、`hasChannelTokenOverrides`(:278)、`GetIntervalPricing`(:333)、`PricingSource*` 常量(:12-17) |
| `service/pricing_service.go` | **定价目录服务**：LiteLLM/USD 目录的下载、哈希同步、fallback/override 叠加、模型名匹配、内存缓存与热重载 | `Initialize`(:302)、`checkAndUpdatePricing`(:394)、`syncWithRemote`(:457)、`downloadPricingData`(:613)、`loadPricingData`(:1086)、`buildPricingData`(:1074)、`useFallbackPricing`(:1179)、`GetModelPricing`(:1241)、`GetIdentifiedModelPricing`(:1315)、`startUpdateScheduler`(:344)、`ForceUpdate`(:1743) |

### 3.2 调用关系（一次 token 计费请求）

```
handler (e.g. GatewayHandler.Messages)
  └─ billingCacheService.CheckBillingEligibility(...)          # 余额/订阅/配额预检
  └─ reserveInflightBalance(...)                               # 在途余额预留 + 国模定价前置校验
  └─ gatewayService.RecordUsage(...)  [异步任务]
        └─ recordUsageCore
              ├─ ResolveUserGroupRateMultiplier(...)           # 用户专属 > 分组 > 系统默认
              ├─ computePeakAwareMultipliers(...)              # 高峰因子（token 倍率）
              ├─ 选定 billingModel（渠道映射 / requested / upstream / composite）
              ├─ calculateRecordUsageCost
              │     └─ billingService.CalculateCostUnified(CostInput{...})
              │           ├─ Resolver.Resolve(...)  → ResolvedPricing
              │           │     ├─ matchGroupModelPricing(group, model)      # 分组价卡优先
              │           │     ├─ channelService.GetChannelModelPricing(...) # 渠道价卡
              │           │     └─ getModelPricingAt / getUSDModelPricingAt   # 目录 / fallback
              │           ├─ calculateTokenCost / calculatePerRequestCost
              │           │     ├─ resolveNativeTokenPricing(...)  # CNY 时段+阶梯重取价
              │           │     ├─ computeTokenBreakdown(...)      # 逐项费用
              │           │     └─ applyCostBreakdownMultiplier(...)
              │           └─ 返回 *CostBreakdown{PricingCurrency, PricingSource, PricingSnapshot, TotalCost, ActualCost}
              ├─ costPricingSnapshot(cost)                     # → usage_logs.pricing_snapshot
              ├─ applyUsageBilling(...) → repo.Apply(cmd)      # 原子扣费 + 去重
              └─ finalizePostUsageBilling(...)                 # 缓存同步、平台配额、低余额通知
```

---

## 4. 定价来源（USD 目录）

### 4.1 内置目录位置与加载

| 项 | 值 |
|---|---|
| 内置 fallback 目录 | `backend/resources/model-pricing/model_prices_and_context_window.json`（LiteLLM 镜像副本，说明见同目录 `README.md`） |
| 配置默认值 | `backend/internal/config/config.go:2350`：`pricing.fallback_file = ./resources/model-pricing/model_prices_and_context_window.json` |
| 远程主源 | `pricing.remote_url` 默认 `https://raw.githubusercontent.com/Wei-Shaw/model-price-repo/...`；`pricing.hash_url` 为 sha256 锚点 |
| 本地缓存文件 | `pricing.data_dir` 默认 `./data`，目录文件由 `getPricingFilePath()`（`pricing_service.go:1761`）拼接 |
| 运行期叠加 | `pricing.fallback_file` + `pricing.override_file`（`applyPricingOverrides` :957、`loadPricingOverrideEntries` :979、`mergePricingOverrideEntry` :1003） |

加载流程（`pricing_service.go`）：

1. `Initialize()`(:302) 建数据目录 → `loadNativeCNYCache()`(:309) → `checkAndUpdatePricing()`(:310)。
2. `checkAndUpdatePricing()`(:394)：本地文件不存在 → `downloadPricingData()`；存在 → `loadPricingData()`，再按远程 hash 或文件年龄决定是否重下。
3. `loadPricingData()`(:1086) → `buildPricingData()`(:1074)（含 override/fallback 叠加）→ 写入内存 `s.pricingData` + `s.localHash` + `s.lastUpdated`。
4. 下载失败 → `useFallbackPricing()`(:1179) 把内置文件复制到数据目录并加载。
5. `startUpdateScheduler()`(:344) 起后台 ticker：按 `pricing.hash_check_interval_minutes` 周期做 `syncWithRemote()` + `reloadIfCustomFilesChanged()`(:555)。

### 4.2 解析与匹配

| 函数 | 行号 | 行为 |
|---|---|---|
| `GetModelPricing(modelName)` | `pricing_service.go:1241` | ① 确定性识别（精确键 / 拼写变体 / 去日期后缀，`lookupIdentifiedModelPricingLocked` :1275）② Claude 系列模糊匹配 `matchByModelFamily`(:1443) ③ `gpt-*` 走 `matchOpenAIModel`(:1577) |
| `GetIdentifiedModelPricing(modelName)` | :1315 | 只做确定性识别，**不含**系列兜底；用于「上游自报模型」准入判断 |
| `buildModelLookupCandidates` | :1329 | 生成候选：原名 / 去 `models/` / 末段 / 归一化名 |
| `buildPricingData` | :1074 | 解析 + override 合并 + fallback 合并（`mergeFallbackPricingData` :1119） |
| `parsePricingData` | :689 | LiteLLM 原始 JSON → `LiteLLMModelPricing`；处理 `above_*k_tokens` 长上下文阶梯（:815 `deriveLongContextFromAboveTierFields`） |

### 4.3 缓存机制

| 层 | 实现 |
|---|---|
| 内存目录缓存 | `PricingService.pricingData map[string]*LiteLLMModelPricing`（`pricing_service.go:278`），读写由 `s.mu sync.RWMutex`(:277) 保护 |
| 磁盘缓存 | `data_dir` 下的目录 JSON（`getPricingFilePath`），下载/回退时写入 |
| 哈希锚点 | `s.localHash`（sha256 全文）；远程 `hash_url` 不一致才重下（:473-477） |
| 自定义层热重载 | `customPricingFilesFingerprint`(:510) 对 fallback/override 做「长度前缀+正文」联合 sha256；变化才 `reloadCustomPricingLayers`(:570)，并用前后指纹一致性防止读到半写文件 |
| CNY 缓存 | `s.nativeCNYCards` + 磁盘 `<data_dir>/model_pricing_cny.json`（见 §5.1） |
| 上游账号/分组缓存 | `service/api_key_auth_cache_pricing_test.go` 显示分组快照携带定价；分组结构新增字段需同步失效缓存（见项目 AGENTS.md 边界） |

---

## 5. fork 的两个魔改提交

### 5.1 `11109acd` — feat(pricing): add native CNY prices and credit billing controls

规模：103 files，+2219/-228。核心是**新增「国模原始人民币价」并行定价源**与**额度（credits）计费口径**。

#### 5.1.1 新增文件

| 文件 | 行数 | 作用 |
|---|---|---|
| `backend/internal/service/pricing_native_cny.go` | 494 | CNY 目录抓取/解析/校验、时段+阶梯取价、内存缓存 |
| `backend/internal/service/pricing_controls.go` | 139 | 两个开关（远程拉取 / 国模 CNY）的持久化与并发代际控制 |
| `backend/internal/service/pricing_snapshot.go` | 36 | `costPricingSnapshot`：把原币种与实际单价序列化进 `usage_logs.pricing_snapshot` |
| `backend/internal/service/billing_context_schedule.go` | 新增 21 行（CNY 分支） | 模型广场/价格展示的上下文阶梯，CNY 直接由 `card.ratesAt` 生成 |
| `backend/internal/service/billing_inflight_reservation.go` | 新增 50 行 | 在途预留 + `ValidateNativePricingRequest` 国模定价前置校验 |
| `backend/internal/handler/admin/pricing_controls_handler.go` | 50 | `GetPricingControls` / `UpdatePricingControls` / `RefreshPricing` |
| `backend/internal/handler/pricing_currency.go`、`handler/admin/pricing_currency.go`、`repository/pricing_currency.go` | 各 8 | 币种归一化：只认 `CNY`，其余一律 `USD` |
| `backend/internal/repository/pricing_snapshot.go` | 8 | `nullablePricingSnapshot` |
| `backend/migrations/251_native_currency_pricing.sql` | 4 | 见 §5.1.5 |
| `frontend/src/components/admin/PricingSettingsPanel.vue` | 90 | 开关面板 |
| `frontend/src/utils/billingCredits.ts` | 12 | `formatBillingAmount` 在额度模式下渲染「N 额度」而非 `$N` |

#### 5.1.2 CNY 目录来源

`pricing_native_cny.go:15-16`：

```go
const nativeCNYDefaultURL = "https://basellm.github.io/llm-metadata/api/all.json"
const nativeCNYSupplementDefaultURL = "https://raw.githubusercontent.com/szp2005/llm-prices-cn/main/prices.json"
```

- 配置项：`pricing.native_cny_url` / `pricing.native_cny_supplement_url`（`config/config.go:2351-2352`，默认值与常量一致）。
- URL 白名单：`security.url_allowlist.pricing_hosts` 追加 `basellm.github.io`、`llm-metadata.pages.dev`（`config/config.go:2107-2109`，`deploy/config.example.yaml` 同步）。
- 主源解析 `parseNativeCNYCatalog`(:140)：只遍历**国内原生端点** provider 白名单
  `deepseek, zhipuai, alibaba-cn, moonshotai-cn, minimax-cn, volcengine, xiaomi, stepfun, stepfun-ai, bailing, longcat, sensenova, tencent-tokenhub`（:163），
  跳过 `subscription=true` 的 provider（:165），且 `currency != "CNY"` 时必须存在 `currency_options.CNY`（:176-182），否则丢弃。
  一条都没解析到则报错 `native catalog has no explicit valid CNY token prices`（:189）。
- 补充源解析 `parseNativeCNYSupplement`(:194)：只接受 `billing_currency == "CNY"`、非 `retired`、且 `isNativeCNModel(id)` 的条目（:217）；
  明确**忽略** `input_price_cny_per_m` 这类美元换算字段（只用 `*_price_orig_per_m`，见 :201-203 与测试 `pricing_native_cny_test.go:79-84`）。
- 国模判定 `isNativeCNModel`(:65)：按 `nativeModelKey`(:52)（小写、去 `vendor/` 前缀、仅保留字母数字）匹配前缀
  `deepseek, qwen, qwq, qvq, glm, kimi, moonshot, minimax, doubao, ernie, hunyuan, spark, mimo, baichuan, step, longcat, bailing, sensenova`。
- 合并策略（`syncNativeCNY` :354）：主源为准；补充源只在 key 不存在时补齐（:408-412）。补充源失败时保留上次补充源条目（:421-431），并把错误记到 `nativeCNYError`（:443-445）。

#### 5.1.3 峰谷 schedule 结构与 `ratesAt()`

结构体（`pricing_native_cny.go:19-50`）：

```go
type NativeRates struct { Input, Output, CacheRead, CacheWrite, Reasoning *float64 }  // 原始 CNY / 百万 token
type NativeTier  struct { NativeRates; Tier struct{ Size int; Type string } }         // Type 只允许 "context"
type NativeWindow struct { NativeRates; Weekdays []int; HourRanges []string; Tiers []NativeTier }
type NativeSchedule struct { Timezone string; Windows []NativeWindow }
type NativeCNYCard struct { NativeRates; Tiers []NativeTier; Schedule *NativeSchedule; Provider, Source string; FetchedAt time.Time }
```

`ratesAt(contextTokens int, at time.Time) NativeRates`（:271-313）算法：

1. 基线 = `card.NativeRates`（卡级 flat 价）+ `card.Tiers`。
2. 若 `Schedule != nil`：用 `time.LoadLocation(Timezone)` 把 `at` 转本地（:274-275），算当日分钟数 `hour*60+minute`（:276）。
3. 遍历 `Windows`：`Weekdays` 命中 `now.Weekday()` 才算当天（:279-286）；`HourRanges` 逐段 `"HH:MM-HH:MM"` 判定，
   支持跨零点（`start > end` 时按 `minute >= start || minute < end`，:292）；`24:00` 由 `nativeMinute`(:261) 特判为 1440。
4. **命中第一个窗口即 break**（:301）：用 `mergeNativeRates`(:246) 把窗口价覆盖到基线上（只覆盖非 nil 字段），
   若窗口带 `Tiers` 则整组替换卡级 Tiers（:298-300）。
5. 阶梯：把 tiers 按 `Tier.Size` 升序（:305-306），对每个 `contextTokens > tier.Size` 的档**依次叠加**覆盖（:307-311）。

校验 `validNativeCard`(:95)：卡级必须有 input+output；tier 的 `Type` 必须是 `"context"`；
窗口必须有 `Weekdays` 与 `HourRanges`；timezone 必须可加载；`Reasoning` 若存在必须等于 `Output`（:88-92，网关把 reasoning token 计入 output，独立费率必须走手工卡）。

取价入口：

| 函数 | 行号 | 说明 |
|---|---|---|
| `nativePricingAt(model, at)` | :450 | 无卡即 `ErrModelPricingUnavailable: no original CNY price for X`；否则 `ratesAt(0, at)` 生成 `ModelPricing{Currency:"CNY", PricingSource: card.Source, NativeCard: card, NativePricingAt: at, CacheCreationPriceExplicit: true}` |
| `resolveNativeTokenPricing(p, tokens, at)` | :477 | 按**实际输入总量** `Input+CacheRead+CacheCreation` 重新 `ratesAt`（:487），克隆后覆盖单价；若用到了 cache 读/写但该档没有对应价，返回 `CNY cache price is missing`（:488-490） |
| `applyNativeRates` | :462 | 元/百万 → 元/token（`/1e6`）；图片输入/输出价显式置 0（:474-475） |
| `RequiresNativeCNY(model)` | :75 | `GetControls().NativeCNYEnabled && isNativeCNModel(model)` |

#### 5.1.4 开关（credit billing controls）与并发控制

`pricing_controls.go`：

| 项 | 值 |
|---|---|
| 持久化 key | `SettingKeyPricingControls = "model_pricing_controls"`（:10），存 settings 表，JSON `{"remote_enabled":bool,"native_cny_enabled":bool}` |
| 默认值来源 | 配置文件 `pricing.remote_enabled`（`*bool`）/ `pricing.native_cny_enabled`（`config/config.go:670-673`），DB 优先 |
| 加载 | `loadControls`(:69)：无值时把当前默认**写回 DB**（:86-92）；装配在 `service/wire.go:ProvidePricingService`，读失败直接启动失败（注释「Never fall back to enabled remote access on a settings read failure」） |
| 更新 | `UpdateControls`(:97)：写 DB → `remoteGeneration++` → 取消所有在途请求 → 替换内存 controls |
| 在途取消 | `beginRemoteRequest`(:119) 返回 `(ctx, finish, generation)`；每个出网请求（`downloadPricingData`、`fetchRemoteHash`、`syncNativeCNY`）提交前检查 `RemoteEnabled && generation == s.remoteGeneration && ctx.Err()==nil`，否则 `ErrPricingPullDisabled`（:118-124、`pricing_native_cny.go:418-420`、`pricing_service.go:651-656`） |
| 路由 | `routes/admin.go:787-789`：`GET/PUT /api/v1/admin/channels/pricing/settings`、`POST /api/v1/admin/channels/pricing/refresh` |
| 公开设置 | `BillingCreditsEnabled` 进入 `PublicSettings`（`setting_public.go:303-305`、`settings_view.go:330`）与 HTML 注入 payload（`setting_public.go:569、664`）；开关变更后 `NotifyPublicSettingsUpdate()`（`setting_service.go:388`，调用点 `pricing_controls_handler.go:35`） |
| 前端 | `billingCreditsEnabled` ref + `formatBillingAmount`（`frontend/src/utils/billingCredits.ts:4-11`）；面板 `PricingSettingsPanel.vue` |

#### 5.1.5 落库：`pricing_snapshot` 与 `currency` 列

迁移 `backend/migrations/251_native_currency_pricing.sql`：

```sql
ALTER TABLE channel_model_pricing ADD COLUMN IF NOT EXISTS currency varchar(3) NOT NULL DEFAULT 'USD';
ALTER TABLE channel_model_pricing ADD CONSTRAINT channel_model_pricing_currency_check CHECK (currency IN ('USD', 'CNY'));
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS pricing_snapshot jsonb;
```

| 环节 | 位置 | 说明 |
|---|---|---|
| Ent schema | `backend/ent/schema/usage_log.go:186-188` | `field.JSON("pricing_snapshot", json.RawMessage{}).Optional()` |
| 领域字段 | `service/usage_log.go:105` | `UsageLog.PricingSnapshot []byte` |
| 写入 | `repository/usage_log_repo_insert.go` `prepareUsageLogInsert` | `nullablePricingSnapshot(log.PricingSnapshot)`；`usageLogInsertArgTypes` 末尾追加 `"jsonb"`；单条/批量/best-effort 三类 INSERT 均加列 |
| 读取 | `repository/usage_log_repo_query.go` `usageLogSelectColumns` / `scanUsageLog` | SELECT 列表与扫描目标均追加 `pricing_snapshot` |
| 序列化内容 | `service/pricing_snapshot.go:7-36` | `currency`（空则 USD）、`source`（空则 default）、`quota_unit="credits"`、`total_cost`、`actual_cost`；有 `PricingSnapshot` 时追加 `input_per_token`、`output_per_token`、`cache_read_per_token`、`cache_write_per_token`、`cache_write_1h_per_token`；有 `NativeCard` 时追加 `fetched_at`、`provider` |
| 调用点 | `service/openai_gateway_usage.go:424`、`service/gateway_usage_billing.go:1253` | 落账时写入 |
| 对用户暴露 | `handler/dto/types.go` `UsageLog.PricingSnapshot json.RawMessage`（`omitempty`） | 历史行无快照 → 保持 nil |

#### 5.1.6 `currency` 如何影响结算

数据流（**币种只决定「用哪套原始单价」，不改变扣费单位**——扣费永远是 credits 数字）：

```
channel_model_pricing.currency  (迁移 251)
  └─ repository: storedPricingCurrency()  (repository/pricing_currency.go:3)   # 只保留 CNY，其余写 USD
  └─ service.ChannelModelPricing.Currency  (service/channel.go:91)
        └─ ModelPricingResolver.applyTokenOverrides  (model_pricing_resolver.go:244-248)
              # CNY → 丢弃 USD 基数，重置为空 CNY 卡；USD → 若基数被污染成 CNY 则回查 USD
        └─ BillingService.applyChannelTokenPriceOverrides  (billing_service.go:1450-1491)
              # 置 PricingSource="manual"、Currency=渠道币种；清空 NativeCard
        └─ computeTokenBreakdown  (billing_service.go:1685-1689)
              # CostBreakdown.PricingCurrency = pricing.Currency（空则 USD）
        └─ costPricingSnapshot  (pricing_snapshot.go:19)  → usage_logs.pricing_snapshot.currency
```

配套约束：

- 校验：`validatePricingEntries`（`channel_service.go:682-690`）要求 `currency ∈ {USD, CNY}`；**CNY token 手工卡必须同时给出 input / output / cache_read / cache_write**，免费项显式填 0。
- 账号成本统计**只允许 USD**：`validateAccountStatsPricingRules`（`channel_service.go:749-751`）对 CNY 直接返回 `ACCOUNT_STATS_CURRENCY_UNSUPPORTED`。
- 账号成本回算强制 USD：`account_stats_pricing.go` 在 `RequiresNativeCNY` 时用 `getUSDModelPricingAt` 覆写 `BasePricing`（diff 中新增块），避免把人民币售价当美元累加。
- 按次计费：`calculatePerRequestCost`（`billing_service.go:1819-1831`）仅在渠道价卡 `Currency == "CNY"` 时标记 `PricingCurrency="CNY"`，`PricingSource="manual"`。
- CNY 不叠加 DeepSeek 官方峰谷：`calculateTokenCost`（`billing_service.go:1612`）加了 `pricing.Currency != "CNY"` 前置条件；CNY 的峰谷由 `card.ratesAt` 自带。
- CNY 不套用模型策略覆盖：`applyModelSpecificPricingPolicyEx`（`billing_service.go:1902-1904`）对 CNY 直接原样返回。
- 展示侧：`fillGlobalPricingFallback`（`channel_available.go:124-141`）在 `RequiresNativeCNY` 时用 `nativePricingAt` 合成展示价卡；`model_plaza_service.go:355-362` 同样从 `NativeCard.ratesAt` 取展示价。

### 5.2 `0e6f5efb` — feat(channels): add model mapping, upstream sync and pricing snapshots

规模：27 files，+1101/-82。

| 主题 | 变更 | 位置 |
|---|---|---|
| 分组模型映射候选 | 新增「只列出上游账号已配置的具体模型名」的服务方法（不联系上游、不暴露凭据、不加全局目录） | `service/admin_group_model_mapping.go:16` `GetGroupModelMappingCandidates`；接口登记 `service/admin_service.go:43` |
| 上游模型同步 | 新增批量读取上游 `/models` 的管理端点，并发上限 4，45s 超时；全部账号失败返回 502 | `handler/admin/group_upstream_models.go:28` `SyncUpstreamModels`、`:118` `clientModelsFromUpstream`；路由 `routes/admin.go:330` `POST /api/v1/admin/groups/models/sync-upstream` |
| 映射候选端点 | — | `routes/admin.go:338` `GET /api/v1/admin/groups/:id/model-mapping-candidates` → `handler/admin/group_handler.go:641` |
| 默认定价快照 | 把原先内联在 handler 里的默认价卡快照抽成领域方法，新增 `snapshot_ready` 语义 | `service/model_default_pricing.go:22` `DefaultPricingSnapshot()`；调用点 `handler/admin/channel_handler.go:618` |
| 仅模型名的规则不再当币种覆盖 | `hasChannelTokenOverrides` 判定；空价条目不再把 CNY/无价强制成 USD 或零价手工卡 | `model_pricing_resolver.go:278` `hasChannelTokenOverrides`、`:233-238`（`applyTokenOverrides` 早退）、`billing_service.go:1415`（`GetModelPricingWithChannel` 改判） |
| 前端 | 渠道页模型映射/上游同步 UI 与工具函数 | `frontend/src/utils/channelModelMapping.ts`、`channelPricingSnapshot.ts`、`frontend/src/views/admin/ChannelsView.vue` |

`DefaultPricingSnapshot` 的关键语义（`model_default_pricing.go:22-59`）：

- 默认 `SnapshotReady=true`，币种空则 USD（:30-32）。
- 无 `NativeCard` 且四项 token/图片价全为 0 → `SnapshotReady=false`（:35-38），避免把「只有按张价」的目录条目变成免费 token 卡。
- 有 `NativeCard` 时按 `NativePricingAt`（零值取 `time.Now()`）重取 `ratesAt(0, at)`；`CacheRead`/`CacheWrite` 为 nil 则对应字段返回 nil，且 `SnapshotReady` 要求两者都非 nil（:45-58）。测试 `model_default_pricing_test.go:24-32` 用 +24h 验证「跨时段后快照不再 ready」。

---

## 6. 缺价时的行为

### 6.1 两条截然不同的路径

| 场景 | 行为 | 证据 |
|---|---|---|
| **USD 模型缺价** | 记 0 费用 + WARN 日志，**不阻断**，仍写 usage_log | `openai_gateway_usage.go:259-272`（`pricing_missing_record_zero_cost`）；`gateway_usage_billing.go:1163-1169`（返回 `&CostBreakdown{ActualCost: 0}`） |
| **国模（`RequiresNativeCNY`）缺价** | **fail-closed：拒绝转发 / 计费返回错误** | 见下 |

### 6.2 国模 fail-closed 的三道闸

1. **转发前（前置校验）**：`handler/gateway_inflight_reservation.go:76-82` 在 `reserveInflightBalanceCtx` 内**无条件**调用 `ValidateNativePricingRequest`（即使预留开关关闭或订阅分组），失败即 `ErrInsufficientBalance` 级别错误返回给客户端。
   - 实现：`service/billing_inflight_reservation.go:689-717` `validateNativePricingRequest`。对每个候选计费模型：非 CNY 跳过；CNY 且非 token 类型（图片/视频/音频/按次）必须解析到 **分组或渠道的显式按次价**，否则 `ErrModelPricingUnavailable: native CNY media requests require an explicit per-request price`（:705-707）；token 类型解析到 `BasePricing` 或 group/channel 源即可放行（:708-710）；否则回落到 `GetModelPricing`（:712-714）。
2. **计费时（记录阶段）**：
   - OpenAI 网关：`openai_gateway_usage.go:253-258` —— 只要任一候选模型 `RequiresNativeCNY`，**直接 return err**（不走「记 0 + WARN」）。
   - Claude 网关：`gateway_usage_billing.go:1165-1167` 返回 `&CostBreakdown{PricingError: err}`，再由 `recordUsageCore`（:835-837）`return cost.PricingError`。
3. **缓存计量缺失**：`resolveNativeTokenPricing`（`pricing_native_cny.go:488-490`）在使用到 cache 读/写但该卡未定义对应价时返回 `CNY cache price is missing`，同样归入 fail-closed。

### 6.3 与「不得按零价或回落 GPT 兜底价」的项目红线一致性

- 国模走 `getModelPricingAt`（`billing_service.go:1339-1343`）时**直接**走 `nativePricingAt`，**不会**进入 `getUSDModelPricingAt` 的 LiteLLM→fallback 链，因此不存在「回落 GPT 兜底价」。
- 反面风险点在 `getUSDModelPricingAt`：`GetModelPricing` 对含 `haiku`/`opus`/`claude` 的任意名字会命中 `getFallbackPricing` 的系列兜底（注释 `billing_service.go:1306-1311`）。凡是「模型名来自外部且能查到价会直接影响金额」的场景必须用 `HasIdentifiedTokenPricing`（:1312）而不是 `GetModelPricing`——上游自报模型计费正是这样做的（`hasIdentifiedOpenAIResponsePricing` `openai_gateway_usage.go:542-551`）。
- 相关兜底价常量集中在 `billing_service.go:43-199`（`openAIGPTImage25FallbackPricing`、`openAIGPT54FallbackPricing`、`openAIGPT6AstraFallbackPricing`、`claudeOpus55FallbackPricing` 等）。

---

## 7. 关键结论（给后续改造用）

1. **网关入口只有 5 组路由前缀**（`/v1`、`/v1beta`、根别名 `/`、`/backend-api/codex`、`/antigravity/*`），平台分流完全由 `getGroupPlatform(c)` + `domain.UsesOpenAIGateway` 决定，**没有独立的 grok/gemini 路由文件**；Gemini 只有 `/v1beta` 是独立组。
2. **定价解析链是单点**：`ModelPricingResolver.Resolve`（`model_pricing_resolver.go:73`）是分组卡 / 渠道卡 / 目录 / 兜底的唯一汇聚点；新增定价来源应接在这里而不是散落到 handler。
3. **扣费单位恒为 credits 数字**，币种只影响「取哪套单价」与快照记录（`pricing_snapshot.go:5-6` 注释明确：`quota_unit = credits`）。
4. **国模是并行定价源而非覆盖层**：开启 `native_cny_enabled` 后，`isNativeCNModel` 命中的模型**绕开**整条 USD 目录链；未命中前缀的模型仍走 USD。因此「哪些模型算国模」由硬编码前缀表 `isNativeCNModel` 决定（`pricing_native_cny.go:65-73`），新增厂商必须改代码。
5. **缺价策略按币种分裂**：USD 记 0 放行，CNY 一律 fail-closed（前置 + 记录双闸）。改造计费时若放宽 CNY 的 fail-closed，会直接触碰项目红线「缺价不得放行」。
6. **新增计费相关数据库列的完整清单**（漏一处即 INSERT 失败）：`ent/schema/usage_log.go` → `go generate ./ent` → `migrations/24x_*.sql` → `service/usage_log.go` → `repository/usage_log_repo_insert.go`（`usageLogInsertArgTypes` + 4 处 SQL 列 + `prepareUsageLogInsert`）→ `repository/usage_log_repo_query.go`（`usageLogSelectColumns` + `scanUsageLog`）→ `handler/dto/types.go`。
7. **在途预留是「转发前最后一次」的余额闸门**（`handler/gateway_inflight_reservation.go:45`），也是国模定价前置校验的挂载点；任何新增的入口 handler 都必须调用它，否则国模缺价会漏过。

---

## 8. 未确认项

| # | 未确认内容 | 原因 / 需要什么证据 |
|---|---|---|
| 1 | 上游 `basellm/llm-metadata` 实际返回的 `schedule.windows` 数据形态（真实时区、是否真存在峰谷窗口） | 本机未联网抓取该 JSON；只有代码结构与单测 fixture（`pricing_native_cny_test.go:15`）可证。需要实际抓一次 `api/all.json` 比对 |
| 2 | `llm-prices-cn` 补充源当前是否仍包含 `billing_currency=CNY` 的有效条目 | 同上，未实际请求 |
| 3 | 当前 `F:\sub2\backend\data\model_pricing_cny.json` 是否存在、包含多少模型 | 该路径属运行期数据，未在仓库中；未执行部署 |
| 4 | `native_cny_enabled` 在当前数据库 settings 中的实际取值 | 需要连库读 `settings` 表的 `model_pricing_controls`；本次只读源码 |
| 5 | `pricing.remote_enabled` 与 DB 值不一致时多实例的一致性表现 | 代码注释称「多个服务实例需重启以读取同一份数据库开关」（`resources/model-pricing/README.md:49`），未做多实例实测 |
| 6 | 全量入口 handler 是否**都已**挂 `reserveInflightBalance` | §1.4 表格基于 `grep CheckBillingEligibility|reserveInflightBalance|RecordUsage`；`openai_live.go`、`openai_gateway_count_tokens.go` 只见预检未见预留，需逐个 handler 复核是否有意为之 |
| 7 | `billing_context_schedule.go` 的 CNY 展示分支是否覆盖 `Schedule` 的窗口 tier | 代码 `:111-117` 收集窗口 tier 作为断点、`:121` 用 `ratesAt(seg.min+1, timezone.Now())` 取当前时段价；跨时段展示是否正确未做端到端验证 |
