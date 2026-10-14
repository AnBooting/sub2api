# 架构侦察文档索引

> 由 Agent Team 并行侦察产出（2026-10-10）。5 份文档 + 1 张交互式架构图，全部带 `文件:行号` 证据。
> **全部为只读侦察**：`git diff HEAD -- backend frontend deploy` 为空，源码零改动。

| 文档 | 范围 | 行数 | 状态 |
|---|---|---|---|
| [00-architecture.html](diagrams/00-architecture.html) | **交互式架构总览图**（archify，9/9 校验通过，8 处源码证据已验证） | — | ✅ |
| [01-request-billing.md](01-request-billing.md) | 请求链路与计费定价 | 418 | ✅ |
| [02-scheduling.md](02-scheduling.md) | 调度与并发控制 | 543 | ✅ |
| [03-media.md](03-media.md) | 图片与视频生成链路 | 312 | ✅ |
| [05-frontend.md](05-frontend.md) | 前端结构与构建 | 447 | ✅ |
| [06-repository-map.md](06-repository-map.md) | 项目代码地图、影响面查询与最小上下文流程 | — | 🧪 |

---

## 系统形态

Sub2API 是**单二进制** AI API 网关：前端 Vue SPA 构建产物被 `//go:embed all:dist` 内嵌进 Go 可执行文件
（`backend/internal/web/embed_on.go:29`），运行时只需 PostgreSQL 18 + Redis 8。
Compose 部署三容器（app / postgres / redis），后两者不对宿主暴露端口。

```
客户端 → 反向代理(:443) → Gin 中间件链(:8080) → 网关 Handler(/v1/*)
    → 协议转换 → 调度器选号 → 上游 AI 服务
    → 用量记录 → 计费 → PostgreSQL
```

## 五条最重要的发现

### 1. ⚠️ USD 缺价会「记 0 放行」——与项目红线冲突

`backend/internal/service/openai_gateway_usage.go:255-272`：非国模模型缺价时，
记录 `pricing_missing_record_zero_cost` 警告后**按零成本放行**。

- **国模（CNY）路径是 fail-closed**：`RequiresNativeCNY(model)` 为真时直接 `return err`；
  转发前还有一道 `ValidateNativePricingRequest`（`handler/gateway_inflight_reservation.go:76-82`，
  **即使余额预留开关关闭也无条件执行**）。
- **USD 路径是 fail-open**：缺价 = 零成本放行。

→ 这是**上游既有行为**，不是我们引入的。若要求「任何模型缺价都拒绝」，需单独立项（见 REQ 待办）。

### 2. 视频生成存在，但有致命缺口

- 视频**确实有**：Grok/xAI（`routes/gateway.go:270-281`）+ Seedance/Ark（`:367-371`），共 12 条路由。
- **但被硬编码为 Grok 专属**：`migrations/220` 开头明确 "Videos are Grok/xAI-only"，
  且**没有独立的 `allow_video_generation` 开关**（复用 `allow_image_generation`）。
- **无本地任务表 + 被动结算**：Grok/Seedance 视频任务只在 Redis 存计费快照
  （`service/grok_media.go:431-440`），**只有客户端轮询到 done 才结算**；
  `docs/seedance-api.md:41` 原文：「不查询的任务不会自动结算」。
- **计费兜底风险**：`billing_service.go:2385` 的 `getDefaultVideoPrice` 对未知视频模型
  **回落到 2K 图片价**——与「缺价不得放行」红线冲突。
- 国模媒体链路无现成参考：外部参考材料只有 provider 身份与定价，零媒体能力。

### 3. 调度是两套并行技术栈 + 快照秒级延迟

- **通用网关调度**（`gateway_scheduling.go`，2476 行）与 **[OI] 高级调度器**（`openai_account_scheduler.go`，2960 行）
  是两套独立实现，但**共用同一个 ConcurrencyService 与同一套 Redis 槽位**。
- 调度读的是**快照**（按 `{groupID, platform, mode}` 分桶），账号变更经 **outbox 异步刷新**，
  默认轮询 1s、滞后 5s 告警、10s 强制重建（`config.go:2569-2571`）。
- → **「改了账号并发数但没生效」必须先排除快照延迟这一层。**
- 并发控制细节见 [02-scheduling.md](02-scheduling.md) §3。

### 4. 兜底分组机制已存在，但语义与直觉不同

| 字段 | 真实触发条件 | 证据 |
|---|---|---|
| `fallback_group_id` | **非 [CC] 客户端**请求降级 | `gateway_scheduling.go:922-949` |
| `fallback_group_id_on_invalid_request` | **仅上游返回 prompt too long 时** | `gateway_handler.go:1005-1054` |

⚠️ 字段名里的 "invalid request" **不是**「分组 ID 无效」。
源码中**没有**「请求带不存在的分组 → 兜底」这条路径。

### 5. 外部充值对接有现成抓手，缺三样东西

- **现成**：`POST /api/v1/admin/redeem-codes/create-and-redeem`（`redeem_handler.go:168`）——
  原子建码+兑换、强制 `Idempotency-Key`、同码不同用户 409、中途崩溃自动补兑换。
  契约见 `docs/ADMIN_PAYMENT_INTEGRATION_API.md`。
- **缺口 G1**：无代客下单 API（用户侧全挂 `jwtAuth`，`routes/payment.go:40-44`）。
- **缺口 G2**：无按外部订单号反查。
- **缺口 G3**：无出站 webhook（入账只发邮件，`payment_fulfillment.go:454-474`）。
- 支付系统本身完整（5 provider、三层幂等），**不需要重造**。

---

## 需求归属

| 需求 | 归属 | 依据 |
|---|---|---|
| 1 国模 RMB / Claude·GPT 美元 | fork 已实现 → **验证补漏** | commit `11109acd` |
| 2 调度并发 | **我们** | 无现成能力，见 02 |
| 3 外部充值对接 | **我们** | 有抓手，缺 G1/G2/G3 |
| 4 兜底分组 | **我们**（多为配置） | 机制已存在，见 04 §B |
| 5 DeepSeek 峰谷 | fork 已实现 → **验证补漏** | `pricing_native_cny.go` ratesAt() |
| 6 图片/视频兼容 | **我们** | 视频有缺口，见 03 |


## 已知的基线问题（非我们引入）

- `go test -tags=unit ./...` 有 5 个失败用例（`internal/repository` ×4、`internal/service` ×1），见 [../11-collaboration-norms.md](../11-collaboration-norms.md) §7。
- 根 `Makefile` 的 `FRONTEND_CRITICAL_VITEST`（29 条）**不含** fork 新增的 5 个 spec。
- `api/admin/pricing.ts` 未注册进 `adminAPI` barrel。
