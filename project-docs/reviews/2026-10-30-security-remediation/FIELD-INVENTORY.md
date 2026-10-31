# 字段消费清单与收敛交付说明（FIELD-INVENTORY）

> 本文件是 F-05（`GET /groups/available` 字段收敛）与 F-04（`GET /channel-monitor-v2/snapshot` 用户域 config 收敛）的**唯一实施依据**：谁读哪个字段、读的是哪条响应、裁掉会坏什么、改哪里、怎么验收。
> 结论全部由当前源码复核，不采信外部报告的直接断言。

| 项 | 值 |
|---|---|
| 代码基线 | 分支 `fix/security-remediation-20261030`（PR #18，base `develop`） |
| 复核范围 | `frontend/src`（非测试代码的实际读取点）+ `backend/internal/handler` 的 DTO/mapper |
| 复核时间 | 2026-10-31（对 10-30 初版做来源复核后重写） |
| 结论强度 | 「必需」= 有用户面读取点且数据来自该响应；「可剔除」= 无读取点，或读取点的数据来源是**另一条**响应 |

## 0. 结论先行

1. `/groups/available` 的**最小必需字段只有 15 个**（§2.5），其余可全部从用户面剔除。
2. **绝对不要裁剪 `dto.Group` 本体**（`backend/internal/handler/dto/types.go:107`）：它被 6 处内嵌复用，裁了会连带坏掉订阅进度条、用量表分组列、批量生图入口。必须**新增独立用户面 mapper**，只给 `GetAvailableGroups` 用（§2.3、§4.1）。
3. 有 3 个字段（`long_context_pricing_enabled` / `image_rate_independent` / `video_rate_independent`）**曾被误列为「必需」**。复核结论：它们由 `/model-plaza` 的自有白名单 DTO 下发，与 `/groups/available` 无关（§2.4.2）。把它们留在白名单里无害但无必要。
4. `/channel-monitor-v2` 用户域 snapshot 的 `config` 可收敛到**仅 `refresh_interval_seconds`**；其余 5 个端点（matrix/dimensions/models/errors/users）的响应形状必须逐字不变（§3）。

## 1. 复核方法（可重跑）

```powershell
# 1) 找前端读取点（剔除测试）
rg -n "<字段名>" frontend/src --glob '!**/__tests__/**' --glob '!**/*.spec.ts'
# 2) 判定来源：读 props 类型/数据赋值，而不是只看字段名
rg -n "defineProps<\{" frontend/src/components/keys/GroupChainEditor.vue -A 12
rg -n "getAvailable\(\)" frontend/src
# 3) 反向核对后端：DTO 定义 + mapper + 已有用户面白名单
rg -n "type Group struct|type AdminGroup struct" backend/internal/handler/dto/types.go
rg -n "GroupFromServiceShallow\(" backend/internal/handler/dto/mappers.go
```

**判据（本轮新增，务必沿用）**：字段名命中不等于该响应被读取。必须回答两个问题：
① 读取点的 props/变量类型是不是 `Group`；② 该 `Group` 是不是 `/groups/available` 赋值来的。
只有 ① ② 同时成立才算「必需」。10-30 初版有三处把「同名字段」当成了「本端点字段」，本轮已按此判据修正。

## 2. `GET /api/v1/groups/available`

- 路由：`backend/internal/server/routes/user.go:93`
- 处理：`backend/internal/handler/api_key_handler.go:485` `GetAvailableGroups` → 第 500 行 `dto.GroupFromService(&groups[i]).Localized(...)`
- 前端调用点只有 2 处：
  - `frontend/src/views/user/KeysView.vue:1674` → `groups`（`KeysView.vue:1399`），下发给分组下拉、分组链、HA 兜底选择器、批量编辑
  - `frontend/src/views/user/UsageView.vue:872` → `groups`（`UsageView.vue:426`），**只**用于分组筛选下拉

### 2.1 该端点实际被读取的字段（**必需**，共 15）

| 字段 | 读取位置 | 用途 |
|---|---|---|
| `id` | `KeysView.vue:1543`、`1513`；`GroupChainEditor.vue:93`；`UsageView.vue:435` | 选项 value / 分组匹配 |
| `name` | `KeysView.vue:1544`；`GroupChainEditor.vue:55`、`94`；`KeyGroupChain.vue:26`；`UsageView.vue:435` | 展示名 |
| `description` | `KeysView.vue:1545`、`1597`（搜索匹配）；`GroupOptionItem.vue:71-84` props | 下拉副标题 + 搜索 |
| `platform` | `KeysView.vue:1553`、`1561`、`1566`；`GroupChainEditor.vue:94`；`KeyGroupChain.vue:27` | 平台图标/分组过滤 |
| `rate_multiplier` | `KeysView.vue:1546`；`GroupChainEditor.vue:55`、`94` | 默认倍率（专属倍率来自 `/groups/rates` 前端 join） |
| `subscription_type` | `KeysView.vue:1552`；`GroupChainEditor.vue:94` | 订阅分组视觉区分 |
| `status` | `GroupChainEditor.vue:92`（`!== 'inactive'`）；`KeyHAGroupPicker.vue:47` | 过滤停用分组 |
| `peak_rate_enabled` | `KeysView.vue:1548`；`GroupChainEditor.vue:94` | 高峰倍率徽标开关 |
| `peak_start` | `KeysView.vue:1549`；`GroupChainEditor.vue:94` | 高峰窗口起点 |
| `peak_end` | `KeysView.vue:1550`；`GroupChainEditor.vue:94` | 高峰窗口终点 |
| `peak_rate_multiplier` | `KeysView.vue:1551`；`GroupChainEditor.vue:94` | 高峰倍率数值 |
| `deepseek_pricing_state` | `GroupChainEditor.vue:41` → `GroupPricingStateBadge.vue:37,41` → `deepseekPricingState.ts:4,61` | DeepSeek 峰谷徽标（缺失即不渲染） |
| `allow_live` | `GroupChainEditor.vue:39` → `GroupLiveBadge.vue:30,35` | Live 徽标 |
| `audio_realtime_price_per_min` | `GroupLiveBadge.vue:30,38` | Realtime 每分钟单价（缺失→只显示徽标不显示价） |
| `allow_high_availability` | `KeyHAGroupPicker.vue:47` | 筛「可作 HA 兜底」的候选组 |

可选保留（不读，留着无害）：`is_exclusive`、`allow_image_generation`、`allow_batch_image_generation`。三者在本端点的消费方均无读取（证据见 §2.4），保留只是少改一处风险。

### 2.2 收敛后必须保持的契约细节

1. `Localized(Accept-Language)` 必须继续调用（`dto/group_localize.go:8`）：它把 name/description 换成请求语言并**清空 `i18n`**。新 mapper 若绕开它，用户侧会看到原文案 + 多出译文表。
2. 响应形态保持「平数组」`data: Group[]`（前端 `frontend/src/api/groups.ts:16-19` 按 `Group[]` 接收）。
3. `deepseek_pricing_state` 是 `omitempty` 指针；`audio_realtime_price_per_min` 是 `*float64`。缺失时前端已容错（`GroupLiveBadge.vue:38` 判 null），不要改成必填默认 0。
4. `/groups/rates`（`api_key_handler.go:507` `GetUserGroupRates`）与本次收敛无关，保持不动。

### 2.3 红线：`dto.Group` 被 6 处复用，禁止裁剪本体

`dto.Group`（`dto/types.go:107`）经 `GroupFromServiceShallow`（`dto/mappers.go:130`）与 `groupFromServiceBase`（`mappers.go:217`）被内嵌进以下结构：

| 内嵌点 | 定义 | 字段 | 用户面后果（裁掉后） |
|---|---|---|---|
| `dto.APIKey` | `dto/types.go:92` / `mappers.go:113` | `group` | `KeysView.vue:1094-1095` 的 `claude_code_only`/`allow_messages_dispatch` 丢失 → Claude Code 客户端选择器与教程判据失效；`useBatchImageAccess.ts:16`、`BatchImageGuideView.vue:945` 的 `key.group.allow_batch_image_generation` 丢失 → 批量生图入口误判 |
| `dto.UserSubscription` | `dto/types.go:809` / `mappers.go:953` | `group` | `SubscriptionsView.vue:108-225`、`common/SubscriptionProgressMini.vue:75-230` 的 `daily/weekly/monthly_limit_usd` 丢失 → **进度条分母 undefined** |
| `dto.UsageLog` | `dto/types.go:705` / `mappers.go:789` | `group` | `admin/usage/UsageTable.vue:122` 的 `row.group.name` 丢失 → 用量表分组列空白 |
| `dto.RedeemCode` | `dto/types.go:556` / `mappers.go:697` | `group` | 兑换码关联分组展示 |
| `dto.AccountGroup` | `dto/types.go:464` / `mappers.go:557` | `group` | 账号-分组关联展示 |
| `dto.Account` | `mappers.go:492` | `groups[]` | 账号详情里的分组列表 |

结论：**新增 `GroupUserFacingFromService`（或同义的独立白名单结构），只接在 `api_key_handler.go:500` 一处**；`mappers.go` 的其余调用点保持 `GroupFromServiceShallow` 不变。

### 2.4 可从用户面剔除（附证据）

#### 2.4.1 无任何用户面读取（仅 admin `GroupsView` 或仅计费内部）

`rpm_limit`、`max_reasoning_effort`、`max_reasoning_effort_over_limit`、`reasoning_effort_mappings`、`require_oauth_only`、`require_privacy_set`、`image_price_1k/2k/4k`、`video_price_480p/720p/1080p`、`video_model_prices`、`video_price_plans`、`web_search_price_per_call`、`search_price_per_1k`、`audio_tts_price_per_million_chars`、`audio_stt_price_per_hour`、`batch_image_discount_multiplier`、`batch_image_hold_multiplier`、`image_rate_multiplier`、`video_rate_multiplier`、`fallback_group_id`、`fallback_group_id_on_invalid_request`、`long_context_pricing_enabled`（见 2.4.2）、`default_mapped_model`、`messages_dispatch_model_config`、`scheduling_strategy`、`effective_scheduling_strategy`、`created_at`、`updated_at`、`i18n`（`Localized` 已清空）。

> `created_at`/`updated_at` 在 `KeysView`/`UsageView` 里是**表格列 key 与排序参数**，不是 group 字段；`i18n` 只在 `AdminGroup`。

#### 2.4.2 曾被误判为必需的 3 个字段（来源复核纠正）

| 字段 | 真实读取点 | 数据来源 | 处置 |
|---|---|---|---|
| `long_context_pricing_enabled` | `modelPlaza/PlazaGroupSection.vue:120`；`account/longContextBilling.ts:10` | `ModelPlazaGroup`（`frontend/src/api/modelPlaza.ts:59,81`，后端 `modelPlazaGroup` `model_plaza_handler.go:104`）；`AdminGroup`（账号弹窗，`account/longContextBilling.ts:1` 显式 import `AdminGroup`） | **不需要**进 `/groups/available` 白名单；留也无害 |
| `image_rate_independent` | `PlazaGroupSection.vue:64`；其余全在 `views/admin/GroupsView.vue` | 同上（`modelPlaza.ts:75`） | 同上 |
| `video_rate_independent` | `PlazaGroupSection.vue:66`；其余全在 `GroupsView.vue` | 同上（`modelPlaza.ts:78`） | 同上 |

#### 2.4.3 由**另一条**响应的自有白名单下发（与本端点无关）

| 字段 | 读取点 | 实际来源 |
|---|---|---|
| `claude_code_only` | `KeysView.vue:1094`（`selectedKey?.group`） | `dto.APIKey.Group` |
| `allow_messages_dispatch` | `KeysView.vue:1095` | `dto.APIKey.Group` |
| `allow_batch_image_generation` | `useBatchImageAccess.ts:16`、`BatchImageGuideView.vue:945` | `key.group`（keys 列表） |
| `is_exclusive` | `channels/AvailableChannelsTable.vue:364,368`；`modelPlaza/PlazaGroupSection.vue:24` | `userAvailableGroup`（`available_channel_handler.go:56`，端点 `/channels/available`）；`modelPlazaGroup` |
| `daily/weekly/monthly_limit_usd` | `SubscriptionsView.vue:108-225`、`SubscriptionProgressMini.vue:75-230` | `dto.UserSubscription.Group` |

### 2.5 交付白名单（照抄即可）

```text
必需（15）:
id, name, description, platform, rate_multiplier, subscription_type, status,
peak_rate_enabled, peak_start, peak_end, peak_rate_multiplier,
deepseek_pricing_state, allow_live, audio_realtime_price_per_min, allow_high_availability

可选保留（3，不读但无害）:
is_exclusive, allow_image_generation, allow_batch_image_generation
```

与 10-30 初版（20 字段）的差异：移除 `long_context_pricing_enabled`、`image_rate_independent`、`video_rate_independent`、`claude_code_only`、`allow_messages_dispatch`（后两个是 `dto.APIKey.Group` 的字段，不在本响应里）。

## 3. `GET /api/v1/channel-monitor-v2/snapshot`（用户域，`admin=false`）

- 处理：`channel_monitor_v2_handler.go:93` `Snapshot` → `:102` `snapshot(c,false)` → `service/channel_monitor_v2.go:487` `Snapshot(ctx,filter,admin)`
- 现状脱敏：`service/channel_monitor_v2.go:496-497` → `redactChannelMonitorV2Snapshot`（`:611`）→ `redactChannelMonitorV2PublicConfig`（`:621`），当前置 nil 的是 `group_ids`/`ignored_error_categories`/`updated_by`/`platforms[].models`
- 前端消费：`views/user/ChannelStatusV2View.vue` + `features/channel-monitor-v2/{RelayPulseMatrix,MonitorTrendChart}.vue`（`MonitorSettingsPanel.vue` 只打 **/admin** 端点，不在收敛范围）

### 3.1 config 可收敛范围

| 字段 | 用户面读取 | 处置 |
|---|---|---|
| `refresh_interval_seconds` | `ChannelStatusV2View.vue:819`（`?? 300` 兜底） | **保留（唯一必需）** |
| `enabled` | 无 | 剔除 |
| `version` | 无 | 剔除 |
| `platforms[]`（含上游模型清单） | 无（筛选器的 platforms 来自 `/dimensions`） | 剔除 |
| `group_ids` | 无 | 剔除（现状已 nil） |
| `health_thresholds` | 无 | 剔除 |
| `ignored_error_categories` | 无 | 剔除（现状已 nil） |
| `updated_at` / `updated_by` | 无 | 剔除 |

目标形状：`"config": { "refresh_interval_seconds": 300 }`，**只这一个 key**。

### 3.2 其余响应形状必须逐字不变

| 端点 | 必须保持的字段 |
|---|---|
| snapshot | `coverage.*`（`requested_start/requested_end/coverage_start/data_through/bucket_seconds/coverage_complete/bootstrap{active,progress_percent}`）、`metrics.{error_rate,ttft,cache_rate,rpm,tpm}`、`health.{overall,error_rate,ttft,cache,score}`、`trend[]` |
| `/matrix` | `coverage.*`、`items[].{platform,group_id,group_name,model,metrics,health,buckets[]}` |
| `/dimensions` | `platforms[].{value,label,request_count}`、`groups[].{id,name,platform,request_count}`、`models[].{value,label,platform,request_count}` |
| `/models` | `items[].{platform,model,metrics,health}` |
| `/errors` | `items[].{category,count,rate,ignored}`；`details[]` 仅管理员（`service:575-583` 已对非管理员置 nil） |
| `/users` | `items[].{user_id,rank,email,username,display_label,is_self,can_drilldown,metrics}` |

### 3.3 安全依据（为什么剔除 config 字段不会让 UI 失效）

- `errors[].ignored` 由**服务端**在查询时算出：`service/channel_monitor_v2.go:269` 定义字段，`repository/channel_monitor_v2_repo.go:729-730` 用 `ignoredSet` 赋值（`ignoredSet` 来自服务端配置，不来自响应里的 `config`）。因此 `config.ignored_error_categories` 置 nil 不会让置灰逻辑失效。
- 前端 `api/channelMonitorV2.ts:197` 只是读取该布尔值，不自己推导。

## 4. 实施步骤（照做即可）

### 4.1 后端 F-05（`/groups/available`）

1. 在 `backend/internal/handler/dto/` 新增用户面类型与 mapper（建议文件名 `group_user_facing.go`，不放进 `types.go` 以减少上游冲突面）：

```go
// UserFacingGroup 是 /groups/available 的响应白名单：只含用户界面上真正渲染的字段。
// 注意：不是 dto.Group 的替代品——dto.Group 仍被 6 类响应内嵌复用，不得裁剪。
type UserFacingGroup struct {
    ID                    int64                    `json:"id"`
    Name                  string                   `json:"name"`
    Description           string                   `json:"description"`
    Platform              string                   `json:"platform"`
    RateMultiplier        float64                  `json:"rate_multiplier"`
    SubscriptionType      string                   `json:"subscription_type"`
    Status                string                   `json:"status"`
    PeakRateEnabled       bool                     `json:"peak_rate_enabled"`
    PeakStart             string                   `json:"peak_start"`
    PeakEnd               string                   `json:"peak_end"`
    PeakRateMultiplier    float64                  `json:"peak_rate_multiplier"`
    DeepSeekPricingState  *DeepSeekPricingStateDTO `json:"deepseek_pricing_state,omitempty"`
    AllowLive             bool                     `json:"allow_live"`
    AudioRealtimePricePerMin *float64              `json:"audio_realtime_price_per_min"`
    AllowHighAvailability bool                     `json:"allow_high_availability"`
}

// Localized 与 Group.Localized 用同一条语言选择规则：把 name/description 换成
// 请求首选语言的版本。这里没有 i18n 字段可清空，所以只做替换。
func (g *UserFacingGroup) Localized(i18n domain.GroupI18n, acceptLanguage string) *UserFacingGroup {
    if g == nil {
        return nil
    }
    g.Name, g.Description = i18n.Localize(acceptLanguage, g.Name, g.Description)
    return g
}
```

2. 新增 mapper。注意 `UserFacingGroup` **不**携带 `i18n` 字段，语言表由调用方直接从 `service.Group.I18n` 传给 `Localized`：

```go
func UserFacingGroupFromService(g *service.Group) *UserFacingGroup
```

> 关键点：`Localized` 必须**逐字复用** `dto/group_localize.go:8` 的语言选择（`I18n.Localize(acceptLanguage, name, description)`），不要另写一套回退规则，否则同一分组在两条路径上可能显示不同语言。

3. 只改 `backend/internal/handler/api_key_handler.go:500`：

```go
lang := c.GetHeader("Accept-Language")
out := make([]dto.UserFacingGroup, 0, len(groups))
for i := range groups {
    g := &groups[i]
    out = append(out, *dto.UserFacingGroupFromService(g).Localized(g.I18n, lang))
}
```

4. 不动 `dto.Group`、`groupFromServiceBase`、`GroupFromServiceShallow`、其余 `GroupFromService*` 调用点。
5. 同步更新契约快照：`backend/internal/server/api_contract_test.go:318` 的 `wantJSON` 改成新形状（该用例已经断言「不含 model_routing/account_count/profit_control」，是天然的收敛护栏）。

### 4.2 前端 F-05

1. `frontend/src/types/index.ts:605` 的 `Group` 拆两份：
   - `UserFacingGroup`（15 字段，与后端同名；非必需项全部标 `?`）
   - `Group` 保留为「上游全量形状」，供 admin / 内嵌响应继续用；`AdminGroup`（`:673`）不动。
2. `frontend/src/api/groups.ts:16` 返回类型改成 `UserFacingGroup[]`；`KeysView.vue:1399`、`UsageView.vue:426` 的 `ref<Group[]>` 同步改。
3. 下游组件 props 收窄只做「变可选 + 缺省降级」，不改渲染逻辑：
   - `GroupChainEditor.vue:73` `groups: Group[]` → `UserFacingGroup[]`；`badgeProps`（`:94`）读的都是必需字段
   - `KeyHAGroupPicker.vue:37`、`KeyGroupChain.vue:22`、`BulkEditKeysModal.vue:173`
   - `common/GroupBadge.vue`、`common/GroupOptionItem.vue`、`user/GroupLiveBadge.vue`、`user/GroupPricingStateBadge.vue` 已是 `Pick`/`unknown` 入参，无需改
4. **不要**顺手把 `is_exclusive` 等删出类型：`AvailableChannelsTable.vue:364`、`PlazaGroupSection.vue:24` 仍在读，它们走的是另一条响应。

### 4.3 后端 F-04（`/channel-monitor-v2/snapshot` 用户域）

推荐做法（显式白名单，不产生零值填充字段）：

1. 在 `service/channel_monitor_v2.go` 新增：

```go
// ChannelMonitorV2PublicConfig 是用户域 snapshot 下发的 config 白名单。
// 管理端 /config 仍返回完整 ChannelMonitorV2Config。
type ChannelMonitorV2PublicConfig struct {
    RefreshIntervalSeconds int `json:"refresh_interval_seconds"`
}
```

2. 新增 `ChannelMonitorV2PublicSnapshot`（`Config` 用上面的类型，`Coverage/Metrics/Health/Trend` 复用现有字段类型）+ `func (s *ChannelMonitorV2Service) SnapshotPublic(...)`；`handler` 的 `snapshot(c, false)` 改调它，`admin=true` 仍走 `Snapshot`。
3. 删除 `redactChannelMonitorV2PublicConfig`（`:621`）与 `redactChannelMonitorV2Snapshot` 里的 config 部分；其余 metric 脱敏（`:637`）保留不动。
4. **风险点**：`ChannelMonitorV2Snapshot` 目前是单一结构体（`:211`），直接改它的 `Config` 类型会波及 repo 层测试（`repository/channel_monitor_v2_repo_test.go:239` 断言 `snapshot.Config == service.ChannelMonitorV2Config{}`）。因此选「新增 Public 结构 + 新增 service 方法」，不改老结构。

### 4.4 前端 F-04

`frontend/src/api/channelMonitorV2.ts:101` `MonitorConfig` 拆两份：`MonitorConfig`（admin，保持不动）、`MonitorPublicConfig`（`{ refresh_interval_seconds: number }`）；`:147` `MonitorSnapshot.config` 改用后者。`ChannelStatusV2View.vue:819` 无需改动（已是可缺省读取）。

## 5. 验收（必须逐条给出命令与输出）

### 5.1 静态/单测

```powershell
cd F:\sub2\backend
go build -o bin/server.exe ./cmd/server                                  # 期望 0
go test -tags=unit -count=1 ./internal/handler/... ./internal/server/... # 期望 0
gofmt -l internal/handler internal/server                                 # 期望空
cd F:\sub2\frontend
.\node_modules\.bin\vue-tsc.cmd -b                                       # 期望 0
node node_modules/vitest/vitest.mjs run                                   # 期望全绿
```

### 5.2 形状断言（新增测试，两处都要写）

| 断言 | 期望 |
|---|---|
| `/groups/available` 响应数组元素的 JSON key 集合 | `⊇` §2.5 的 15 个必需字段；**不含** `rpm_limit`/`max_reasoning_effort`/`image_price_1k`/`daily_limit_usd`/`claude_code_only`/`created_at` |
| `i18n` | 不出现（`Localized` 生效） |
| 用户域 snapshot `config` 的 key 集合 | `== ["refresh_interval_seconds"]` |
| 管理域 snapshot `config` | 仍含 `version/enabled/platforms/group_ids/health_thresholds/ignored_error_categories` |
| `/matrix`/`/dimensions`/`/models`/`/errors`/`/users` | 键集合与 §3.2 一致 |

### 5.3 变异验证（不做不算验证过）

1. 从用户面 mapper 里删掉 `allow_high_availability` → 上面的 key 集合断言必须失败；恢复。
2. 把 `UserFacingGroupFromService` 换成 `GroupFromService`（旧实现）→ 「不含 rpm_limit」断言必须失败；恢复。
3. 把 `SnapshotPublic` 换成 `Snapshot` → config key 集合断言必须失败；恢复。

### 5.4 实机抽查（部署后，5 项）

| # | 页面 | 期望 |
|---|---|---|
| 1 | API 密钥页 → 新建 Key 分组下拉 | 图标/名称/描述/倍率/高峰标记齐全，无 undefined |
| 2 | 分组链编辑器（编辑已有 Key） | Live 徽标与 Realtime 单价正常（配置了单价的分组） |
| 3 | 分组链徽标 | DeepSeek 峰谷徽标按状态显示/隐藏 |
| 4 | 新建 Key → HA 兜底候选 | 只出现管理员标记过的分组；勾选后保存成功 |
| 5 | 用量页 → 分组筛选 | 下拉列表有值，筛选生效 |

## 6. 风险与回滚

| 风险 | 触发条件 | 处置 |
|---|---|---|
| 前端某处显示 undefined | 白名单漏字段 | 以 §5.2 的 key 集合断言为准，先补断言再补字段 |
| 订阅进度条分母消失 | 误裁 `dto.Group` 本体 | 回滚该文件；只允许新增 mapper |
| 管理端分组编辑表单字段丢失 | 把 `AdminGroup` 也一起收窄 | `AdminGroup` 保持不动（`types.go:191`/`types.go:673`） |
| 监控页自动刷新停滞 | config 收敛后前端仍读 `config.enabled` | 前端只读 `refresh_interval_seconds`，`?? 300` 兜底已在位 |

回滚粒度：F-05 与 F-04 各自独立可回滚（前者改 2 个 handler/mapper + 前端类型，后者改 service + handler 的一个分支）。

## 7. 一页 checklist（提 PR 前自查）

- [ ] 没有改动 `dto.Group` / `groupFromServiceBase` / `GroupFromServiceShallow`
- [ ] 只有 `api_key_handler.go:500` 一处换成了用户面 mapper
- [ ] `Localized` 仍在用户面路径上被调用
- [ ] §2.5 的 15 个字段全部在响应里，且 `i18n` 不在
- [ ] 用户域 snapshot `config` 只剩 `refresh_interval_seconds`
- [ ] `/matrix`/`/dimensions`/`/models`/`/errors`/`/users` 形状未变
- [ ] 契约快照（`api_contract_test.go:318` 等）已同步更新
- [ ] 三条变异验证都做过并恢复
- [ ] `gofmt -l` 空、`vue-tsc -b` 0、`vitest run` 全绿
