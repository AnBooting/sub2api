# 全量安全/质量审计结果（2026-10-19）

> 覆盖：Agent Team 对抗审查、OCR 可用批次、Lead 对关键路径的源码复核。
> 未覆盖项与产品决策项已明确标出，不作为已修复结论。

## 严重级汇总

| 严重级 | 数量 | 状态 |
|---|---|---|
| Critical | 1 | 待产品决策（Live 零计费） |
| High | 1 | 已记录，待后端存储/CSP 修复（SVG/iframe/plugin bridge 相关） |
| Medium | 10+ | 部分已修，部分待决策/待实现 |
| Low | 若干 | 已修或归档 |

## 已修复

1. DingTalk OAuth/API：nil staff 不再 panic；响应体读取错误不再被吞。
2. Admin 统计桩：不再返回伪造全零统计。
3. Antigravity：`LoadCodeAssist`/`OnboardUser` 增加 2 MiB 响应上限。
4. 仓库工具：错误传播、manifest 校验、stdin 挂起、机器特定路径。
5. 前端：`order_not_paid` 文案映射；`KeyUsageView.iconSvg` 消毒。

## Critical：OpenAI Live 零计费（待产品确认）

- 入口：`POST /v1/live`、`/backend-api/codex/realtime/calls`（`handler/openai_live.go`）。
- 证据：`service/openai_live.go:827-853` 最终 usage 的 `TotalCost/ActualCost=0`，绕过计费管道。
- 影响：余额用户可重复使用最长 1h 的 Live 会话且不扣费。
- 建议：产品确认 Live 是否计费；计费则冻结 quote 并接入现有 usage/billing，免费则移除 BillingType 误导。

## High：前端报告的高危项（待实施）

- `AppSidebar.vue` 管理端 SVG 经 DOMPurify 渲染；建议在存储边界做结构校验/白名单。
- `CustomPageView.vue` 自定义外部页把 JWT 放入 iframe URL；建议服务端 allowlist + 非认证 token/sandbox。
- `PluginsView.vue` 插件 UI 使用 fragment bridge token 且 `script-src unsafe-inline`；建议会话绑定 token 与更严 CSP。

## Medium/Low 记录

- 第三方 IP 地理查询默认外发 IP：需改为后端代理/默认关闭。
- localStorage 存 JWT/refresh token：建议 HttpOnly cookie 或显式 fallback 配置。
- `home_content` 存储未做内容白名单：建议 Markdown 白名单/存储边界校验。
- `KeyUsageView.fetchUsage` 应校验 Content-Type 和 gateway base URL 配置。
- 多个大文件（`UseKeyModal.vue` 2085 行等）需要拆分，本轮未重构。

## OCR 批次记录

- 成功：B3、B1c、B9。
- 失败：其余批次因远端 LLM 全量失败未产出；不列为审查完成。
- 原始 JSON 在 `F:\sub2--audit\full-20261019\ocr\`。

## 验证状态

- `go build ./...` 通过；前端 typecheck/build 与完整 Vitest（379/2993）通过。
- 后端全量 unit：除上游基线 `TestCalculateCostUnified_ChannelTimePricingDoesNotApplyOutsideMatchingTime` 外通过。
- OCR 仅完成 B3/B1c/B9，其余批次远端失败。

## 未验证边界

- PostgreSQL/Redis integration、真实上游调用、Docker E2E、生产部署。
- 管理统计真实聚合、插件桥 token 改造、外部 iframe 安全模型、Live 计费模型。
- OCR 未覆盖的后端 cmd/config/domain/server/admin/dto/yunmao 与前端 api/types/utils/components/views。


## 追加记录（新 OCR Key 轮次，2026-10-19）

### 成功批次

- B7：`frontend/src/api|types|utils`，112 文件 / 5 条意见。
- B8：`frontend/src/components|views`，342 文件 / 6 条意见。
- B1c/B9：上一轮已成功。
- C1：`backend/internal/config`，3 文件 / 3 条意见。
- C3：`backend/internal/handler/admin`，71 文件 / 5 条意见。
- C4：`backend/internal/handler/dto`，8 文件 / 3 条意见。
- C6：`backend/cmd`，7 文件 / 6 条意见。
- C7：`frontend/src/i18n`，37 文件 / 3 条意见；其中多数输出受工具过程文本污染，仅采用明确代码项。
- B4c：`backend/internal/service` 拆批中产出 5 条意见（admin_compliance）。

### 本轮已修复

1. `credentials_redact.go` 等 CVEs 尚未实施；本轮直接修复：
- `admin_compliance.go`：损坏 ack 返回错误、非正 admin id 拒绝、IP/UA 限长、AcceptedAt 非空校验。
- `config/artex_guard.go`：未知模式回退 enforce。
- `config/validate_dingtalk.go`：非法 corp_restriction_policy 拒绝。
- `cmd/jwtgen`：空用户 nil 检查。
- `handler/admin/account_data.go`：代理 expiry_warn_days 负值校验。
- 前端 admin API：`field/value` any 收紧、`proxyId=0` 修复。

### 已复核为设计契约/不修

- 前端账户复制后重复操作的 `idempotency-key` 不在 finally 清理：现有测试明确要求网络失败后复用同一 key，OCR 建议会破坏契约。

### 待产品决策/后续

- `account_data.go` 同名前代理覆盖、`backupProxyName` 缺失级联：需导入主体修复。
- `cleanup-ingress-reject-logs` 删除谓词与未来时间保护：未执行修复。
- `jwtgen` 输出到 stdout：工具明确需要打印，改为文件会破坏 CLI 契约，记录为操作风险。
- `config`/`server`/yunmao 等仍有 OCR 批次有限、失败或超时；不作为“全量已覆盖”结论。

## 追加记录（StepFun step-5-preview OCR 轮次，2026-10-19）

### 结果

- `stepfun` provider：`https://api.stepfun.com/step_plan/v1`，`step-5-preview`，`llm test` 成功。
- 只有去除 `--max-tokens-budget` 的单文件扫描能稳定产出真实结果；带预算的多文件/单文件扫描大量以 0 token 空跑或失败。
- 成功批次：`auth_service.go`（3 comments）、`gateway_service.go`（2 comments）、`account.go`（6 comments）、`api_key_handler.go`（4 comments）。
- 其余 server/routes、server/middleware、yunmao、schema、前端视图单文件，或失败或未产出；不视为全量覆盖。

### 本轮已修复

1. `auth_service.go`：登录回填 email identity 为 nil 时不再 panic。
2. `auth_service.go`：邀请码日志不再打印明文，改为 `code_sha256` 短哈希。
3. `account.go`：`GetCustomErrorCodes` 支持 `json.Number/string/int/int64/float64`，修复 DB 加载路径因类型不匹配导致 fail-open。
4. `gateway_service.go`：调试日志敏感 header 脱敏集合扩展（`x-goog-api-key`、`cookie`、`proxy-authorization` 等）。
5. `api_key_handler.go`：API Key 搜索截断改为 rune-safe；非法 `group_id` 返回 400 而非静默扩大结果。

### 待处理

- Auth refresh token 复用：检测后仅日志，未撤销 family；需要新增 consumed hash tombstones，记录为后续轮。
- `GetCustomErrorCodes` 等低危类型/性能建议已记录。
- StepFun 扫描的剩余文件因 provider 会话不稳定未完成；需要另选模型或进一步重试。

## 追加记录（StepFun 中间件串行轮次，2026-10-19 晚）

### 扫描方式

- 严格单文件串行：`--timeout 240 --concurrency 1 --no-plan --no-summary --audience agent --format json`，不带 `--max-tokens-budget`。
- 每文件一次主扫描，失败/卡死后最多重试一次；仍失败则记录为“未覆盖”，不反复消耗额度。

### 本轮已修复（均有测试，均在本分支提交）

| 文件 | 问题 | 提交 |
| --- | --- | --- |
| `middleware/api_key_auth_google.go` | Gemini/Antigravity 鉴权未注入 `ctxkey.UserID`，导致 `gateway_profit_control` 跳过用户专属分组倍率（计费口径错误） | `a1a5e7815` |
| `middleware/admin_auth.go` | `auth_method` 用字符串字面量，`step_up` 用常量比较；常量改名会让 step-up 门禁静默 fail-open | `3a1ca7571` |
| `middleware/security_headers.go` | nonce 检测扫描整条策略，任一指令含 `'nonce-` 就不注入 `script-src` 占位符，每请求 nonce 永不生效 | `456ed3cee` |
| `middleware/cors.go` | 包级 `sync.Once` 让后续配置的安全告警被静默；origin 匹配大小写敏感，配置含大写时全部跨域请求被拒 | `8aab5a455` |
| `middleware/request_body_limit.go` | `Body == nil` 时 `MaxBytesReader` 首次读会 panic，且使下游 nil 守卫失效 | `dad1dbd29` |
| `middleware/panel_rate_limit.go` | 反代未配 `trusted_proxies` 时公开接口限流整体静默失效，无任何信号 | `ff47e1cd4` |
| `middleware/recovery.go` | broken-pipe 用错误文本匹配，Windows `ECONNRESET` 漏判；改为 `errors.Is` 哨兵 | `4397db451` |
| `middleware/audit_log.go` | 审计路由表条目 `data-management/s3/config` 无对应路由，S3 配置读取永不审计；改为真实 `s3/profiles` | `8606ce19a` |
| `middleware/logger.go` | 访问日志在 `c.Next()` 之后，panic 请求不产生访问日志，错误率/延迟统计漏计 | `8a463770e` |
| `middleware/backend_mode_guard.go` | 角色比较用字面量 `"admin"` 而非 `service.RoleAdmin` | `a3a3b4fd0` |
| `middleware/client_request_id.go` | 复用已有 ID 的分支不写日志字段，且另一分支追加重复 key | `098448f34` |

### 已复核为误报/不修

- `middleware/api_key_auth.go` 的 nil-user 分支缺少 `MarkIngressRejected`：`snapshotFromAPIKey` 在 `User == nil` 时返回 nil，`loadAuthCacheEntry` 转成 `ErrAPIKeyNotFound`，该分支不可达。已回滚所有推测性改动。
- `middleware/admin_only.go` 的 `AdminOnly()` 全仓库无引用（死代码），但来源为上游 2025-12-26 提交 `e5a77853b`，非本 diff 引入；按项目规则不删除导出 API，仅记录。
- `middleware/request_metadata.go` 的 `maxBytes <= 0` 表示不设上限：属契约表述问题，仅补注释，不改行为。

### 本轮未覆盖（provider 侧失败，不代表无问题）

- `middleware/group_model_allowlist.go`：两次扫描均卡死无输出。
- `middleware/server_timing.go`：一次扫描卡死 16 分钟无输出。
- `middleware/artex_guard.go`：两次失败，OCR 给出非法行号范围（`start_line 900 > end_line 130`）。
- 其余 middleware（`admin_compliance`、`request_logger`、`ingress_reject` 主体、`step_up` 等）已在更早轮次覆盖，结果见上文批次记录。

### 尚未扫描（后续轮次）

- `backend/internal/server/{http.go,router.go}`、`routes/*.go` 剩余文件。
- `backend/internal/handler/yunmao/{dto,shop_handler}.go`。
- `backend/ent/schema/*.go`、`backend/migrations/*.sql`。
- `backend/internal/service` 剩余文件、前端 `api/admin`、`types/utils`、`deploy/tools`、`project-docs`。

### 第二轮追加修复（同日晚，路由/handler/service 串行扫描）

| 文件 | 问题 | 提交 |
| --- | --- | --- |
| `middleware/admin_compliance.go` | 合规门禁在 `settingService == nil` 时直接放行（fail-open），而 service 层对 nil 返回 `Required: true` 本可正确阻断；已改为 fail-closed 并加变异验证 | `4f792992e` |
| `server/router.go` | `refreshFrameOrigins` 失败路径完全静默，启动期 DB 不可用会让已配置 iframe 静默失效；补 warn 日志 | `5f45093e8` |
| `service/websearch_config.go` | 读配置失败直接 `SetWebSearchManager(nil)` 且无日志，websearch 模拟静默禁用且无重试 | `393a62a32` |
| `handler/yunmao/shop_handler.go` | 4 个接口缺少文档承诺的 nil service 守卫（会 panic 而非 503）；`Notify` 把所有回调失败统一返回 401，未知订单/上游不可用被误判为鉴权失败导致上游无限重试；已按错误码分类（签名类仍 401，保持 PRD-03 AC-B1 红线） | `f85680a23` |

### 已复核为不修

- `routes/admin.go`：`panelRateLimiter` 为 nil 时管理面限流静默失效——现有测试大量以 `nil` 传入 `RegisterAdminRoutes`，改为 panic 会破坏测试契约，记录为后续项。
- `routes/admin.go`：`pricing/settings` 内联 step-up 中间件按字段“出现”而非“变更”触发 TOTP——属产品语义取舍，记录不修。
- `server/router.go`：`frame-src` 动态回调与 settings 保存路径的两次性能开销——属优化项，记录不修。
- `middleware/admin_only.go`：`AdminOnly()` 无引用，但为上游 2025-12-26 既有代码，非本 diff 引入，不删除导出 API。

### 未覆盖（provider 侧失败）

- `middleware/{group_model_allowlist,server_timing,artex_guard}.go`：多次卡死或 OCR 返回非法行号范围。
- `server/http.go` 首轮失败、重试成功（3 条已记录：websearch 启用判定按未过滤配置、websearch 启动期静默禁用、全局 body 上限压过插件上传上限）。

## 追加记录（sensenova/glm-5.2 补扫 + 未决项实施，2026-10-19 深夜）

### 覆盖补扫：换 provider 解决了"扫不动"

stepfun 反复卡死的文件，改用 `--provider sensenova --model glm-5.2` 全部成功产出：

| 文件 | 结果 |
| --- | --- |
| `middleware/group_model_allowlist.go` | 1 low（body 读取错误固定用 OpenAI 格式，未按入口协议区分） |
| `middleware/server_timing.go` | 1 low → **已修复**（路径分隔符误判） |
| `middleware/artex_guard.go` | **1 high → 已修复**（压缩请求体绕过拦截） |
| `routes/gateway.go` | **1 high → 已修复**（typed-nil APIKey panic）+ 2 medium 记录 |
| `service/yunmao/service.go` | **2 high → 已修复**（结算错误丢弃致误报 NotFound、nil adapter panic）+ 2 记录 |
| `handler/admin/account_data.go` | 1 medium → **已修复**（错误响应泄露代理明文凭据）+ 4 记录 |

### 本轮新增修复

| 文件 | 问题 | 提交 |
| --- | --- | --- |
| `middleware/server_timing.go` | `/api/v1keys` 被拼成 `/keys` 误判命中，非 `/api/v1/` 路径也可能拿到 Server-Timing | `358d6b06f` |
| `middleware/artex_guard.go` + `pkg/httputil/body.go` | **High**：客户端用 `Content-Encoding: gzip/zstd/deflate` 压缩请求体时，守卫对压缩字节做 `json.Unmarshal` 必然失败，ARTEX 拦截被完全绕过；改为检测前先解码（下游仍收原始字节） | `95c55660f` |
| `routes/gateway.go` | `getGroupPlatform` 缺 typed-nil 判断，`apiKey.Group` 会空指针 panic | `68cf9cb3b` |
| `service/yunmao/service.go` | 结算失败时 `record` 被覆盖为 nil，已落库订单误报 `ErrOrderNotFound`；`settleFromUpstream` 未判 nil adapter 导致 panic | `6708cf439` |
| `handler/admin/account_data.go` | 导入错误响应里 `proxy_key` 含代理明文用户名/密码；改为脱敏指纹（导出与内部匹配仍用完整 key） | `3d694bf66` |
| `deploy/docker-compose.standalone.yml` | **High**：四个 compose 里唯一漏透传 `TOTP_ENCRYPTION_KEY`，standalone 部署重启后 2FA 用户无法登录、S3 凭据拒绝保存 | `e9d043d2e` |

### 未决问题实施（按推荐项）

| 问题 | 实施 | 提交 |
| --- | --- | --- |
| Q3+Q9 | 存储边界 SVG/HTML 白名单校验（`setting_content_boundary.go`，17 个边界测试，变异验证通过） | `a93088739` |
| Q6 | 代理导入同名不同 key 直接拒绝，不静默覆盖 | `1a275e8a9` |
| Q7 | 清理脚本加 `created_at <= now()` + 保留窗口双条件、dry-run 计数（实际位置在 `cmd/cleanup-ingress-reject-logs/main.go`，非 SQL） | `1a275e8a9` |
| Q8 | 移除 `get.geojs.io` 硬编码，默认不发起第三方请求，需显式配置 `VITE_IP_GEO_PROXY_URL` | `f6f8eeb31` |
| Q11 | `jwtgen` 保留 stdout，新增 `--output`（0600）与敏感输出警告 | `f6f8eeb31` |

### 仍未实施（需产品/架构决策，非安全补丁）

- **Q1 Live 计费**：需要新增开播价格 quote 冻结与按时长结算管道，触碰"缺价不得放行"红线，不在本轮安全修补范围。当前零成本行为已在代码注释与本文档标注。
- **Q4 iframe JWT**：`buildEmbeddedUrl` 把 JWT 放进 URL 是既有跨版本契约（付费页集成依赖），改 embed token 需同步后端与前端两处协议。
- **Q5 插件 bridge token / CSP unsafe-inline**：需重新设计插件桥接会话模型。
- **Q10 localStorage 存 token**：改 HttpOnly cookie 涉及后端 CORS/CSRF 整体改造，且 `frontend/` 正由另一会话改造中。
- **Q2 refresh token 复用撤销 family**：由 teammate 实施中（tombstone 方案），完成后补记。

### Q2 实施（refresh token 复用撤销 family）

`8ae8ef7eb`：新增 `MarkRefreshTokenConsumed` / tombstone 查询到 refresh token cache 接口，
Redis 实现与内存实现（`memory_refresh_token_cache.go`）同步补齐；轮转时写入 `consumed:{tokenHash} -> FamilyID`
（沿用原 TTL），命中复用即 `DeleteTokenFamily` 并返回 `ErrRefreshTokenReused`。
验证：`go build ./...` exit 0；`internal/service`、`internal/repository`、`internal/handler` 定向测试全绿。

### 覆盖状态更新

改用 `sensenova/glm-5.2` 后，此前"扫不动"的文件全部成功产出，覆盖缺口已基本补齐：

- 已补扫：`group_model_allowlist`、`server_timing`、`artex_guard`、`routes/gateway`、`service/yunmao`、`handler/admin/account_data`、`middleware/request_logger`。
- 仍建议后续补扫（本轮未做）：`ent/schema/*.go`、`migrations/*.sql`、部分 `service` 超大文件、前端 `api/admin` 与 `types/utils`、`deploy/tools`。

### 本轮验证汇总

- `go build ./...` → exit 0
- `internal/server`、`internal/server/middleware`、`internal/server/routes`、`internal/handler`、`internal/handler/yunmao`、`internal/handler/admin`、`internal/service`、`internal/service/yunmao`、`internal/repository`、`internal/pkg/httputil`、`cmd/jwtgen` → 全绿
- `internal/handler/admin` 在满载并发下曾出现一次失败，隔离连跑两次均通过（`ok 22.9s` / `ok 28.1s`），判定为既有 flaky，非本轮改动引入

## 追加记录（Live 计费 + 前端加固，2026-10-20 凌晨）

### Q1：Live 会话按时长计费（已实施）

**Live 是什么**：不是文本模型，是 OpenAI 的 Realtime/语音会话能力（上游 `intent=quicksilver&architecture=avas`，
经 `POST /v1/live` 建会话 + `GET /v1/live/:call_id` 建控制通道，媒体不经过网关）。
模型名来自 `session.model`，缺省 `gpt-live`；该名字在价格目录里**没有条目**，所以不能走 token 计费。

**实现（`9c0b7d296`）**：

- 新增 `BillingService.CalculateLiveSessionCost(durationMs, perMinutePrice, rateMultiplier)`，口径 `单价 × 分钟数 × 倍率`。
- 复用既有分组字段 `audio_realtime_price_per_min`（ent/admin/cache 链路本就齐全，无需新增迁移）。
- **开播即冻结单价**：`LiveCallIdentity.RealtimePricePerMin` → `LiveCallRecord.RealtimePricePerMin`，结算用冻结值，避免会话期间改价导致报价与扣费不一致。
- 结算写 `UsageLog.TotalCost/ActualCost/BillingMode`；`finalizeLiveCall` 里原来的 `TODO(billing)` 已消除。

**与既有 `CalculateAudioCost("realtime")` 的关键差异**：后者在分组未配置时会回落到内置默认价 `0.05/分钟`。
Live 明确不允许猜价——未配置单价时返回 `ErrLivePricingUnavailable`，写零成本账单并告警，符合"缺价不得放行"红线。

**验证**：新增 3 个测试（按时长计费、缺价不伪造成本、计费函数三种拒绝路径）。
**变异验证**：把缺价分支改回"回落 0.05"，两个测试立即失败；恢复后通过。
全量回归：`internal/service` 236s、`internal/handler` 242s、`internal/server/routes` 39s 全绿。

### Q4+Q5：前端嵌入页加固（已实施）

`0d43699d5`：

- **令牌不再进 URL**：`buildEmbeddedUrl` 移除 `token` 参数。iframe 的 `src` 会进入浏览器历史、`Referer` 与各级访问日志，
  把 JWT 放进去等于经 URL 泄露。改为 iframe `@load` 后经 `postMessage` 定向交付（`targetOrigin` 精确限定为菜单配置的源）。
- **iframe 加 sandbox**：`allow-scripts allow-same-origin allow-forms allow-popups-to-escape-sandbox`，
  禁止 top-level 跳转/弹窗/下载，防止嵌入页劫持整站导航。
- **加 `referrerpolicy="strict-origin-when-cross-origin"`**，避免带 query 的完整 URL 泄露给下游。

**验证**：前端 typecheck exit 0；381 文件 / 3013 测试全通过。
**变异验证**：把 `token` 塞回 URL，`embedded-url.spec.ts` 立即失败；恢复后通过。

### 仍未实施

- **Q10 localStorage 存 token 改 HttpOnly cookie**：涉及后端 CORS/CSRF 整体改造与会话模型变更，属架构级调整。
- **Q5 插件 bridge token 会话模型**：需重新设计插件桥接协议。
