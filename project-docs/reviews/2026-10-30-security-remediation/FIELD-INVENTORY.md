# 字段消费清单（FIELD-INVENTORY）

| 项 | 值 |
|---|---|
| 分支 | `fix/security-remediation-20261030` @ `f9d8bc855`（改前基线） |
| 侦察范围 | `frontend/src`（Vue 3 SPA），**只统计非测试代码的实际读取点** |
| 目的 | 决定 api-surface 在 F-05（/groups/available 字段收敛）与 F-04（/channel-monitor-v2 用户域收敛）中的白名单 |
| 产出人 | frontend（task-3） |
| 生成时间 | 2026-10-30 |
| 复核方式 | 下表「读取位置」列均为 `文件:行号`，reviewer 可直接跳到源码核对 |

## 0. 方法

1. `grep frontend/src` 全量搜索字段名，剔除 `__tests__/` 与 `*.spec.ts`。
2. 对每个命中点 `read` 上下文，判定是「读取该响应字段」还是「同名字符串/无关变量」（例如 `created_at` 在 KeysView 中多数是**表格列 key** 而非 group 字段）。
3. 同时反向核对后端 DTO 定义（`backend/internal/handler/dto/types.go:107` `Group`）、内嵌点（`dto/mappers.go`）与已有的用户面白名单 DTO（`available_channel_handler.go:56` `userAvailableGroup`、`model_plaza_handler.go:81` `modelPlazaGroup`）。

## 1. GET /groups/available

前端只有 **2 处**调用 `userGroupsAPI.getAvailable()`：

- `frontend/src/views/user/KeysView.vue:1674` → `groups.value`（API 密钥页：新建/编辑 Key 的分组下拉、分组链、HA 兜底选择器）
- `frontend/src/views/user/UsageView.vue:843` → `groups.value`（**仅**用于「分组」下拉筛选选项）

### 1.1 该端点实际被读取的字段

| 字段 | 读取位置（文件:行号） | 用途 | 收敛处置 |
|---|---|---|---|
| `id` | KeysView.vue:1543、1513、1561、1566；GroupChainEditor.vue:55,92,93,99；KeyHAGroupPicker.vue:47；UsageView.vue:422 | 选项 value / 分组匹配 | **保留** |
| `name` | KeysView.vue:1544；GroupChainEditor.vue:55,94；KeyGroupChain.vue:27；UsageView.vue:422 | 展示名 | **保留** |
| `description` | KeysView.vue:1545、1597（搜索匹配）；GroupOptionItem.vue props | 下拉项副标题 + 搜索 | **保留** |
| `platform` | KeysView.vue:1553、1561、1566（`getKeyGroupProvider`）；GroupChainEditor.vue:94；KeyGroupChain.vue:27 | 按 Anthropic/OpenAI/... 分组过滤 | **保留** |
| `rate_multiplier` | KeysView.vue:1546；GroupChainEditor.vue:55,94；KeyGroupChain.vue:27 | 默认倍率展示（用户专属倍率来自 /groups/rates 在前端 join） | **保留** |
| `subscription_type` | KeysView.vue:1552；GroupChainEditor.vue:94；KeyGroupChain.vue:27 | 订阅分组视觉加深 | **保留** |
| `status` | GroupChainEditor.vue:92（`g.status !== 'inactive'`）；KeyHAGroupPicker.vue:47 | 过滤停用分组 | **保留** |
| `peak_rate_enabled` | KeysView.vue:1548；GroupChainEditor.vue:94 | 高峰倍率徽标开关 | **保留** |
| `peak_start` | KeysView.vue:1549；GroupChainEditor.vue:94 | 高峰窗口起点 | **保留** |
| `peak_end` | KeysView.vue:1550；GroupChainEditor.vue:94 | 高峰窗口终点 | **保留** |
| `peak_rate_multiplier` | KeysView.vue:1551；GroupChainEditor.vue:94 | 高峰倍率 | **保留** |
| `deepseek_pricing_state` | GroupChainEditor.vue:41 → GroupPricingStateBadge.vue:41 → deepseekPricingState.ts:61 | DeepSeek 峰谷状态徽标（**已容错**：缺失即不渲染） | **保留** |
| `allow_live` | GroupChainEditor.vue:39 → GroupLiveBadge.vue:30,35 | Live 徽标 | **保留** |
| `audio_realtime_price_per_min` | GroupLiveBadge.vue:30,38 | Realtime 每分钟单价（缺失→不显示价格） | **保留** |
| `allow_high_availability` | KeyHAGroupPicker.vue:42,47 | 筛出「可作 HA 兜底」的候选组 | **保留** |
| `is_exclusive` | 本端点消费方**不读**（仅 /channels/available 的 AvailableChannelsTable.vue:364,368 与 admin 读） | — | **保留（低风险）**：task-2 白名单已含；剔除亦不影响本端点 |

### 1.2 通过**其他响应内嵌**的同一 DTO 读取的字段（务必保留）

这些字段读的不是 /groups/available 的响应，而是 `dto.Group` 被内嵌进其他用户侧响应后的结果。**如果直接裁剪 `dto.Group` 结构体本体，这几处会一起坏掉。**

| 字段 | 读取位置 | 数据来源 | 后果 |
|---|---|---|---|
| `claude_code_only` | KeysView.vue:1094 | `dto.APIKey.Group`（mappers.go:113 `GroupFromServiceShallow`） | Claude Code 客户端选择器判据丢失 |
| `allow_messages_dispatch` | KeysView.vue:1095 | 同上 | Claude Code 教程显隐判据丢失 |
| `allow_batch_image_generation` | composables/useBatchImageAccess.ts:16 | 同上（`key.group`） | 批量生图入口鉴权误判 |
| `daily_limit_usd` | SubscriptionsView.vue:108,114,123,129,223；SubscriptionProgressMini.vue:75,85,91,98,214 | `dto.UserSubscription.Group`（mappers.go:953） | **订阅进度条分母变 undefined** |
| `weekly_limit_usd` | SubscriptionsView.vue:143,149,158,164,224；SubscriptionProgressMini.vue:103,113,119,126,217 | 同上 | 同上 |
| `monthly_limit_usd` | SubscriptionsView.vue:182,188,197,203,225；SubscriptionProgressMini.vue:131,141,147,156,220 | 同上 | 同上 |
| `name` | components/admin/usage/UsageTable.vue:122（`row.group.name`） | `dto.UsageLog.Group`（mappers.go:789） | 用量表「分组」列空白 |
| `description`/`platform`/`rate_multiplier` | SubscriptionsView.vue:32,39,43,45,49,50,53 | `dto.UserSubscription.Group` | 订阅卡片标题/平台色/倍率丢失 |
| `is_exclusive` | AvailableChannelsTable.vue:364,368 | `userAvailableGroup`（/channels/available 自己的白名单） | 不受影响 |

### 1.3 该端点消费方**不读**（可从用户面剔除）

| 字段 | 说明 |
|---|---|
| `daily_limit_usd` / `weekly_limit_usd` / `monthly_limit_usd` | /groups/available 消费方不读（订阅页读的是 subscription.group，见 1.2） |
| `fallback_group_id` / `fallback_group_id_on_invalid_request` | 无用户面读取 |
| `image_price_1k` / `_2k` / `_4k` | 仅 admin GroupsView 与 groupsImagePricing.ts |
| `video_price_480p` / `_720p` / `_1080p`、`video_model_prices`、`video_price_plans` | 仅 admin |
| `web_search_price_per_call`、`search_price_per_1k` | 仅 admin GroupsView |
| `audio_tts_price_per_million_chars`、`audio_stt_price_per_hour` | 仅 admin GroupsView |
| `batch_image_discount_multiplier`、`batch_image_hold_multiplier` | 仅 admin GroupsView |
| `image_rate_independent` / `image_rate_multiplier` / `video_rate_independent` / `video_rate_multiplier` | 仅 /model-plaza（PlazaGroupSection.vue:64-67）与 admin |
| `long_context_pricing_enabled` | 仅 admin GroupsView + /model-plaza（PlazaGroupSection.vue:120） |
| `allow_image_generation` | **无任何用户面读取**（仅 admin GroupsView） |
| `rpm_limit`、`max_reasoning_effort`、`max_reasoning_effort_over_limit`、`reasoning_effort_mappings` | 仅 admin |
| `require_oauth_only`、`require_privacy_set` | 仅 admin GroupsView |
| `scheduling_strategy`、`effective_scheduling_strategy` | 仅 admin GroupsView |
| `default_mapped_model`、`messages_dispatch_model_config` | 仅 admin |
| `created_at` / `updated_at` | 无用户面读取（KeysView/UsageView 的 `created_at` 是表格列 key 与排序参数，不是 group 字段） |
| `i18n` | 用户侧已在 `Localized()` 中把译文合并进 name/description 后清空 |

### 1.4 实施约束（给 api-surface，硬约束）

1. **不要直接裁剪 `dto.Group`（types.go:107）本体**——它被 `dto.APIKey.Group`（mappers.go:113）、`dto.UserSubscription.Group`（mappers.go:953）、`dto.UsageLog.Group`（mappers.go:789）、`dto.RedeemCode.Group`（mappers.go:697）、`dto.AccountGroup.Group`（mappers.go:557）复用（均走 `GroupFromServiceShallow`）。
2. 建议**新增独立用户面 mapper**（如 `GroupUserFacingFromService`），只用在 `api_key_handler.go:500`（GetAvailableGroups）；其余内嵌点保持 `GroupFromServiceShallow`。
3. 若必须裁 `dto.Group`：至少保留 `name`/`description`/`platform`/`rate_multiplier` + `daily_limit_usd`/`weekly_limit_usd`/`monthly_limit_usd` + `claude_code_only`/`allow_messages_dispatch`/`allow_batch_image_generation`，否则订阅进度条、用量表分组列、Claude Code/批量生图入口会同时坏。
4. 前端侧配合：收敛落地后把 `frontend/src/types/index.ts:605 interface Group` 拆成「用户面 Group（可选字段）」与 `AdminGroup`，消费点全部改可选链 + 缺省降级。

### 1.5 白名单建议

`id, name, description, platform, rate_multiplier, is_exclusive, subscription_type, status, peak_rate_enabled, peak_start, peak_end, peak_rate_multiplier, deepseek_pricing_state, allow_image_generation, allow_batch_image_generation, allow_high_availability, allow_messages_dispatch, allow_live, audio_realtime_price_per_min, claude_code_only`

（`allow_image_generation` 与 `is_exclusive` 属「保留无害」，不属于必需项。）

## 2. GET /channel-monitor-v2/*（用户域，`admin=false`）

唯一消费组件：`frontend/src/views/user/ChannelStatusV2View.vue` + `frontend/src/features/channel-monitor-v2/{RelayPulseMatrix,MonitorTrendChart}.vue`。

> `frontend/src/features/channel-monitor-v2/MonitorSettingsPanel.vue` 只调用 **/admin**/channel-monitor-v2/config（api/channelMonitorV2.ts:265,269），属管理域，**不受用户域收敛影响**。

### 2.1 snapshot（ChannelStatusV2View.vue:721 `getSnapshot(filter, isAdmin, signal)`）

| 路径 | 读取位置 | 用途 | 收敛处置 |
|---|---|---|---|
| `config.refresh_interval_seconds` | **ChannelStatusV2View.vue:819**（`snapshot.value?.config?.refresh_interval_seconds || 300`） | 自动刷新轮询间隔 | **保留**（唯一被读的 config 字段；缺失已有 300s 兜底） |
| `coverage.requested_start` | RelayPulseMatrix.vue:231,335 | 矩阵时间轴起点 | 保留 |
| `coverage.requested_end` | RelayPulseMatrix.vue:232-233,336 | 矩阵时间轴终点 | 保留 |
| `coverage.coverage_start` | RelayPulseMatrix.vue:337 | 实际覆盖起点 | 保留 |
| `coverage.data_through` | ChannelStatusV2View.vue:27-28；RelayPulseMatrix.vue:236,338 | 「更新至」时间 | 保留 |
| `coverage.bucket_seconds` | RelayPulseMatrix.vue:230,283,339,469；MonitorTrendChart.vue:106 | 时间桶宽度 | 保留 |
| `coverage.coverage_complete` | ChannelStatusV2View.vue:32 | 不完整提示 | 保留 |
| `coverage.bootstrap.active` | ChannelStatusV2View.vue:649 | 首次回填进度条开关 | 保留 |
| `coverage.bootstrap.progress_percent` | ChannelStatusV2View.vue:651 | 回填百分比 | 保留 |
| `metrics.error_rate` / `ttft.p50_ms` / `tpm` / `rpm` / `cache_rate` | ChannelStatusV2View.vue:205-206,211-213,219-221,225-226,232-234；表格 :321-329 | KPI 卡与矩阵 | 保留 |
| `health.overall` / `error_rate` / `ttft` / `cache` / `score` | ChannelStatusV2View.vue:207,214,227,881-884 | 健康色带 | 保留 |
| `trend[]` | ChannelStatusV2View.vue:253 | 折线趋势 | 保留 |
| **`config.enabled`** | 无读取 | — | **可剔除** |
| **`config.version`** | 无读取 | — | **可剔除** |
| **`config.platforms[]`（含上游平台清单）** | 无读取 | — | **可剔除** |
| **`config.group_ids`** | 无读取 | — | **可剔除**（现状已由 `redactChannelMonitorV2PublicConfig` 置 nil） |
| **`config.health_thresholds`** | 无读取（用户侧不渲染阈值） | — | **可剔除** |
| **`config.ignored_error_categories`** | 无读取 | — | **可剔除**（现状已置 nil） |
| **`coverage.aggregation_lag_seconds`** | 无读取 | — | 保留或剔除均可（低风险） |
| **`health.minimum_sample` / `health.thresholds`** | 无读取 | — | 保留或剔除均可 |

### 2.2 matrix / dimensions / models / errors / users

| 端点 | 被读字段 | 读取位置 | 收敛处置 |
|---|---|---|---|
| `/matrix` | `coverage.*`、`items[].platform`、`items[].group_id`、`items[].group_name`、`items[].model`、`items[].metrics`、`items[].health`、`items[].buckets[].{bucket_start,metrics,health}` | ChannelStatusV2View.vue:260,295-329,656 | **形状不变** |
| `/dimensions` | `platforms[].{value,label,request_count}`、`groups[].{id,name,platform,request_count}`、`models[].{value,label,platform,request_count}` | ChannelStatusV2View.vue:714，筛选器 | **形状不变** |
| `/models` | `items[].{platform,model,metrics,health}` | ChannelStatusV2View.vue:783 | **形状不变** |
| `/errors` | `items[].{category,count,rate,ignored}`；`items[].details[]` 仅管理员渲染 | ChannelStatusV2View.vue:341,350,355,361,785；details 见 :366-368 | **形状不变** |
| `/users` | `items[].{user_id,rank,email,username,display_label,is_self,can_drilldown,metrics}` | ChannelStatusV2View.vue:787 | **形状不变** |

**关键点**：`errors[].ignored` 由服务端计算（`backend/internal/service/channel_monitor_v2.go:269`），**不依赖 `config.ignored_error_categories`**，因此剔除 config 中的该字段不会让 UI 置灰失效。

## 3. 结论

1. /groups/available 可以安全收敛到 §1.5 的白名单，**前提是新建独立 mapper**，不动 `dto.Group` 本体。
2. /channel-monitor-v2 用户域 snapshot 的 `config` 可收敛为仅 `refresh_interval_seconds`；其余响应形状必须保持不变。
3. 现有 `redactChannelMonitorV2PublicConfig`（service/channel_monitor_v2.go:621）已剔除 `group_ids`/`ignored_error_categories`/`updated_by`/`platforms[].models`，再剔 `enabled`/`version`/`platforms`/`health_thresholds` 同样安全。
