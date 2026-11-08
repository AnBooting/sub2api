# REVIEW-CONTRACT — PR #19 F-05 / F-04 公共契约评审

> 只读评审：未修改业务源码、未操作 Git 索引/提交/推送、未改 worktree 文件。本文件是唯一写域产物。
> 评审对象：PR #19 AnBooting/sub2api「fix(security): 收敛用户响应字段并阻止 SMTP 未配置时发码」，作者 Akanyi。
> 本文只覆盖 task-6（F-05 + F-04）；SMTP（I-01b）由 task-7 负责。

| 项 | 值 |
|---|---|
| head / base | dev/security-public-contracts-smtp-20261108 @ 849c14a7b / develop @ 4a0064318 |
| 代码快照 | F:\sub2\.agents\worktrees\pr19-review-20261108（工作树干净：git status 仅分支行） |
| 规格基准 | FIELD-INVENTORY.md §2/§3/§5；23-handoff §3.1；PR18 RESOLUTION.md |
| 环境 | Go 1.27.3（GOROOT=F:\tools\go1.27.3\go，GOTOOLCHAIN=local）；前端 vue-tsc（worktree 内已装 node_modules）；本机无 docker |
| 评审时间 | 2026-11-08 |
| 评审者 | pr19-contract-reviewer（Lead 团队） |

## 0. 裁决建议

- **F-05 `GET /groups/available` 字段收敛：通过（Pass）**。白名单 15 字段与 FIELD-INVENTORY §2.5 逐字一致；未多下发任何非白名单字段；`dto.Group` 本体与 6 处内嵌消费方未改动；`Localized(Accept-Language)` 仍在用户面路径；契约测试由「50 字段全量快照」改为「精确 15 键」的 JSONEq，**不是削弱而是更强**；前端类型拆分与消费面完整（`vue-tsc -b --force` 退出 0）。
- **F-04 用户域 snapshot `config` 收敛：通过（Pass）**。用户域 `config` 严格只剩 `refresh_interval_seconds`；`admin=true` 快照与 `/admin/channel-monitor-v2/config` 未变；其余 5 端点代码路径未改；`errors[].ignored` 仍由服务端查询时按服务端 config 计算，不依赖响应里的 `config`。
- **整体：无 Critical、无 Required，建议 Approve**。Nit 2 条、Optional 2 条、FYI 1 条（§4），均不阻塞合并；建议 Lead 先跑 §5 的变异验证（至少 M-01/M-02/M-04/M-05/M-06）再合并。
- 遗留风险提示（非阻塞）：`/groups/available` 字段删除是**有意的破坏性 API 变更**，发布说明需注明；另有 1 处既有用户面全量 group 下发（§4 C-05，超出 F-05 声明范围）。

## 1. 实际运行的命令与退出码

| # | 命令（cwd） | 退出码 | 结果摘要 |
|---|---|---|---|
| 1 | `git -C <worktree> status --porcelain=v1 -b` | 0 | 仅 `## pr19-review-20261108`，工作树干净 |
| 2 | `git -C <worktree> diff --stat origin/develop...HEAD` | 0 | 20 files changed, 426 insertions(+), 108 deletions(-) |
| 3 | `go version; go env GOROOT GOTOOLCHAIN` | 0 | go1.27.3 windows/amd64；F:\tools\go1.27.3\go；local |
| 4 | `go test -p 1 -tags=unit ./internal/handler ./internal/server ./internal/service ./internal/handler/dto -count=1`（worktree/backend） | **0** | `ok internal/handler 239.962s`；`ok internal/server 4.099s`；`ok internal/service 245.492s`；`ok internal/handler/dto 3.051s` |
| 5 | `go test -p 1 -tags=unit ./internal/server -run 'TestAvailableGroupsPublicFieldsAndLocalization|TestAPIContracts' -count=1 -v` | **0** | `TestAPIContracts/GET_/api/v1/groups/available` PASS；`TestAvailableGroupsPublicFieldsAndLocalization`（en-US / zh）PASS |
| 6 | `go test -p 1 -tags=unit ./internal/handler -run TestChannelMonitorV2SnapshotConfigResponseContract -count=1 -v` | **0** | user / admin 两个子用例 PASS |
| 7 | `go test -p 1 -tags=unit ./internal/service -run TestSnapshotRedactsPublicConfigPolicyFields -count=1 -v` | **0** | PASS |
| 8 | `gofmt -l`（9 个改动 Go 文件） | 0 | 输出为空（无未格式化文件） |
| 9 | `frontend: .\node_modules\.bin\vue-tsc.cmd -b`（worktree） | **0** | `VUE_TSC_EXIT=0` |
| 10 | `frontend: .\node_modules\.bin\vue-tsc.cmd -b --force`（worktree） | **0** | `VUE_TSC_FORCE_EXIT=0`（强制全量重建，排除 tsbuildinfo 增量跳过） |
| 11 | `go build ./cmd/server` / 前端 vitest / golangci-lint / govulncheck | **未运行** | 见 §6 未覆盖项（Lead 侧已报 build 0、前端 typecheck/build 0） |

证据抓取方式：`grep` 工具（ripgrep）按文件/目录检索；行号以 worktree @ 849c14a7b 当前源码为准。

## 2. F-05：`GET /api/v1/groups/available` 核对

### 2.1 白名单与规格（A1 / A2）

`backend/internal/handler/dto/group_user_facing.go:10-26` 的 `UserFacingGroup` 字段与 FIELD-INVENTORY §2.5 / handoff §3.1.3 逐字比对：

| # | 规格必需字段 | 实现（JSON tag） | 文件:行 |
|---|---|---|---|
| 1 | id | `id` | group_user_facing.go:11 |
| 2 | name | `name` | :12 |
| 3 | description | `description` | :13 |
| 4 | platform | `platform` | :14 |
| 5 | rate_multiplier | `rate_multiplier` | :15 |
| 6 | subscription_type | `subscription_type` | :16 |
| 7 | status | `status` | :17 |
| 8 | peak_rate_enabled | `peak_rate_enabled` | :18 |
| 9 | peak_start | `peak_start` | :19 |
| 10 | peak_end | `peak_end` | :20 |
| 11 | peak_rate_multiplier | `peak_rate_multiplier` | :21 |
| 12 | deepseek_pricing_state | `deepseek_pricing_state,omitempty` | :22 |
| 13 | allow_live | `allow_live` | :23 |
| 14 | audio_realtime_price_per_min | `audio_realtime_price_per_min` | :24 |
| 15 | allow_high_availability | `allow_high_availability` | :25 |

结论：**15/15 命中，顺序与规格一致，无多余字段**；字段类型与既有语义一致（`deepseek_pricing_state` 保留 `omitempty` 指针；`audio_realtime_price_per_min` 保留 `*float64`，未改成默认 0，符合 §2.2.3）。

规格「可选保留（3，不读但无害）」：实现**未保留** `is_exclusive` / `allow_image_generation` / `allow_batch_image_generation`，取严格最小集。核对这三者在本端点确无读取点（以下读取点的数据源均为**另一条**响应）：

- `is_exclusive`：`components/channels/AvailableChannelsTable.vue:364,368`（数据源 `/channels/available`，`api/channels.ts:22`）；`components/modelPlaza/PlazaGroupSection.vue:24`（数据源 `/model-plaza`，`api/modelPlaza.ts:73`）；其余在 admin `GroupsView.vue` / `UsersView.vue:1094,1111` / `apiKeyGroupFilterOptions.ts:54`（AdminGroup）。
- `allow_batch_image_generation`：`composables/useBatchImageAccess.ts:16`、`views/user/BatchImageGuideView.vue:945` 均读 `key.group`（`dto.APIKey` 内嵌 Group，另一条响应）；其余在 admin。
- `allow_image_generation`：仅 admin `GroupsView.vue` 与测试。
=> 判定：**可接受且比规格更收敛**；不算发现。风险点仅在未来若前端改从 getAvailable 读这三字段会拿不到，类型系统已在 `UserFacingGroup` 层面拦截。

**剔除字段核对**：`UserFacingGroup` 结构体本身只有 15 个字段，清单中的 `rpm_limit`、`max_reasoning_effort(_over_limit)`、`reasoning_effort_mappings`、`require_oauth_only`、`require_privacy_set`、`image_price_1k/2k/4k`、`video_price_480p/720p/1080p`、`video_model_prices`、`video_price_plans`、`web_search_price_per_call`、`search_price_per_1k`、`audio_tts/stt` 价格、`batch_image_*`、`image/video_rate_multiplier`、`fallback_group_id(_on_invalid_request)`、`default_mapped_model`、`messages_dispatch_model_config`、`scheduling_strategy`、`effective_scheduling_strategy`、`created_at`、`updated_at`、`i18n` **在结构体上不存在**，且由两条契约测试断言（§2.4）。

补充：`deepseek_pricing_state` 虽为 `omitempty`，但生产恒有值（`service/group.go:427-436` 的 `DeepSeekPricingStateAt` 无论平台都返回非 nil state，不适用时 state=none），因此「精确 15 键」断言不是 flaky 断言。

### 2.2 接线范围与内嵌消费方（A3）

- `git diff origin/develop...HEAD -- backend/internal/handler/api_key_handler.go`：**只有 1 个 hunk**（`GetAvailableGroups`，worktree `api_key_handler.go:485-505`；接线 `:498-504`）。`make([]dto.Group…)` → `make([]dto.UserFacingGroup…)`，mapper 换 `dto.UserFacingGroupFromService(g).Localized(g.I18n, lang)`。
- `dto/types.go`、`dto/mappers.go` 在本 PR **零改动**（diff --stat 20 文件清单中没有这两个文件；`git diff origin/develop...HEAD -- backend/internal/handler/dto/types.go backend/internal/handler/dto/mappers.go` 为空）。`dto.Group` 仍是 `types.go:107-187` 的 50 字段全量形状。
- 6 处内嵌消费方未受影响（`GroupFromServiceShallow` 调用点）：`mappers.go:113`（dto.APIKey）、`:492`（dto.Account.groups[]）、`:557`（dto.AccountGroup）、`:697`（dto.RedeemCode）、`:789`（dto.UsageLog）、`:953`（dto.UserSubscription）。
- 同一 handler 内其它用户面路径未改（例如 `writeHAEnablementResponse` 仍用 `dto.GroupFromService`，属既有行为，见 §4 C-05）。

### 2.3 Localized 仍在用户面路径（A4）

- `group_user_facing.go:43-48`：新类型 `Localized(i18n domain.GroupI18n, acceptLanguage string)` 调用与 `dto/group_localize.go:12` **同一个** `i18n.Localize(acceptLanguage, name, description)`，没有另写回退规则；新类型无 `I18n` 字段可清空（等价于原 `Group.Localized` 的 `g.I18n = nil` 效果）。
- 调用方直接传 `g.I18n`（`api_key_handler.go:502`），语言选择语义与旧路径一致。
- 测试证据：`available_groups_public_contract_test.go:27-30` 两个 `Accept-Language` 子用例（`en-US,en;q=0.9` → Translated；`zh` → 原名/原描述）均通过（命令 #5，退出 0）。

### 2.4 契约测试是否被削弱（A5）

1. `api_contract_test.go` 比对方式**未变**：`require.JSONEq(t, tt.wantJSON, body)`（`api_contract_test.go:1411`，精确比较整包，不是子集断言）。本 PR 只改该用例的 `wantJSON` 字面量（`:351-374`），把原 50 个分组字段收敛为精确 15 个。=> 既保留「多字段即失败」的等价于白名单护栏，又新增「不得出现被剔除字段」的约束，**未削弱**。
2. 用例 setup 仍注入 `ProfitControlEnabled / ProfitMinMargin / ProfitSafetyBuffer / ModelRouting(Enabled) / AccountCount`（`:334-341`）。这些字段只在 `AdminGroup` 上（`dto/types.go:191-237`），若误用 `GroupFromServiceAdmin` 会在 JSONEq 中多键失败；旧的注释约束（`:321-322`）仍然有护栏。
3. 新增 `available_groups_public_contract_test.go`（57 行，`//go:build unit`）**绑定生产 handler**：`newContractDeps` 在真实 Gin 路由上注册 `v1Keys.GET("/groups/available", apiKeyHandler.GetAvailableGroups)`（`api_contract_test.go:1517`），`doRequest` 走 `httptest` + `router.ServeHTTP`（`:1550-1565`）；被测代码是生产 handler + 生产 service + stub repository，不是重写逻辑。断言：
   - `require.Len(t, group, len(fields))`（:41）——**精确 15 键**（比规格 §5.2 的「⊇15」更严）；
   - 15 字段逐个 `require.Contains`（:42-44）；
   - `rpm_limit / max_reasoning_effort / image_price_1k / daily_limit_usd / claude_code_only / created_at / i18n` 逐个 `NotContains`（:45-47）；
   - 本地化 + 关键值（`allow_high_availability` / `allow_live` / `peak_*` / `audio_realtime_price_per_min`，:48-54）。
4. 覆盖盲点（非缺陷）：新测试的 15 字段清单在测试内硬编码（与 DTO 无共享来源），后续新增白名单字段必须同步改测试——这是有意护栏；`available_groups_public_contract_test.go` 未断言 3 个可选字段的取舍（因实现未保留，精确 15 键已隐含其缺席）。

### 2.5 前端消费面（A6）

| 核对项 | 结果 | 证据 |
|---|---|---|
| `UserFacingGroup` 拆分 | 已拆；`Group` 保留全量形状供 admin/内嵌响应用；`AdminGroup extends Group` 未动 | `frontend/src/types/index.ts:605-628`（UserFacingGroup，15 字段，`deepseek_pricing_state?` 可选）、`:630-696`（Group）、`:698+`（AdminGroup） |
| `api/groups.ts` 返回类型 | `Promise<UserFacingGroup[]>` + `apiClient.get<UserFacingGroup[]>` | `frontend/src/api/groups.ts:16-19` |
| KeysView ref 类型 | `ref<UserFacingGroup[]>([])` | `views/user/KeysView.vue:1399`；赋值 `:1674` |
| UsageView ref 类型 | `ref<UserFacingGroup[]>([])`；只用于分组筛选下拉（id/name） | `views/user/UsageView.vue:426`；`:435`；赋值 `:872-875` |
| 四个组件 props 收窄 | GroupChainEditor / KeyGroupChain / BulkEditKeysModal 用 `DisplayGroup = Omit<UserFacingGroup,'allow_high_availability'>` 保持对全量 Group 可赋值；KeyHAGroupPicker 用 `Pick<...> & { allow_high_availability?: boolean }`，缺省按 falsy 降级 | `components/keys/GroupChainEditor.vue:67-75,94-96`；`KeyGroupChain.vue:19-24,28`；`BulkEditKeysModal.vue:162-175`；`KeyHAGroupPicker.vue:35-39,49` |
| 组件实际读取字段 | 仅白名单内字段（id/name/platform/rate_multiplier/subscription_type/status/peak_*/allow_high_availability） | grep `group.` in `frontend/src/components/keys`（4 处命中 + HAGroupPicker:49，均白名单内）；`regex (is_exclusive|allow_image_generation|allow_batch_image_generation|daily_limit_usd|image_price|rpm_limit|claude_code_only|fallback_group_id|scheduling_strategy|created_at)` 命中 0 |
| 仍按全量 Group 读、但数据源是 getAvailable() 的漏改点 | **未发现**。`getAvailable()` 在 `frontend/src` 只有 2 个用户调用点（KeysView:1674、UsageView:872）；两名 ref 已改窄；`vue-tsc -b --force`（全量重建）退出 0 | 命令 #9/#10；grep `getAvailable(\(` |
| 其它 `is_exclusive` 读取面 | 均来自另一条响应（channels/modelPlaza/admin），未受 F-05 影响 | §2.1 证据 |

## 3. F-04：用户域 `/channel-monitor-v2/snapshot` 核对

### 3.1 用户域 config 严格键集；admin 完整不变（B1）

- 新增 `backend/internal/service/channel_monitor_v2_public.go`：`ChannelMonitorV2PublicConfig{RefreshIntervalSeconds int `json:"refresh_interval_seconds"`}`（`:6-9`，**唯一字段**）；`ChannelMonitorV2PublicSnapshot`（`:11-17`）的 `Config/Coverage/Metrics/Health/Trend` 与旧 `ChannelMonitorV2Snapshot`（`channel_monitor_v2.go:211-217`）同名同 tag；`SnapshotPublic`（`:19-29`）显式投影，只复制 refresh 数值。
- handler：`channel_monitor_v2_handler.go:110-118` 新增 `if !admin { SnapshotPublic(...) }` 分支；admin 分支 `:119-124` 仍走 `Snapshot(ctx, filter, true)`，与基线逐字相同。路由未改（`routes/user.go:168` → `h.Snapshot`；`routes/admin.go:894` → `h.AdminSnapshot`；`/admin/channel-monitor-v2/config` GET/PUT 在 `routes/admin.go:887-888` 未动）。
- 测试证据：`channel_monitor_v2_handler_test.go:38-86` 的 `TestChannelMonitorV2SnapshotConfigResponseContract`（真实 handler + 真实 service + stub repo）：
  - user：`require.Len(body.Data.Config, 1)` + `JSONEq 300` for `refresh_interval_seconds`（`:77-78`），并断言 `repo.admin==false`（:60）；
  - admin：config 含 `version/enabled/platforms/group_ids/health_thresholds/ignored_error_categories`（`:70-72`），`group_ids=[7]`、`ignored_error_categories=["timeout"]`（:73-74）。
  => 用户域 config **严格 == ["refresh_interval_seconds"]**；admin 快照仍为完整 config。命令 #6 退出 0。

### 3.2 其余 5 端点形状逐字不变（B2）

- `git diff origin/develop...HEAD -- backend/internal/handler/channel_monitor_v2_handler.go`：仅 `snapshot()` 一处新增分支；`Dimensions/Models/Matrix/Errors/Users` 的 handler 与 service 方法（`channel_monitor_v2.go:472-604`）未改；响应结构体（`ChannelMonitorV2Matrix/Dimensions/ModelRow/ErrorRow/UserRow`，`channel_monitor_v2.go:219-296`）未改。
- service 侧本 PR 仅删 `redactChannelMonitorV2PublicConfig` 与 `redactChannelMonitorV2Snapshot` 的 config 段（`channel_monitor_v2.go:606-614`），该函数只被 `Snapshot` 调用（`:497`），与其它 5 端点无关。
- 判定：**形状逐字不变**（静态核对；无新增测试护栏，见 §4 C-04）。

### 3.3 errors[].ignored 仍由服务端计算（B3）

- `repository/channel_monitor_v2_repo.go:697` `ignoredSet := service.ChannelMonitorV2IgnoredCategorySet(cfg)`；`:729-730` `_, ignored := ignoredSet[category]` → `ChannelMonitorV2ErrorRow{Ignored: ignored}`。`cfg` 来自 `service.getEnabledConfig` 的**完整** config（`ErrorsForViewer` `channel_monitor_v2.go:572-588`），不是响应里的 `config`；repo/service 均未改。
- `channel_monitor_v2.go:262-270` 字段注释仍成立（UI 置灰依赖该布尔）。前端 `api/channelMonitorV2.ts:201-202` 只读该布尔，不自行推导。
- 结论：删除响应里的 `config.ignored_error_categories` **不会**让错误分类置灰失效。

### 3.4 被删脱敏逻辑的残留检查（B4）

- `grep redactChannelMonitorV2PublicConfig` 全仓：**无残留调用点**（该符号只剩 PR 前的删除记录，不在当前源码）。
- `redactChannelMonitorV2Snapshot` 当前唯一调用点：`channel_monitor_v2.go:497`（`Snapshot`，仅在 `!admin` 时调用）。其函数体现在只做 metric 脱敏（`:606-614`），语义变化是「config 收敛移交用户面投影」，与规格 §4.3 一致。
- 现有非 admin 调用 `Snapshot(..., false)` 的路径只有 `channel_monitor_v2_public.go:20`（`SnapshotPublic`）；未发现其它会序列化完整 config 的用户面路径。风险提示见 §4 C-03。
- metric 脱敏保留：handler 测试 user 分支断言 `RequestCount==0`、`RPM==0`、`ErrorRate≈0.1`、`Trend[0].RequestCount==0`（`:79-82`）；service 测试 `:534-540` 同样断言。

### 3.5 前端兼容（B5）

- `MonitorPublicConfig{refresh_interval_seconds: number}`（`frontend/src/api/channelMonitorV2.ts:147-149`）；`MonitorSnapshot.config: MonitorPublicConfig`（`:151-157`）；admin `MonitorConfig` 原样保留（`:101-122`），`getConfig()` 返回 `MonitorConfig`（`:268-269`）。
- `ChannelStatusV2View.vue:819`：`snapshot.value?.config?.refresh_interval_seconds || 300`——实现用 `||` 而非 `??`，缺省/0 都回落 300，**兼容**；全视图无其它 config 字段读取（grep `config` 仅此 1 行）。
- 其余 5 端点的前端类型与函数（`getDimensions/getMatrix/getModels/getErrors/getUsers` `:244-267`）未改；`vue-tsc -b --force` 退出 0（命令 #10）。

## 4. 发现清单

> 严重级：Critical / Required（无前缀）/ Nit / Optional / FYI。本 PR **无 Critical、无 Required**。

| ID | 级别 | 位置 | 描述 | 证据 | 处置建议 |
|---|---|---|---|---|---|
| C-01 | **Nit** | `frontend/src/api/channelMonitorV2.ts:201` | `MonitorErrorRow.ignored` 的注释仍写「true when category is in config.ignored_error_categories」，但用户域 `config` 已不下发该键；易误导后来者以为客户端可自行推导或重新加回字段 | 注释原文；服务端计算见 `repository/channel_monitor_v2_repo.go:729-730`；用户快照 config 唯一键见 `channel_monitor_v2_public.go:6-9` | 注释改为「服务端按 ignoredSet 计算；客户端只读该布尔」 |
| C-02 | **Nit** | `backend/internal/service/channel_monitor_v2_test.go:511` | 测试名 `TestSnapshotRedactsPublicConfigPolicyFields` 与断言不符：现在测的是 `SnapshotPublic` 的 config 投影 + metric 脱敏，已不再断言「config 策略字段被 redact」 | 名称 `:511` vs 断言 `:534-540` | 重命名为 `TestSnapshotPublicProjectsConfigAndRedactsMetrics` |
| C-03 | **Optional** | `backend/internal/service/channel_monitor_v2.go:487-500,606-614` | `Snapshot(ctx,filter,false)` 现在返回**未收敛**的完整 config（config 收敛只在用户面投影做）；当前唯一调用方是 `SnapshotPublic`，无泄漏，但方法注释未提示该语义变化，后续若有人直接 `Snapshot(false)` 会下发 `group_ids/platforms.Models/ignored_error_categories/updated_by` | 非 admin `Snapshot` 调用点仅 `channel_monitor_v2_public.go:20`（grep `.Snapshot(\(` in `internal/service`）；`redactChannelMonitorV2Snapshot` 现只脱敏 metric（`:606-614`） | 在 `Snapshot` / `redactChannelMonitorV2Snapshot` 注释注明「config 白名单由用户面投影负责，非 admin 调用方须自行裁剪」；或把非 admin 路径收为包内，降低误用面 |
| C-04 | **Optional** | 规格验收（FIELD-INVENTORY §5.2 末行 / handoff §3.1.7） | 规格要求「`/matrix`/`/dimensions`/`/models`/`/errors`/`/users` 键集合与 §3.2 一致」的断言**未新增**。本 PR 未改这些路径（静态核对形状不变），风险低，但缺少自动化护栏 | `git diff` 仅 snapshot 分支 + redact helper；`routes/user.go:167-172`、handler/service 对应方法未改（§3.2） | 后续补一条 5 端点键集合契约测试；若本轮不补，请在 PR 说明中显式记录为验收例外（由 Lead 决定） |
| C-05 | **FYI** | `backend/internal/handler/api_key_handler.go:473-480`；`backend/internal/server/routes/user.go:85-86` | 用户面 `PUT/DELETE /api/v1/keys/:id/ha-groups/:groupID` 的成功响应仍用 `dto.GroupFromService(result.Group)` 返回**全量 50 字段**分组（含 `rpm_limit`、图片/视频价格、`fallback_group_id`、`require_* ` 等），`dto.APIKeyFromService` 也内嵌全量 group。属 F-05 声明范围外（FIELD-INVENTORY §2 只覆盖 `/groups/available`），为**既有行为**，本 PR 未引入 | `api_key_handler.go:476`（`dto.GroupFromService(...)`）、`:475`（APIKeyFromService）；路由 `user.go:85-86` | 不在本 PR 返工；建议记入后续安全收敛 backlog（同一类「用户响应不下发运营策略字段」） |

## 5. 建议变异（由 Lead 执行，评审者不改源码）

> 做法：改生产源码 → 跑指定测试确认**变红** → 恢复源码 → 复跑确认**变绿**。变异 1/4/5/6 是必做（分别对应 F-05 白名单、F-04 投影、F-04 config 类型、metric 脱敏）。

| # | 变异内容 | 预期变红的测试（断言） |
|---|---|---|
| M-01 | 把 `api_key_handler.go:498-504` 恢复为旧实现：`out := make([]dto.Group,0,len(groups))`；`out = append(out, *dto.GroupFromService(g).Localized(lang))` | `TestAPIContracts/GET_/api/v1/groups/available`（JSONEq 多出 35 键）；`TestAvailableGroupsPublicFieldsAndLocalization`（`Len(group)==15` → 50） |
| M-02 | 从 `UserFacingGroup` 删除 `AllowHighAvailability` 字段（或 mapper 中不赋值；删字段会同时改 JSON 键集） | `available_groups_public_contract_test.go` `require.Contains("allow_high_availability")`（:43）+ `Len`（:41）；`TestAPIContracts` JSONEq |
| M-03 | 把 `UserFacingGroup.Localized` 改为直接 `return g`（跳过 `i18n.Localize`） | `TestAvailableGroupsPublicFieldsAndLocalization/en-US,en;q=0.9` 的 name/description 断言红；`zh` 子用例仍绿（证明语言断言有约束力） |
| M-04 | 删除 handler `!admin` 分支（`channel_monitor_v2_handler.go:110-118`），退回统一 `Snapshot(ctx, filter, admin)` | `TestChannelMonitorV2SnapshotConfigResponseContract/user` 的 `Len(config,1)` 红（config 变 7+ 键） |
| M-05 | 给 `ChannelMonitorV2PublicConfig` 加回 `GroupIDs []int64 `json:"group_ids"` 并在 `SnapshotPublic` 赋值 | 同上 user 子用例 `Len(config,1)` 红 |
| M-06 | 把 `SnapshotPublic` 改为返回 `s.Snapshot(ctx,filter,true)` 的投影（即跳过 metric 脱敏） | user 子用例 `Zero(RequestCount)`/`Zero(RPM)` 红（`:79-80`）；service 测试 `:537,540` 红 |
| M-07（可选） | 把 `SnapshotPublic` 的 Config 赋值改成常量 0 | user 子用例 `JSONEq 300`（:78）红 |

## 6. 未覆盖项与边界

1. **未运行 integration 套件**（本机无 PostgreSQL/Redis）：F-04 的 `ignoredSet` 与 snapshot SQL 只做静态核对（repo 代码未改，风险低）。
2. **未运行 `go build ./cmd/server`**、前端 `vitest`、`golangci-lint`、`govulncheck`：Lead 侧已报后端 build 0、前端 typecheck/build 0；本评审只复跑 §1 列出的命令。前端我只跑 `vue-tsc -b`（含 `--force`），**未跑 vitest**。
3. **本机无 docker**，未执行真实 `docker compose` / 容器启停；FIELD-INVENTORY §5.4 的 5 项部署后实机抽查**未做**（需线上验证：分组下拉、分组链 Live/Realtime 徽标、DeepSeek 峰谷徽标、HA 候选、用量页筛选）。
4. **未验证第三方/旧客户端兼容性**：`/groups/available` 删除 35 个字段是**有意的破坏性 API 变更**，本仓库前端已同步；仓库外调用方（脚本/插件）不在验证范围，建议发布说明注明。
5. **未验证 entitlement/ent/wire 生成物**：本 PR 未改 schema/wire，无需重生成。
6. F-04 admin `/admin/channel-monitor-v2/config` 端点本身无新增测试（handler/service/struct 均未改，连带断言在 admin snapshot 用例中覆盖了 config 键集）。

## 7. 结论摘要（供 Lead 汇报）

- F-05：**Pass**；接线范围精确（1 hunk + 1 新 dto 文件），契约测试更强，前端 typecheck 强制全量重建通过。
- F-04：**Pass**；用户域 config 严格 1 键，admin 完整，其余端点未动，ignored 服务端计算未受影响。
- 发现：2 Nit、2 Optional、1 FYI，0 Critical、0 Required。
- 建议：执行 M-01/M-02/M-04/M-05/M-06 变异后合并；C-05 记 backlog，C-04 由 Lead 决定补测试或记录例外。
