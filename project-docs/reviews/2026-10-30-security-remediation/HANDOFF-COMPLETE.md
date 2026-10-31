# Sub2API 安全整改：交接文档（单份完整版）

> **这份文档自包含**：接手时不需要再读其他文件即可开工。需要更细的原始证据时，末尾「附录」列了按需查阅的路径。
>
> | 项 | 值 |
> |---|---|
> | 项目 | Sub2API 二开（Go 1.27.3 + Vue 3 + Ent + PostgreSQL + Redis） |
> | 分支 | `fix/security-remediation-20261030`（base `develop` @ `f9d8bc855`） |
> | PR | https://github.com/AnBooting/sub2api/pull/18 —— 11 提交，100 文件，+15073/−116 |
> | 线上 | https://sub2.lansn.icu/ （部署在 `develop` HEAD `f9d8bc855`，**本 PR 尚未上线**） |
> | 轮次 | 2026-10-30 ~ 10-31 |

---

## 1. 这一轮做完了什么

| 编号 | 内容 | 状态 |
|---|---|---|
| F-01 | **P0**：转发头信任方向反转；IP 限流可被伪造 `X-Real-IP` 绕过 → 修复 | ✅ |
| F-01b | 账号维度限流作为主防线（IP 坍缩也不失效）；停止"按请求次数"锁账号 | ✅ |
| F-02 | `/redeem` 独立限流（原判"无限流"已更正：既有 30 次/10 分钟，新增 10 次/10 分钟一层） | ✅ |
| F-03 | `admin@sub2api.local` 整域保留标识不可注册 | ✅ |
| F-07 | `/usage/:id` 越权与不存在同码同体（消除存在性泄露） | ✅ |
| N-01 | 未定价模型 503 的 `message` 从 `internal error` 改为可行动文案 | ✅ |
| N-02 | 统计未对账从 500 / 静默空数组 → 统一 503 + `usage_stats_incomplete` | ✅ |
| I-01a | 前端：SMTP 未配置时发码入口显式阻断；管理端下发只读 `smtp_configured` | ✅ |
| D-01 | 本地内嵌 `dist` 陈旧导致误判部署 | ✅ 文档 + 校验脚本 |
| D-02 | 内联 `__APP_CONFIG__.version` 空串 → 版本单一事实源（下次构建生效） | ✅ |
| D-03 | CDN 拓扑下 IP 坍缩 → `deploy/Caddyfile.edgeone` + compose 变量 + runbook | ✅ |
| P3-02 | `/admin/security-audit` 缺 redirect、`/admin/payment/channels` 死代码 | ✅ |
| **F-06** | 未知模型 404 细分 | ❌ **尝试后整体回退**（一行改动就能破坏"缺价不得放行"红线，见 §3.2） |
| **F-05/F-04** | 分组字段收敛 / 监控配置用户域收敛 | ⬜ **未做，见 §3.1** |
| N-03 / I-01b / I-02 / R-01 / P3-01 / O-01 | 见 §3.3–§3.8 | ⬜ 未做 |

**一个已澄清的误判**：用户曾反馈"前端好像没部署上去"。实测结论是**前端确实已部署**（`/api/v1/settings/public` 的 version 为 `0.2.17-fork.f9d8bc855`，产物级比对通过）。看起来"没变"的两个真实原因：① PR #7 前端视觉重设计（`feat/frontend-fluent-redesign-clean`）从未合并，线上仍是上游 Teal 主题；② 本地 `backend/internal/web/dist/` 曾被内嵌为 10-21 的陈旧产物，本地构建出的二进制因此带旧前端。

---

## 2. 当前验收状态（2026-10-30 21:35 工作树冻结后实跑）

| 检查 | 命令 | 结果 |
|---|---|---|
| 后端构建 | `go build -o bin/server.exe ./cmd/server` | **退出 0** |
| 后端单测 | `go test -tags=unit -count=1 ./...` | **退出 1**，仅两类红且均非本轮引入（见下） |
| 前端类型 | `vue-tsc -b` | **退出 0** |
| 前端单测 | `vitest run` | **退出 0**，435 files / 3456 tests 全过 |
| i18n 门禁 | `check:i18n` | **退出 0**（3 passed） |
| 格式 | `gofmt -l internal cmd` | 空 |

**两类可接受的失败（不要误判为自己引入）**：

| 失败 | 原因 | 处置 |
|---|---|---|
| 3 条 `TestPgDumper*`（`internal/repository/backup_pg_dumper_test.go`） | 本机 Windows 无 `sh` | 环境性；CI（Linux）不会红 |
| 1 条满载 flaky ⚠️ | `TestInflightEstimate_AccountMappingNoDBAndBoundedMemory` 或 `TestOpenAICompatibleScannerRequestContract`（两次运行命中的不是同一条，属满载偶发） | 隔离 `-count=3` 通过即可 |

`verify-deployed-build.ps1` 会把"前端 dist hash 不一致"报成 DRIFT —— 这是**弱判据**（重建 dist 改变入口 hash，构建不可复现），默认不影响退出码；强判据是后端 `version` 与本地 HEAD 一致。

---

## 3. 还没做完的事（本交接的核心）

推荐顺序：**§3.1（F-05+F-04 一个 PR）→ §3.2（F-06 单独 PR）→ §3.3–§3.7 打包一个 PR → §3.8 单独 → PR #7 最后**。
理由：§3.1 输入最完整、收益最确定；§3.2 风险最高必须隔离；杂项打包减少评审轮次。

### 3.1 F-05 + F-04：字段收敛（有完整输入，可直接开工）

#### 3.1.1 目标

- **F-05**：`GET /api/v1/groups/available` 返回的分组对象从 40+ 字段收敛到用户界面真正渲染的字段。
- **F-04**：`GET /api/v1/channel-monitor-v2/snapshot`（用户域，`admin=false`）的 `config` 收敛为只剩 `refresh_interval_seconds`。

#### 3.1.2 硬约束（违反会连带坏别的功能）

1. **绝对不要裁剪 `dto.Group` 本体**（`backend/internal/handler/dto/types.go:107`）。它经 `GroupFromServiceShallow`（`dto/mappers.go:130`）→ `groupFromServiceBase`（`mappers.go:217`）被 **6 处**内嵌复用：

   | 内嵌点 | 定义 | 裁掉后的用户可见后果 |
   |---|---|---|
   | `dto.APIKey` | `types.go:92` / `mappers.go:113` | `KeysView.vue:1094-1095` 的 `claude_code_only`/`allow_messages_dispatch` 丢失 → Claude Code 客户端选择器与教程判据失效；`useBatchImageAccess.ts:16`、`BatchImageGuideView.vue:945` 的 `key.group.allow_batch_image_generation` 丢失 → 批量生图入口误判 |
   | `dto.UserSubscription` | `types.go:809` / `mappers.go:953` | `SubscriptionsView.vue:108-225`、`common/SubscriptionProgressMini.vue:75-230` 的 `daily/weekly/monthly_limit_usd` 丢失 → **订阅进度条分母变 undefined** |
   | `dto.UsageLog` | `types.go:705` / `mappers.go:789` | `admin/usage/UsageTable.vue:122` 的 `row.group.name` 丢失 → 用量表分组列空白 |
   | `dto.RedeemCode` | `types.go:556` / `mappers.go:697` | 兑换码关联分组展示 |
   | `dto.AccountGroup` | `types.go:464` / `mappers.go:557` | 账号-分组关联展示 |
   | `dto.Account.groups[]` | `mappers.go:492` | 账号详情里的分组列表 |

2. **新增独立用户面 mapper，只接 `api_key_handler.go:500` 一处**（`GetAvailableGroups`，路由 `routes/user.go:93`）；`mappers.go` 其余调用点保持 `GroupFromServiceShallow` 不变。
3. **`Localized(Accept-Language)` 必须仍在用户面路径上**（`dto/group_localize.go:8`）：否则 user 侧文案不本地化、还会多出 `i18n` 字段。

#### 3.1.3 白名单（照抄）

```text
必需 15 个:
id, name, description, platform, rate_multiplier, subscription_type, status,
peak_rate_enabled, peak_start, peak_end, peak_rate_multiplier,
deepseek_pricing_state, allow_live, audio_realtime_price_per_min, allow_high_availability

可选保留 3 个（不读但无害）:
is_exclusive, allow_image_generation, allow_batch_image_generation
```

**极易踩的认知陷阱（务必照抄这条判据）**：字段名在前端被读 ≠ 该端点需要它。必须同时满足 ① 读取点的类型是 `Group`；② 该 `Group` 来自 `getAvailable()` 的赋值。这条判据纠正了初版清单的 5 个误判：

| 字段 | 真实来源 | 结论 |
|---|---|---|
| `long_context_pricing_enabled` | `/model-plaza` 的自有白名单 `ModelPlazaGroup`（`api/modelPlaza.ts:59,81`；后端 `modelPlazaGroup` `model_plaza_handler.go:81`） | 不需要 |
| `image_rate_independent` | 同上（`modelPlaza.ts:75`） | 不需要 |
| `video_rate_independent` | 同上（`modelPlaza.ts:78`） | 不需要 |
| `claude_code_only` | `dto.APIKey.Group`（`KeysView.vue:1094` 读的是 `selectedKey.group`） | 不需要 |
| `allow_messages_dispatch` | 同上（`KeysView.vue:1095`） | 不需要 |

前端只有 2 处调用 `userGroupsAPI.getAvailable()`：`views/user/KeysView.vue:1674`（→ `groups` @ `:1399`）与 `views/user/UsageView.vue:872`（→ `groups` @ `:426`，只用于分组筛选下拉）。二者组成的实际读取集就是上面 15 个字段（逐条 `文件:行号` 证据见附录「FIELD-INVENTORY.md」§2.1）。

可从用户面安全剔除的例子：`rpm_limit`、`max_reasoning_effort`、`max_reasoning_effort_over_limit`、`reasoning_effort_mappings`、`require_oauth_only`、`require_privacy_set`、`image_price_1k/2k/4k`、`video_price_480p/720p/1080p`、`video_model_prices`、`video_price_plans`、`web_search_price_per_call`、`search_price_per_1k`、`audio_tts_price_per_million_chars`、`audio_stt_price_per_hour`、`batch_image_discount_multiplier`、`batch_image_hold_multiplier`、`image_rate_multiplier`、`video_rate_multiplier`、`fallback_group_id`、`fallback_group_id_on_invalid_request`、`default_mapped_model`、`messages_dispatch_model_config`、`scheduling_strategy`、`effective_scheduling_strategy`、`created_at`、`updated_at`、`i18n`。

> `created_at`/`updated_at` 在 KeysView/UsageView 里是**表格列 key 与排序参数**，不是 group 字段；`i18n` 只属于 `AdminGroup`。

#### 3.1.4 后端实现（F-05）

新增文件 `backend/internal/handler/dto/group_user_facing.go`（不放进 `types.go`，减少上游冲突面）：

```go
// UserFacingGroup 是 /groups/available 的响应白名单：只含用户界面上真正渲染的字段。
// 注意：不是 dto.Group 的替代品——dto.Group 仍被 6 类响应内嵌复用，不得裁剪。
type UserFacingGroup struct {
    ID                       int64                    `json:"id"`
    Name                     string                   `json:"name"`
    Description              string                   `json:"description"`
    Platform                 string                   `json:"platform"`
    RateMultiplier           float64                  `json:"rate_multiplier"`
    SubscriptionType         string                   `json:"subscription_type"`
    Status                   string                   `json:"status"`
    PeakRateEnabled          bool                     `json:"peak_rate_enabled"`
    PeakStart                string                   `json:"peak_start"`
    PeakEnd                  string                   `json:"peak_end"`
    PeakRateMultiplier       float64                  `json:"peak_rate_multiplier"`
    DeepSeekPricingState     *DeepSeekPricingStateDTO `json:"deepseek_pricing_state,omitempty"`
    AllowLive                bool                     `json:"allow_live"`
    AudioRealtimePricePerMin *float64                 `json:"audio_realtime_price_per_min"`
    AllowHighAvailability    bool                     `json:"allow_high_availability"`
}

// Localized 与 Group.Localized 用同一条语言选择规则；本结构没有 i18n 字段可清空。
func (g *UserFacingGroup) Localized(i18n domain.GroupI18n, acceptLanguage string) *UserFacingGroup {
    if g == nil {
        return nil
    }
    g.Name, g.Description = i18n.Localize(acceptLanguage, g.Name, g.Description)
    return g
}

func UserFacingGroupFromService(g *service.Group) *UserFacingGroup
```

只改 `api_key_handler.go:500`：

```go
lang := c.GetHeader("Accept-Language")
out := make([]dto.UserFacingGroup, 0, len(groups))
for i := range groups {
    g := &groups[i]
    out = append(out, *dto.UserFacingGroupFromService(g).Localized(g.I18n, lang))
}
```

同时更新契约快照 `backend/internal/server/api_contract_test.go:318` 的 `wantJSON`（该用例已经断言"不含 model_routing/account_count/profit_control"，是天然的收敛护栏）。

#### 3.1.5 后端实现（F-04）

**不要改 `ChannelMonitorV2Snapshot`（`service/channel_monitor_v2.go:211`）的字段类型** —— 它是单结构体，直接改会波及 `repository/channel_monitor_v2_repo_test.go:239`（断言 `snapshot.Config == service.ChannelMonitorV2Config{}`）。

做法：

1. 新增 `ChannelMonitorV2PublicConfig{ RefreshIntervalSeconds int `json:"refresh_interval_seconds"` }`。
2. 新增 `ChannelMonitorV2PublicSnapshot`（`Config` 用上面的类型，`Coverage/Metrics/Health/Trend` 复用现有类型）+ `SnapshotPublic()`。
3. 用户域入口 `channel_monitor_v2_handler.go:93` `Snapshot`（内部调 `:102` 的私有 `snapshot(c, false)`）改调 `SnapshotPublic`；`admin=true` 仍走 `Snapshot`。
4. 删 `redactChannelMonitorV2PublicConfig`（`service:621`）与 `redactChannelMonitorV2Snapshot`（`:606`）里的 config 那一行；**metric 脱敏（`:637`）保留不动**。

目标形状：`"config": { "refresh_interval_seconds": 300 }`，只这一个 key。

**为什么剔除安全**：`errors[].ignored` 由服务端在查询时算出（`repository/channel_monitor_v2_repo.go:729-730` 用 `ignoredSet` 赋值），**不依赖响应里的 `config.ignored_error_categories`**；前端 `api/channelMonitorV2.ts:197` 只读该布尔值。因此 `config` 收敛不会让错误分类置灰失效。

**其余 5 个端点的形状必须逐字不变**：`/matrix`、`/dimensions`、`/models`、`/errors`、`/users`（前端 `views/user/ChannelStatusV2View.vue` + `features/channel-monitor-v2/{RelayPulseMatrix,MonitorTrendChart}.vue` 依赖）。`MonitorSettingsPanel.vue` 只打 `/admin/channel-monitor-v2/config`，不受影响。

#### 3.1.6 前端改动（F-05/F-04）

1. `frontend/src/types/index.ts:605` 的 `Group` 拆两份：`UserFacingGroup`（15 字段）、`Group`（保留全量形状供 admin/内嵌响应用）；`AdminGroup`（`:673`）**不动**。
2. `frontend/src/api/groups.ts:16` 返回类型改 `UserFacingGroup[]`；`KeysView.vue:1399`、`UsageView.vue:426` 的 `ref<Group[]>` 同步改。
3. props 收窄只做"变可选 + 缺省降级"，不改渲染逻辑：`GroupChainEditor.vue:73`、`KeyHAGroupPicker.vue:37`、`KeyGroupChain.vue:22`、`BulkEditKeysModal.vue:173`。`GroupBadge.vue`、`GroupOptionItem.vue`、`GroupLiveBadge.vue`（已是 `Pick` 入参）、`GroupPricingStateBadge.vue`（已是 `unknown` 入参）无需改。
4. **不要**把 `is_exclusive` 等从类型里删掉：`channels/AvailableChannelsTable.vue:364`、`modelPlaza/PlazaGroupSection.vue:24` 仍在读，它们走的是另一条响应。
5. F-04：`frontend/src/api/channelMonitorV2.ts:101` 的 `MonitorConfig` 拆为 `MonitorConfig`（admin，不动）+ `MonitorPublicConfig`（`{ refresh_interval_seconds: number }`）；`:148` `MonitorSnapshot.config` 改用后者。`ChannelStatusV2View.vue:819` 已是可缺省读取（`?? 300`），无需改。

#### 3.1.7 必过的测试与变异验证

| 断言 | 做法 |
|---|---|
| `/groups/available` 响应元素 key 集合 ⊇ 15 个必需字段 | 新增单测或改写 `api_contract_test.go:318` |
| 响应**不含** `rpm_limit`/`max_reasoning_effort`/`image_price_1k`/`daily_limit_usd`/`claude_code_only`/`created_at`/`i18n` | 同上 |
| 用户域 snapshot `config` 的 key 集合 **==** `["refresh_interval_seconds"]` | 新增 |
| 管理域 snapshot `config` 仍含 `version/enabled/platforms/group_ids/health_thresholds/ignored_error_categories` | 新增 |
| 变异 1：删掉 `allow_high_availability` → key 集合断言必须红；恢复 | 手工做一次并记录 |
| 变异 2：换回 `GroupFromService` → "不含 rpm_limit" 断言必须红；恢复 | 手工做一次并记录 |
| 变异 3：`SnapshotPublic` 换回 `Snapshot` → config key 断言必须红；恢复 | 手工做一次并记录 |

部署后人工抽查 5 项：① 新建 Key 分组下拉（图标/名称/描述/倍率/高峰标记齐全，无 undefined）；② 分组链编辑器 Live 徽标与 Realtime 单价；③ DeepSeek 峰谷徽标按状态显示/隐藏；④ HA 兜底候选只出现管理员标记过的分组；⑤ 用量页分组筛选下拉有值且生效。

### 3.2 F-06：未知模型 404 细分（**风险最高，必须单独 PR**）

本轮已尝试并**整体回退**。原因：它的一行调用（`refinePricingRejection`）就能把"缺价"`ErrModelPricingUnavailable` 改判成"模型不存在"，落进 `writeError` 的未知分支 → 兜底 **502 且请求穿透到上游**（5/5 子测试 `expected 503, actual 502 upstream_error`），直接破坏项目红线"缺价不得放行"。当时已有的 7 条分支级探针**全部通过**。

下一轮的硬要求：

1. 路由集合判定与缺价判定**严格分离**，不许在同一处改写错误类型；
2. 任何新错误类型必须先在 `writeError` 的各协议分支注册，否则兜底 502 并继续走上游；
3. **不改** `gateway_handler.go:2551-2560` 的 `billingErrorDetails`（缺价恒 503 + `type=pricing_unavailable`）；
4. mandatory 回归（既有，不需新写）= `TestOpenAIChatCompletionsLockedPricingMissingRejectedBeforeUpstream`（`handler/openai_chat_completions_locked_pricing_test.go:159`）：5 种缺价形态 ×（503 + `upstream.callList()` 空 + `ledger.commands` 空）；
5. **动手前先在 `develop` 上跑一次该测试确认基线绿**；
6. 实现参考：`handler/no_account_error.go` 的 404 `model_not_found` 分类（`:34`、`:101` `classifyNoAccountError`、`:132`）。

### 3.3 I-01b：SMTP 未配置时后端要真报错

- 现状：`service/auth_service.go:345` `SendVerifyCodeAsync` 只判 `emailQueueService == nil`（:373-374），入队失败也只 `fmt.Errorf`（:385）；SMTP 未配置时后端仍"假成功"，运维发现不了。前端阻断（I-01a）已完成。
- 改：在 :371 之前插入 `EmailService.IsConfigured` 检查，未配置直接返回 `service.ErrEmailNotConfigured`（已存在，`service/email_service.go:27`，`ServiceUnavailable("EMAIL_NOT_CONFIGURED", …)`）。同源方法 `SendVerifyCode`（:341 调用点）一并处理。
- 必过测试：新增 `TestSendVerifyCodeAsync_ReturnsEmailNotConfigured_WhenSMTPMissing`，断言 `errors.Is(err, service.ErrEmailNotConfigured)` **且** `EnqueueVerifyCode` 未被调用（spy）。
- 变异：删掉检查，测试必须红。

### 3.4 N-03：空 `messages` 必须 400

- 现状：`handler/gateway_handler_chat_completions.go:23` `ChatCompletions` 只在 :59 判空 body、:75 判 `model`，**没有** `messages` 非空校验 → `messages: []` 被转发上游并按 prompt_tokens≈35 计费（线上复现 4 次）。
- 改：在 `model` 校验之后加 `messages` 非空且为数组的检查，返回 `400 invalid_request_error`（文案 `messages is required`）。同一校验要覆盖 `/v1/messages`（`gateway_handler.go:126`）与 `/v1/responses` 入口，否则只堵一半。
- 必过测试：各入口一条，断言 **400 + `upstream.callList()` 为空 + `ledger.commands` 为空**（与缺价红线同一套断言风格）。
- **不要只在前端拦**：这是网关契约。

### 3.5 I-02：`/setup/status` 硬编码

- 现状：`server/routes/common.go:23-31` 恒返回 `needs_setup:false, step:"completed"`。
- 改：读真实初始化状态（setting 表是否已有站点配置）。注意这是匿名端点、且被 `middleware/logger.go:49` 排除出日志 —— 只暴露布尔 + 阶段枚举，不要泄露内部状态。
- 必过测试：未初始化库 → `needs_setup:true`；已初始化 → `false`。

### 3.6 R-01：service 层 429 缺 `Retry-After`

- 现状：`service/redeem_service.go:23` `ErrRedeemRateLimited = infraerrors.TooManyRequests(...)`，:347 返回；经 `response.ErrorFrom` 输出时没有 `Retry-After` 头。本轮新增的中间件层（`server/middleware/panel_rate_limit.go:238` `RedeemGuard`）会写该头，线上多数情况已被覆盖，但 service 层这条仍是既有缺陷。
- 改：优先 ① 让 429 错误携带 retry-after 元数据、在写响应处统一落头（其他 429 同样受益）；② 退路是在 `Redeem` handler 里显式 `c.Header("Retry-After", …)`。
- 必过测试：命中限额后断言 `Retry-After` 非空且为正整数。

### 3.7 P3-01：分页参数越界静默钳制

- 现状：`pkg/response/response.go:170` `ParsePagination` 对 `page/page_size` 非法值静默回落/钳制（92 处调用）。
- **前提：这是契约变更**，会改变 92 个端点的行为。建议只在明显越界（`page<=0`、`page_size<=0`、`page_size>上限`）时报 400，其余保持宽松；上线前同步前端调用点，否则会把正常页面打挂。
- 不建议与其他项混在一个 PR。

### 3.8 O-01：`PUT /user` 冷路径 5.0s

- 现象：`handler/user_handler.go:163` `UpdateProfile` 冷启动首次请求 5.0s，命中 EdgeOne 回源超时返回 **522**（非应用 5xx），重试即 200。
- **尚未定位根因。** 下一步取证（不要猜）：① 冷路径分段打点（各 DB 往返耗时）；② 是否有连接池首次建连 / 迁移检查落在请求路径上；③ 与 `/health` 预热做对照。
- 取得证据前**不要改代码**。

### 3.9 PR #7：前端视觉重设计（CLOSED）

`feat/frontend-fluent-redesign-clean` 跨 105 文件，从未合并（线上仍是上游 Teal 主题：部署的 CSS 里 `#14b8a6` 出现 30 次、Azure `#1a9dce` 0 次）。需独立 rebase 到 `develop` 后单开一轮，并做视觉回归。

---

## 4. 必须由人做的事（不做则不生效）

### 4.1 🔴 填 EdgeOne 回源网段

`deploy/Caddyfile.edgeone` 第 74 行：

```caddyfile
trusted_proxies static 198.51.100.0/24 2001:db8:beef::/48 # REPLACE_WITH_EDGEONE_ORIGIN_CIDRS
```

换成腾讯云控制台 → EdgeOne → 该站点「回源配置/源站信息」里的**回源 IP 段**（不是客户端 IP 段），或提工单索取。

**不填不会报错，只会静默出错**：Caddy 不认为请求来自可信代理 → 不采信 `EO-Connecting-IP`/`X-Forwarded-For` → 全站 client_ip 坍缩成回源节点的几个 IP。表现：限流把所有用户算成同一个人（容易全站 429）、审计日志 `client_ip` 恒等于 `remote_ip`、按 IP 限流完全失效。

### 4.2 🔴 两步上线，不要一次全上

本轮有一处会**改变全站行为**的开关：`security.trust_forwarded_ip_for_api_key_acl`。

| 步骤 | 上什么 | 风险 |
|---|---|---|
| **第 1 步（可观测，不改判定）** | 账号维度限流、`/redeem` 限流、API 面收敛、N-01/N-02 错误口径、前端降级、部署脚本与文档 | 低。最坏是限流偏紧或文案变化 |
| **第 2 步（改判定）** | `server.trusted_proxies` 生效 + `trust_forwarded_ip_for_api_key_acl=false` | **高**。Caddy/EdgeOne 配错 → 全站 429 |

第 2 步的三个前置：① 4.1 已完成且验证过（两个不同出口 IP 得到两个不同 `client_ip`）；② 第 1 步稳定运行至少一天、无异常 429；③ 回滚就绪（把 `SECURITY_TRUST_FORWARDED_IP_FOR_API_KEY_ACL` 改回 `true` 并重启，不需要回滚镜像）。

环境变量模板见 `deploy/.env.example`（`SERVER_TRUSTED_PROXIES=172.18.0.0/16` 是 **Caddy 容器网段**，不是 EdgeOne 网段 —— 两层信任链不要混）。

### 4.3 🔴 发布后立刻跑两项自检

```powershell
pwsh -File tools\verify-deployed-build.ps1 -BaseUrl https://sub2.lansn.icu
```

| 退出码 | 含义 | 处置 |
|---|---|---|
| 0 | 强判据通过（后端 `version` 里的 commit == 本地 HEAD） | 继续 |
| 1 | DRIFT：线上跑的不是你构建的代码 | **回滚**，不要继续 |
| 2 | 前置条件失败（连不上/认证失败） | 先排查连通性 |

再跑 `project-docs/08-runbook.md` §8 的 5 条限流绕过自检。其中第 1 条是**本轮核心修复的回归判据**：轮换 `X-Real-IP` 连续 25 次请求 → 必须出现 429（修复前是 25 次全 401、0 次 429）。若仍是全 401，立刻停下查 `SERVER_TRUSTED_PROXIES`。

### 4.4 🔴 数据清理需要授权（涉及删数据，本轮只读未动）

| 对象 | 标识 | 建议 |
|---|---|---|
| 测试用户 | `uid=5,6,7,8`（`@example.invalid`） | 可删 |
| 测试用户 | `uid=9`（`admin@sub2api.local`，`role=user`） | 可删；F-03 已阻断今后注册同域 |
| API Key | `uid=3` 下的 `probe-7255`、`probe-g3` | 可删 |
| 兑换码 | `id=1,2,3`（历史遗留，与本轮无关） | 先确认无用户依赖 |
| 用量日志 | 上述 Key 产生的 `usage_logs` | **建议保留**，仅标记 |

**不要直接 `DELETE FROM users`** —— 走后台「用户管理 → 删除」软删除，先确认级联行为。

### 4.5 ⚪ 换网段后重跑头部语义回归

EdgeOne 回源网段更新后，重跑 `08-runbook.md` §9.4 的检查（确认 `EO-Connecting-IP` 没被上游覆盖）。

---

## 5. 工作纪律（本轮实际踩过，别再犯）

1. **改共享文件前先 `git show develop:<path>` 取基线全文，按行插入，绝不整文件覆盖。** 本轮一次整文件覆盖把 `gateway_locked_billing_helper.go` 里既有的 `apply` 与 `validateSharedGeminiImagePricing` 删掉了（项目明文红线）。
2. **分支级探针不能替代端到端断言。** 见 §3.2：7 条探针全绿，端到端仍 502 且穿透上游。
3. **判定"既有失败"必须做基线对照**（`git show develop:` 或 A/B 只换回单个文件），不能靠"这个文件我没改过"推断。本轮有一次因此误判，靠 A/B 才定位到是本轮引入。
4. **多文件改动一次写完再落盘；跨包改动先落被依赖符号。** 本轮 4 次主工作区编译红，全部是"引用先于定义落盘"。
5. **不要在 `main` 提交**（`main` 是上游镜像，只用于 `git fetch upstream`）。
6. **不改写已存在的迁移文件**；新迁移从 **259** 起（`backend/migrations/` 当前最新 258）。
7. **缺价不得放行**：未定价模型必须在上游请求前拒绝，不得按零价或回落 GPT 兜底价。
8. 前端验证必须 `corepack pnpm@9`（全局 pnpm 11 与上游锁文件不兼容）；也可直接 `node_modules\.bin\vue-tsc.cmd -b` + `node node_modules/vitest/vitest.mjs run`。

---

## 6. 怎么把改动拿到自己的仓库

**方式 A（最简单）**

```powershell
git fetch origin
git switch -c review/pr18 origin/fix/security-remediation-20261030
```

**方式 B（无 PR 权限／想逐提交看）**

```powershell
git checkout develop
git am -3 <PR18 各提交的 .patch>     # 按时间顺序
# 或只要最终状态：
git apply --3way <PR18 合并 diff>
```

> 补丁基于 `develop` 的 `f9d8bc855`。若 `develop` 已前进，用 `git am -3` 做三方合并。

---

## 7. 附录：需要更细证据时查哪里

全部在 `project-docs/reviews/2026-10-30-security-remediation/`：

| 文件 | 用途 |
|---|---|
| `FIELD-INVENTORY.md` | F-05/F-04 的逐字段证据表（每个字段的 `文件:行号` 读取点）、复核方法、完整验收与回滚 |
| `ACTION-ITEMS.md` | 人工事项全文（EdgeOne 网段怎么取、5 条限流自检、数据清理清单） |
| `VERIFICATION.md` | 验证记录：命令、退出码、通过数、变异验证逐条 |
| `REVIEW-FINAL-VERDICT.md` | 独立审查裁决（6 条必答、6 点变异、红线合规结论） |
| `EVIDENCE.md` | 线上实测证据（含 F-02 阈值更正、7 条新发现） |
| `PLAN.md` | 本轮整改计划与两步上线顺序 |
| `E2E-REPORT.md` | 17 页 E2E 基线与改动清单 |
| `HANDOFF.md` | 本轮交付小结与三条教训 |
| `WORKSPACE-CLEANUP.md` | 工作区与 Git 清理记录 |
| `QA-FINAL-REPORT.json` / `QA-CONSOLIDATED.json` | 线上功能矩阵与受控探测原始结论（API Key 已脱敏） |
| `HANDOFF-COMPLETE.md` | **本文件** |

项目规则与真实命令：仓库根 `AGENTS.md`。部署与运维：`project-docs/08-runbook.md`（§8 限流自检、§9 EdgeOne 网段）。
