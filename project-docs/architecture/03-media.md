# 03 · 图片与视频生成链路侦察

## 状态

| 项 | 值 |
|---|---|
| 状态 | 已完成（只读侦察，未修改任何源码） |
| 仓库 | `F:\sub2`，分支 `develop` |
| 侦察范围 | 图片生成（同步 / 异步 / 批量）、视频生成（Grok / Seedance）、协议适配、计费、存储、前端 |
| 产出文件 | `project-docs/architecture/03-media.md`（本文件，唯一写入） |
| 结论 | 图片生成是**三条独立链路**（同步 / 异步 Redis / 批量 PostgreSQL）；视频生成**确实存在**，但只有两种"原生透传"（xAI Grok、火山 Ark Seedance），且被硬编码为 Grok 平台专属，缺少统一的视频任务模型、存储与主动结算 |

## 证据来源

全部证据来自当前工作树（`develop`）源码，标注为 `文件:行号`：

- 路由：`backend/internal/server/routes/gateway.go`
- Handler：`backend/internal/handler/{openai_images,grok_media,seedance,image_task_handler,batch_image_handler}.go`
- Service：`backend/internal/service/{openai_images,grok_media,seedance,image_task,batch_image_*,billing_service,video_billing,image_billing_size,media_price_config,image_storage,openai_gateway_usage,admin_group,account,image_generation_intent}.go`
- 数据模型：`backend/ent/schema/batch_image_{job,item,event}.go`、`backend/ent/schema/group.go`
- 迁移：`backend/migrations/{158,170,171,172,217,220}_*.sql`
- 文档：`docs/{ASYNC_IMAGE_TASKS,BATCH_IMAGE_MVP,seedance-api}.md`
- 前端：`frontend/src/components/admin/account/AccountTestModal.vue`、`frontend/src/views/admin/GroupsView.vue`

## 未确认项

1. **运行时行为未实测**：本次为纯静态侦察，未启动 PostgreSQL/Redis/S3，也未跑 `go test`；所有"启用条件"结论均来自代码默认值与文档，未验证实际部署是否已开启。
2. **Grok 视频上游真实响应形状**：`service/grok_media.go:550-556` 的注释引用 docs.x.ai，仓库内无上游抓包样本；`video.duration`/`video.url` 字段名以代码为准，未与真实 xAI 响应比对。
3. **Seedance 的 `usage.completion_tokens` 口径**：代码在 `service/seedance.go:150` 读取该字段并按输出 token 计价，但未确认 Ark 是否对所有模型/时长都返回该字段；缺失时该次任务不计费（`handler/seedance.go:44-45` 在 `OutputTokens<=0` 时直接返回 nil）。
4. **Composite 分组的视频路由边界**：`routes/gateway.go:96`（生成）与 `:112/:128`（状态/内容）允许 Composite，但 `:141/:149`（edits/extensions）只允许 Grok 平台；这是有意还是遗漏，未在代码注释中说明。
5. **前端是否存在独立的用户侧生图/生视频工作台**：`grep playground` 在 `frontend/src` 无命中；仅确认了管理端账户测试与分组定价表单，未穷尽所有视图。
6. **`_reference/domestic-provider-kit` 的媒体能力**：core 目录只有 4 个 Go 文件（provider 身份 + 定价），`grep Endpoint|Capability|image|video` 无命中；参考包自身的 UI 侧存在 `MediaCapability = 'image'|'video'|'music'`（见 `MERGE-INDEX.md:3845`），但未纳入 core 移植范围。

---

## 1. 结论摘要

| 能力 | 是否存在 | 载体 | 任务持久化 | 计费口径 |
|---|---|---|---|---|
| 同步图片生成/编辑 | ✅ | `/v1/images/generations`、`/v1/images/edits` | 无（同步返回即计费） | 按张 × 尺寸档（1K/2K/4K） |
| 异步图片任务 | ✅（默认关，依赖对象存储） | `/v1/images/{generations,edits}/async`、`/v1/images/tasks/{id}` | Redis（24h TTL） | 复用同步路径，完成时计费 |
| 批量图片 | ✅（默认关，仅 Gemini） | `/v1/images/batches*` | PostgreSQL（`batch_image_*` 三表） | 按成功张数结算，支持 hold/结算幂等 |
| Grok 视频生成 | ✅（仅 Grok / Composite） | `/v1/videos*` | **无本地任务表**，仅 Redis 延迟计费快照 | 按秒 × 时长 × 数量 |
| Seedance 视频生成 | ✅（仅 OpenAI / Composite 分组 + Seedance 能力账号） | `/api/v3/contents/generations/tasks` | 同上，仅 Redis pending | 按上游 `usage.completion_tokens` |
| 统一视频抽象 / 视频流式 / 视频对象存储 | ❌ | — | — | — |

---

## 2. 图片生成链路

### 2.1 路由总表

来源：`backend/internal/server/routes/gateway.go`

| 方法 | 路径（含无前缀别名） | 处理器 | 行号 |
|---|---|---|---|
| POST | `/v1/images/generations` | `imagesHandler` → `OpenAIGateway.Images` / `GrokImages` | 253, 415 |
| POST | `/v1/images/edits` | 同上 | 254, 416 |
| POST | `/v1/images/generations/async` | `h.AsyncImage.Submit` | 255, 417 |
| POST | `/v1/images/edits/async` | `h.AsyncImage.Submit` | 256, 418 |
| GET | `/v1/images/tasks/:task_id` | `h.AsyncImage.Get` | 257, 419 |
| POST | `/v1/images/batches` | `h.BatchImage.Submit` | 258 |
| GET | `/v1/images/batches` | `h.BatchImage.List` | 259 |
| GET | `/v1/images/batches/models` | `h.BatchImage.Models` | 260 |
| GET | `/v1/images/batches/:id` | `h.BatchImage.Get` | 261 |
| GET | `/v1/images/batches/:id/items` | `h.BatchImage.Items` | 262 |
| GET | `/v1/images/batches/:id/items/:custom_id/content` | `h.BatchImage.ItemContent` | 263 |
| GET | `/v1/images/batches/:id/download` | `h.BatchImage.Download` | 264 |
| POST | `/v1/images/batches/:id/cancel` | `h.BatchImage.Cancel` | 265 |
| DELETE | `/v1/images/batches/:id` | `h.BatchImage.DeleteRecord` | 266 |
| DELETE | `/v1/images/batches/:id/outputs` | `h.BatchImage.DeleteOutputs` | 267 |

**平台准入（关键）**：`imagesHandler`（`gateway.go:76-91`）只对两个平台放行：

```go
case service.PlatformOpenAI: h.OpenAIGateway.Images(c)
case service.PlatformGrok:   h.OpenAIGateway.GrokImages(c)
default:                     // 404 "Images API is not supported for this platform"
```

即 **Anthropic / Gemini / Antigravity 平台分组无法直接调用 `/v1/images/*`**。Gemini 图像模型只能经由 **OpenAI 平台分组 + API-Key 账号**的兼容路径进入（见 2.2）。

### 2.2 同步图片：`openai_images`

| 关注点 | 位置 | 说明 |
|---|---|---|
| Handler 入口 | `handler/openai_images.go:23` `Images()` | 读 body → 解析 → 平台/分组校验 → 安全审核 → 并发槽 → 转发 |
| 分组开关 | `handler/openai_images.go:98-101` | `service.GroupAllowsImageGeneration(apiKey.Group)`，未开启返回 403 |
| 并发限流 | `handler/openai_images.go:106` `acquireImageGenerationSlot`；实现 `handler/image_concurrency_limiter.go:18,22` | 图片专属并发闸；视频路径无对应实现 |
| 请求模型 | `service/openai_images.go:75-105` `OpenAIImagesRequest` | 同时支持 JSON 与 multipart（`Multipart bool`），含 `mask`、`input_fidelity`、`partial_images` 等原生字段 |
| 解析 | `service/openai_images.go:192,247,330` | `ParseOpenAIImagesRequest` / JSON 分支 / multipart 分支 |
| 能力分类 | `service/openai_images.go:57-64,542-566` | 三档：`images-basic`（走 Responses 桥）、`images-native`（原生 Images API）、`images-apikey`（兼容供应商透传） |
| 模型判定 | `service/openai_images.go:478-528` | `gpt-image-*` 与 `grok-imagine*` 为原生生图模型；`gemini-*-image` 走 `images-apikey`（`validateCompatibleImagesModel`） |
| 转发分派 | `service/openai_images.go:599-618` | 按账号类型：APIKey → `forwardOpenAIImagesAPIKey`；OAuth/SetupToken → `forwardOpenAIImagesOAuth` |
| 流式图片 | `service/openai_images.go:985` `handleOpenAIImagesStreamingResponse` | 支持 SSE 增量图片（`partial_images`），并有 keepalive（`:1262,1269`） |
| 响应图片计数 | `service/openai_images.go:1276,1322,1334` | `extractOpenAIImagesBillableCountFromJSONBytes` / `collectOpenAIImagePointers` |
| 上游脱钩 | `service/openai_images.go:653-658` 注释 | 生图是长耗时且上游已计费：客户端断开不取消上游 |

**Gemini 图像输出计数**：`service/gemini_image_output_accounting.go:11,53,83,96,113` 用 context 计数器 `gemini_image_output_counter` 观察 Gemini 原生响应中的 `inlineData` 图片，供计费使用。

**生图意图识别**：`service/image_generation_intent.go:34-37` `GroupAllowsImageGeneration`；`:40` `IsImageGenerationIntent` 把"模型名像生图模型"或"body 带 image 工具"的 chat/responses 请求也判定为生图意图并施加分组开关。**视频没有任何对应的 intent 识别**（`grep VideoGenerationIntent|IsVideoGeneration|videoIntent` 在 `backend/` 零命中）。

### 2.3 异步图片任务（Redis）

| 关注点 | 位置 | 说明 |
|---|---|---|
| Handler | `handler/image_task_handler.go:51` `Submit`、`:164` `Get` | 提交返回 202 + `poll_url`；仅同 API Key 可轮询 |
| 服务 | `service/image_task.go:78` `ImageTaskService` | |
| 状态常量 | `service/image_task.go:17-24` | `processing` / `completed` / `failed`；TTL 24h；执行上限 30min |
| 存储记录 | `service/image_task.go:34-45` `ImageTaskRecord` | **Redis-only**，含 `user_id`/`api_key_id` 归属字段 |
| 存储接口 | `service/image_task.go:67-70` `ImageTaskStore`（`Save`/`Get`）；实现 `repository/image_task_store.go` | 无数据库表 |
| 启用条件 | 依赖对象存储：`docs/ASYNC_IMAGE_TASKS.md:21` 未配置时返回 404 `async image tasks are not enabled` | 未启用时不写 Redis |
| 执行方式 | `handler/image_task_handler.go:211,223,261` | 用 `httptest.ResponseRecorder` 复跑同步 Images 处理器，复用路由/计费/审核/并发全部逻辑 |
| 流式拒绝 | `handler/image_task_handler.go:280` `asyncImageRequestStreams` | 流式请求被拒（轮询只返回一个终态 JSON） |
| 结果转存 | `service/image_task.go:76,112,120` + `service/image_storage.go:60-63` `Rewrite` | 完成时把 `data[].url` 指向对象存储、剥离 `b64_json` |

### 2.4 批量图片（PostgreSQL + Redis Worker）

| 关注点 | 位置 | 说明 |
|---|---|---|
| Handler | `handler/batch_image_handler.go:31` `Submit`、`:244` `Download`、`:283` `DeleteOutputs` | 提交支持 `Idempotency-Key`（`:48`） |
| 公共视图 | `service/batch_image_public.go`（`:991,:1013` 校验 `AllowBatchImageGeneration`） | 不暴露 provider ref / GCS URI / 签名 URL |
| 提供方 | `service/batch_image_provider.go:40-52` | 仅两个：`gemini_api`（`batch_image_provider_gemini.go`）与 `vertex`（`batch_image_provider_vertex.go`） |
| Worker | `service/batch_image_worker.go:50,118,132`；运行时 `batch_image_worker_runtime.go:10,64` | Redis 就绪队列 + 延迟队列 + 陈旧 active 回收；**不做 DB 扫描轮询** |
| 队列 | `repository/batch_image_queue.go`；键名见 `docs/BATCH_IMAGE_MVP.md:139-157` | 就绪/延迟/active/inflight/lock/idempotency |
| 生命周期 | `docs/BATCH_IMAGE_MVP.md:108-137` | `created→uploading→submitted→running→indexing→settling→completed`，对外映射为 queued/running/processing_results/settling/completed |
| 分组开关 | `service/admin_group.go:525`、`:826-827` | `allowBatchImageGeneration = input && allowImageGeneration && platform == Gemini`；**只有 Gemini 分组可开批量生图** |
| 清理 | `service/batch_image_cleanup.go`；默认值 `docs/BATCH_IMAGE_MVP.md:176-182` | 输入 24h / 输出 72h / 上限 7 天 |

### 2.5 数据模型（Ent）

| 表 | Schema | 关键字段 |
|---|---|---|
| `batch_image_jobs` | `ent/schema/batch_image_job.go:19-72` | `batch_id`(唯一)、`user_id`、`api_key_id`、`account_id`、`provider`、`model`、`status`、`provider_job_name`、`provider_input_ref`/`provider_output_ref`、`gcs_input_uri`/`gcs_output_uri`、`item_count`/`success_count`/`fail_count`、`estimated_cost`/`hold_amount`/`actual_cost`(`decimal(20,10)`)、`hold_id`、`manifest_hash`(唯一)、`output_expires_at` |
| `batch_image_items` | `ent/schema/batch_image_item.go:15-53` | `(job_id,custom_id)` 唯一、`status`、`provider_source_object`、`source_line_number`/`byte_offset`/`byte_length`、`mime_type`、`image_count`、`billed_amount` |
| `batch_image_events` | `ent/schema/batch_image_event.go:15-43` | append-only 事件流，`(job_id,event_hash)` 唯一 |

> **注意**：这三张表只服务于**批量**图片。**异步图片任务与全部视频任务都没有 Ent 模型**——异步图片在 Redis，视频仅在 Redis 存计费快照。

---

## 3. 视频生成链路

**结论：视频生成是真实存在的功能，但实现方式是"两家上游原生协议各自透传"，不是一套抽象。** 共有两条链路，均不落数据库。

### 3.1 Grok（xAI）视频

| 关注点 | 位置 | 说明 |
|---|---|---|
| 路由 | `routes/gateway.go:270-281`（gateway 组）、`:420-431`（rootRoute） | 生成 `/videos`、`/videos/generations`；编辑 `/videos/edits`；扩展 `/videos/extensions`；状态 `GET /videos/:request_id`；内容 `GET /videos/:request_id/content` |
| 平台准入 | `routes/gateway.go:92-107` | 生成：`PlatformGrok` **或** `PlatformComposite`；否则 404 |
| 状态/内容准入 | `routes/gateway.go:108-139` | Grok 或 Composite |
| 编辑/扩展准入 | `routes/gateway.go:140-155` | **仅 `PlatformGrok`**（Composite 被排除，无注释说明） |
| Handler | `handler/grok_media.go:29-52` | `GrokVideoGeneration/Edit/Extension/Status/Content` |
| 统一入口 | `handler/grok_media.go:54` `handleGrokMedia` | 账号选择 → 能力校验 → 槽位 → 转发 → 计费 |
| 上游能力校验 | `handler/grok_media.go:337-356`；`service/account.go:1849-1887` `GrokMediaGenerationEligibility` | OAuth 账号需有 billing 快照；403/免费档被拒；支持 `grok_media_eligible` 人工覆盖 |
| 转发 | `service/grok_media.go:646` `ForwardGrokMedia` | 无请求体时也转发（状态查询）；模型按账号映射改写（`:685-695`） |
| 内容代理 | `service/grok_media.go:799` `forwardGrokMediaVideoContent`；URL 改写 `:1431` `rewriteGrokMediaVideoContentURLs` | 把上游视频 URL 改写成网关代理 URL（`:1529` `grokMediaContentProxyURL`），**不落盘、不转存对象存储** |
| 可计费判定 | `service/grok_media.go:557-572` | `status == "done"` 且 `video.url` 非空 |
| 计费元数据提取 | `service/grok_media.go:579-644` `ExtractGrokVideoBillingFromStatusBody` | `duration` 取 `video.duration`；`resolution` **状态响应没有该字段**，只能取创建时快照 |
| 请求体规范化 | `service/grok_media.go:958,1081,1157` | `prepare/normalize/sanitizeGrokMediaForwardBody`；图片引用字段规范化 `:1034,1109` |

**延迟计费机制（关键）**：

1. 创建时**不扣费**，只写 Redis 快照 `grok_video_pending:`（`handler/grok_media.go:489-517` → `service/grok_media.go:443` `StoreGrokVideoPendingBilling`，TTL 24h 见 `:431-435`）。
2. 快照字段：`GrokVideoPendingBilling`（`service/grok_media.go:377-389`）—— `model`/`billing_model`/`upstream_model`/`video_resolution`/`video_duration_seconds`/`original_model`/`created_at`。
3. 任务与账号绑定：`BindGrokMediaVideoRequestAccount`（`service/grok_media.go:297`，调用点 `handler/grok_media.go:480`），保证后续轮询/下载回到同一账号。
4. **只有在客户端轮询状态或下载内容、且观察到 `done + video.url` 时才结算**：`handler/grok_media.go:519-532` → `prepareGrokVideoCompletionBilling`（`:604`）。
5. 一次性认领防重复扣费：`ClaimGrokVideoBilling`（`service/grok_media.go:505`）写 `grok_video_billed:`（TTL 48h，`:437-440`）；结算失败释放认领 `ReleaseGrokVideoBilling`（`:522`，调用点 `handler/grok_media.go:777-784`）。
6. `duration_ms` 用"创建受理 → 首次发现完成"的端到端时长（`GrokVideoE2EDuration`，`service/grok_media.go:398`）。
7. 立即计费闸门 `shouldRecordGrokMediaUsage`（`handler/grok_media.go:586-602`）明确排除所有视频创建端点与查询端点。

> ⚠️ **未确认/风险**：因为结算是被动的，**只提交不轮询的视频任务永远不会结算**（同一模式在 Seedance 文档中被明确写出，见 `docs/seedance-api.md:41`）。代码里没有后台轮询器（对比批量图片有 Worker）。

### 3.2 Seedance（火山方舟 Ark）

| 关注点 | 位置 | 说明 |
|---|---|---|
| 路由 | `routes/gateway.go:367-371` | 前缀循环 `/api/v3`、`/v3`、`/v1`、`""` × `POST/GET/DELETE /contents/generations/tasks[/:task_id]` |
| Handler | `handler/seedance.go:15-39` `SeedanceTasks` | 强制 `application/json`；分组必须是 **OpenAI 或 Composite**（`:24-27`） |
| 端点常量 | `service/seedance.go:16-21` | `seedance_create` / `seedance_status` / `seedance_delete`；能力 `OpenAIEndpointCapabilitySeedance = "seedance"` |
| 任务键 | `service/seedance.go:28` `SeedanceTaskKey` | `"seedance:" + id`，与 Grok 视频任务键隔离 |
| 请求解析 | `service/seedance.go:30-55` `ParseSeedanceRequest` | 保留 Ark 原生 `content[]`（`text` / `image_url`），**不做 messages 转换** |
| 转发 | `service/seedance.go:75-155` `ForwardSeedance` | 仅改写 `model`（账号映射），其余字段原样透传；DELETE 语义透传 |
| URL 构造 | `service/seedance.go:57-71` `buildSeedanceURL` | 自动补 `/api/v3`；task ID 做路径段校验 |
| 不重试 | `service/seedance.go:132-136` 注释 | 异步创建的上游错误不自动重试（避免重复创建付费任务） |
| 计费 | `handler/seedance.go:43-63` `prepareSeedanceCompletionBilling` | 首次查询到 `succeeded` 且 `usage.completion_tokens > 0` 时按 **输出 token** 计费；显式注释"绝不按 Grok 每秒价折算" |
| 账号能力 | `service/account.go:1779-1782` | `seedance` 能力必须显式配置，且账号为 **OpenAI 平台 + API-Key 类型** |
| 文档 | `docs/seedance-api.md` | 要点见第 7.3 节 |

### 3.3 "有没有真正的视频生成？"

**有，且有两个可用供应商**，但与图片相比是"降级实现"：

- 是**真异步任务**：创建返回上游 task id，客户端轮询，服务端只做协议透传与账号/计费粘性。
- **不是**统一抽象：`GrokMediaEndpoint`（`service/grok_media.go:24-57`）把 Grok 与 Seedance 塞进同一个枚举，但分派处用 `if endpoint.IsSeedance()` 硬分支（`handler/grok_media.go:108,388,521,556`）。
- **没有本地任务表**：视频任务在 Sub2API 侧只存在于 Redis 的计费快照与账号绑定键中。
- **没有视频对象存储/转存**：对比图片有 `ImageStorage` 抽象 + S3 实现。

---

## 4. 上游协议差异与适配

| 维度 | OpenAI（原生图片） | Grok（xAI 媒体） | Seedance（Ark 视频） | Gemini（兼容图片） |
|---|---|---|---|---|
| 入站路径 | `/v1/images/{generations,edits}` | `/v1/images/*`、`/v1/videos/*` | `/api/v3/contents/generations/tasks` | `/v1/images/*`（模型名 `gemini-*-image`） |
| 入站格式 | JSON **或 multipart**（`service/openai_images.go:192,247,330`） | JSON 或 multipart（`service/grok_media.go:145,208`） | 仅 JSON（`handler/seedance.go:16-22`） | JSON（透传） |
| 请求体语义 | `prompt`+`size`+`n`+`mask` 等 | `prompt`+`image`（`normalizeGrokMediaJSONImageRefs`，`service/grok_media.go:1034`） | `content[]`（text/image_url） | `prompt`+`size` |
| 响应图片/视频 | `data[].b64_json` 或 `data[].url` | `data[]` 图片；视频为 `{status,video:{url,duration}}` | Ark 任务对象 + `usage.completion_tokens` | Gemini `inlineData` |
| 同步/异步 | 同步（可流式 `partial_images`） | 图片同步、视频异步 | 异步任务 | 同步 |
| 账号类型分支 | APIKey / OAuth / SetupToken（`service/openai_images.go:610-617`） | Grok OAuth 或 API Key（`service/grok_media.go:663,713`） | 仅 API-Key（`service/seedance.go:76-78,105`） | API-Key 透传（`images-apikey`） |
| 模型改写 | `rewriteOpenAIImagesModel`（`service/openai_images.go:874`） | `account.GetMappedModel`（`service/grok_media.go:686`） | `account.GetMappedModel`（`service/seedance.go:96`） | 同左 |
| 内容代理 | 直接回传 base64/URL | 视频 URL 改写为网关代理（`service/grok_media.go:1431`） | 原样回传上游响应 | 直接回传 |
| 能力枚举 | `images-basic/native/apikey`（`service/openai_images.go:57-64`） | `grok_media_generation`（`service/account.go:97-101`） | `seedance`（`service/seedance.go:20`） | 复用 `images-apikey` |

**Anthropic**：`/v1/images/*` 与 `/v1/videos/*` 均对 Anthropic 平台返回 404（`routes/gateway.go:82-90,100-106`）。本仓库中未找到任何 Anthropic 平台专属的图片/视频**生成**路由（`routes/gateway.go` 中 `imagesHandler` 只分派 [OI] 与 Grok）。

---

## 5. 不兼容点定位：视频相比图片缺什么

| # | 缺失环节 | 图片的现状 | 视频的现状 | 证据 |
|---|---|---|---|---|
| 1 | **统一协议转换层** | 三档能力分类 + JSON/multipart 双解析 + 模型判定 + 兼容供应商透传 | 只有两家原生协议透传，靠 `IsSeedance()` 硬分支；无统一视频请求/响应 schema | `service/openai_images.go:57-64,542-566` vs `handler/grok_media.go:108,388,521` |
| 2 | **流式** | SSE 增量图片 + keepalive | 完全没有；视频为异步任务，无流式概念 | `service/openai_images.go:985,1262`；视频侧无对应实现 |
| 3 | **异步任务持久化** | 异步图片 Redis（24h）+ 批量图片 PostgreSQL 三表 | 无任务表；仅 Redis 计费快照（pending 24h / billed 48h） | `service/image_task.go:34-70`、`ent/schema/batch_image_*.go` vs `service/grok_media.go:431-440` |
| 4 | **后台驱动/轮询** | 批量图片有 Worker（就绪/延迟/恢复循环）+ 计费恢复 | 无后台轮询；结算是被动的，不轮询就永不结算 | `service/batch_image_worker.go:118,233,256`、`batch_image_worker_runtime.go:103` vs `docs/seedance-api.md:41` |
| 5 | **对象存储/转存** | `ImageStorage` 接口 + S3 实现 + 32MiB 下载上限 + 结果改写为短链 | 无；视频内容只做 URL 改写（仍然回源上游） | `service/image_storage.go:17-28,60-63` vs `service/grok_media.go:1431,1529` |
| 6 | **计费触发时机** | 同步返回即计费；批量按成功张数结算（幂等 + hold） | 延迟到"客户端观察到 done"；无主动兜底；有 pending 丢失→fail-closed 的注释 | `service/billing_service.go:2242`、`docs/BATCH_IMAGE_MVP.md:159-170` vs `handler/grok_media.go:489-491,627-630` |
| 7 | **定价维度** | 尺寸档 1K/2K/4K × 张数（`ImagePriceConfig`） | 分辨率档 480p/720p/1080p × 秒数 × 数量，且分辨率**只能来自创建时快照** | `service/billing_service.go:2091-2095,2242-2265` vs `:2098-2105,2274-2294`、`service/grok_media.go:611-612` |
| 8 | **平台白名单** | OpenAI + Grok（Gemini 经 OpenAI 分组的兼容路径） | **Grok/xAI-only**；迁移 220 明确清空非 Grok 分组的视频价（保留 composite） | `routes/gateway.go:76-91`；`backend/migrations/220_clear_non_grok_video_generation_config.sql:1-2,31-41` |
| 9 | **分组开关** | `allow_image_generation`（Grok 默认 true）+ `allow_batch_image_generation`（仅 Gemini） | 复用 `allow_image_generation`，**没有独立的 `allow_video_generation`** | `ent/schema/group.go:101,104`；`service/admin_group.go:314-318,525`；`migrations/220:2-3` 注释确认 |
| 10 | **意图识别/准入** | `IsImageGenerationIntent` 覆盖 chat/responses 内嵌生图工具 | 零命中，无视频意图识别 | `service/image_generation_intent.go:40`；`grep VideoGenerationIntent` 无结果 |
| 11 | **并发控制** | 图片专属限流器 | 复用 Responses 账号槽位，无视频专属闸 | `handler/image_concurrency_limiter.go:18` vs `handler/grok_media.go:369` |
| 12 | **计费元数据约束** | `usage_logs` 有 `image_size` 校验 | 迁移 171/172 用 `billing_mode='video'` **或** `video_count>0` 豁免 `image_size` 必填；新增视频供应商若走别的 `billing_mode` 会踩约束 | `migrations/171_allow_video_usage_without_image_size.sql:3,12`、`172_video_per_second_billing_metadata.sql:10-12,25-26` |
| 13 | **前端** | 账户测试生图模式 + 批量生图相关文案 | 仅账户测试的 video 模式（轮询至完成并预览）与分组视频定价表单；**无用户侧视频工作台/任务列表** | `frontend/src/components/admin/account/AccountTestModal.vue:267-278,462-466,1009-1015`、`frontend/src/views/admin/GroupsView.vue:1046-1072`；`grep playground` 无命中 |
| 14 | **国模 kit 覆盖** | — | 参考包 core 只有 provider 身份与定价（4 个 Go 文件，无媒体能力） | `_reference/domestic-provider-kit/core/backend/internal/service/domestic_provider.go:8,21,34,40,54`；`grep Endpoint|Capability|image|video` 无命中 |

---

## 6. 计费实现

### 6.1 图片

| 项 | 位置 | 说明 |
|---|---|---|
| 配置结构 | `service/billing_service.go:2091-2095` `ImagePriceConfig` | `Price1K/2K/4K *float64`，nil = 用默认 |
| 计算 | `service/billing_service.go:2242` `CalculateImageCost` | `单价 × 张数 × 倍率`；`BillingMode = image` |
| 单价解析 | `service/billing_service.go:2297-2318` `getImageUnitPrice` | 分组价优先 → LiteLLM `OutputCostPerImage` → 硬编码 `0.134`（`:2108,2361-2364`） |
| 尺寸档归一 | `service/image_billing_size.go:28-62` `ClassifyImageBillingTier` | `1k/2k/4k` 或像素（maxEdge ≤1024/≤2048/else）；默认 2K（`:64-69`） |
| 档位来源优先级 | `service/image_billing_size.go:71-113` `ResolveImageBillingSize` | 输出尺寸 > 输入尺寸 > 默认 2K，并记录 `Source` |
| Grok 图片默认价 | `service/billing_service.go:2389-2413` | `grok-imagine-image-2.0`=0.06/0.08；`grok-imagine-image-quality`=0.05/0.07；`grok-imagine`=0.02/0.02（1K/2K） |
| 批量图片结算 | `docs/BATCH_IMAGE_MVP.md:159-170` | 仅成功图片计费；结算幂等；有 hold 与有界重试；实现见 `service/batch_image_settlement.go`、`batch_image_billing_hold.go`、`batch_image_billing_recovery.go` |
| 分组字段 | `ent/schema/group.go:114,118,122` | `image_price_1k/2k/4k`；倍率 `image_rate_multiplier`/`image_rate_independent` |

### 6.2 视频

| 项 | 位置 | 说明 |
|---|---|---|
| 配置结构 | `service/billing_service.go:2097-2105` `VideoPriceConfig` | `Price480P/720P/1080P` **每秒单价 USD/s** + `ModelPrices`（族×分辨率覆盖） |
| 计算 | `service/billing_service.go:2274` `CalculateVideoCost` | `每秒价 × 时长(秒) × 数量 × 倍率`；`BillingMode = video` |
| 单价优先级 | `service/billing_service.go:2320-2343` `getVideoUnitPrice` | per-model map → 分组扁平 `video_price_*` → 代码默认 |
| Grok 默认每秒价 | `service/billing_service.go:2118-2122,2426-2451` | `grok-imagine-video`：480p=0.05 / 720p,1080p=0.07；`grok-imagine-video-1.5`：480p=0.08 / 720p=0.14 / 1080p=0.25 |
| 兜底 | `service/billing_service.go:2377-2387` `getDefaultVideoPrice` | 非 Grok 模型回落 2K 图片默认价（注释标注为安全网） |
| 模型族归一 | `service/video_billing.go:12-53` `CanonicalGrokImagineVideoPriceFamily` | 别名/前缀/`xai/``grok/` 前缀 → `grok-imagine-video` 或 `...-1.5` |
| map 清洗 | `service/video_billing.go:63-131` `NormalizeVideoModelPrices` | 排序遍历避免 map 顺序抖动；未知分辨率丢弃并告警；冲突档位告警 |
| 查价 | `service/video_billing.go:134-155` `LookupVideoModelPrice` | 未命中回退扁平列 |
| 计费入口（网关） | `service/openai_gateway_usage.go:810-870` | 渠道 video 定价 → 分组视频价 → 回源刷新 → 渠道按次/按秒 → `CalculateVideoCost` |
| 分组字段 | `ent/schema/group.go:141,145,149`；`migrations/170,172,217` | `video_price_480p/720p/1080p`（USD/s）、`video_rate_independent`/`video_rate_multiplier`、`video_model_prices` JSONB |
| 用量列 | `migrations/172_video_per_second_billing_metadata.sql:10-12` | `usage_logs.video_count` / `video_resolution` / `video_duration_seconds` |
| Seedance | `handler/seedance.go:43-63`；`service/seedance.go:146-152` | **按输出 token**（`usage.completion_tokens`），不使用 Grok 按秒价；`OutputTokens<=0` 不计费 |
| 快照回源兜底 | `service/openai_gateway_usage.go:872-925` | 分组对象媒体价字段疑似缺失时回源查库一次 |

---

## 7. 相关文档要点摘要

### 7.1 `docs/ASYNC_IMAGE_TASKS.md`（166 行）

- 端点：`POST /v1/images/{generations,edits}/async`、`GET /v1/images/tasks/{task_id}`（`:10-15`）。
- **默认关闭**且以对象存储为门禁；未配置时返回 404，不建任务、不写 Redis（`:21`、`:58-72`）。
- 存储配置可从管理端（Admin → Backup → Async image object storage，复用备份 S3 配置）或 `config.yaml` 的 `image_storage` 块（`:23-52`）。
- 完成时把图片转存对象存储，响应改写成 `data[].url` 短链并剥离 `b64_json`；上传失败则任务标 failed（`:54`）。
- 任务 TTL 24h（每次状态更新重置），单任务执行上限 30 分钟（`:164`）；归属为 user + api_key 双维度，越权返回 404（`:166`）。
- 关闭开关只停新提交，已接收任务仍可轮询（`:31`）。

### 7.2 `docs/BATCH_IMAGE_MVP.md`（358 行）

- 提供方仅 `gemini_api` 与 `vertex`（`:5-8`）；公开面不暴露 Gemini 文件名、Vertex job 名、GCS 路径、签名 URL、API key、SA 材料（`:10`）。
- 限制：每 item 参考图 ≤3（Flash）/≤14（Pro）；每 job 参考图 ≤1000、内联参考数据 ≤128MB、输出图 ≤200、每 item `output_count` ≤4（`:56-67`）。
- 生命周期与对外状态映射见 `:108-137`；`completed → output_deleted` 由手动删除或 TTL 清理触发。
- Redis 仅用于唤醒/重试/协调/锁/下载限流，PostgreSQL 是唯一事实源；`batch_image.queue_enabled` 默认 false（`:139-157`）。
- 计费：提交可预估、索引后结算、仅成功图片计费、结算幂等（`batch_image_settlement:{batch_id}`）、有界重试后失败并释放 hold（`:159-172`）。
- 清理默认：输入 24h / 输出 72h / 上限 7 天 / 间隔 30 分钟（`:176-182`）。
- 配置键全量清单在 `:249-300`，功能开关默认全关（`:302`）。
- 未来优化项明确写了"对象存储下载卸载"仍待做（`:319-321`）。

### 7.3 `docs/seedance-api.md`（45 行）

- 目标：支持火山方舟 Ark 异步视频任务协议，**不把 `content[]` 转成 OpenAI `messages` 或 Grok `prompt`**（`:3`）。
- 配置：OpenAI 平台 **API Key** 账号 + Ark Base URL + 账号端点能力勾选 **Seedance (Ark)**（默认不启用）+ 分组允许图片生成 + 模型映射 + 输出 token 定价；**本接口不使用 Grok 按秒视频价**（`:7-10`）。
- 调用示例与 `/api/v3``/v3``/v1` 及无前缀别名（`:14-35`）；文本/图片/视频/音频内容与扩展参数原样传递，仅改写模型名。
- 任务与计费（`:39-43`）：查询/删除限同用户+同 Key+同分组，且始终用原提交账号；创建不扣 token，首次查询到 `succeeded` 后按 `usage.completion_tokens` 计费，重复查询有共享缓存声明 + 持久化用量去重；Redis 保存绑定与模型快照默认 24h；**当前不会后台轮询，只使用回调而不查询的任务不会自动结算**；不开放上游任务列表接口；删除不自动退款；异步创建错误不重试。

---

## 8. 对国模移植的影响（结论）

1. **视频链路不在可复用范围内**：迁移 220 把视频价限定为 Grok/composite，视频路由只认 Grok 平台；任何国模视频供应商（如可灵/即梦/Vidu）都需要**新增**平台能力枚举、路由准入、任务模型与计费分支，不能靠现有抽象复用。
2. **图片链路的可复用面**：`ImageStorage` 抽象（`service/image_storage.go:24-28`）与异步图片任务（`service/image_task.go`）是与供应商无关的；批量图片的 `BatchImageProvider` 接口（`service/batch_image_provider.go:15-25`）也可扩展第三个 provider，但当前分组开关把平台写死为 Gemini（`service/admin_group.go:525,826-827`），接入国模需改这条判定。
3. **计费侧最稳的接入点**：`VideoPriceConfig`/`ImagePriceConfig` + `CalculateVideoCost`/`CalculateImageCost` 与供应商无关，国模可按"分组价 + per-model map"接入；但需注意 `getDefaultVideoPrice` 的兜底会把未知视频模型折算成 2K 图片价（`service/billing_service.go:2377-2387`），与项目红线"缺价不得放行"冲突，接国模视频时必须显式定价或补拒绝逻辑。
4. **参考包无媒体能力**：`_reference/domestic-provider-kit/core` 只提供 provider 身份与定价（4 个 Go 文件），媒体链路需要独立设计，不能从参考包摘录。
