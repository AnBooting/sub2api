# 架构侦察文档索引

> 由 Agent Team 并行侦察产出（2026-10-10）。每份文档独立、带源码行号证据。
> 全部为**只读侦察**，未修改任何源码。

| 文档 | 范围 | 状态 |
|---|---|---|
| [00-architecture.html](diagrams/00-architecture.html) | **交互式架构总览图**（archify，9/9 校验通过，8 处源码证据已验证） | ✅ |
| [01-request-billing.md](01-request-billing.md) | 请求链路与计费定价 | 🔄 进行中 |
| [02-scheduling.md](02-scheduling.md) | 调度与并发控制 | 🔄 进行中 |
| [03-media.md](03-media.md) | 图片与视频生成链路 | ✅ 已完成 |
| [04-payment-fallback.md](04-payment-fallback.md) | 支付充值与兜底分组 | 🔄 进行中 |
| [05-frontend.md](05-frontend.md) | 前端结构与构建 | ✅ 已完成 |

## 核心结论速览

### 系统形态

Sub2API 是一个**单二进制** AI API 网关：前端 Vue SPA 构建产物被 `//go:embed all:dist` 内嵌进 Go 可执行文件（`backend/internal/web/embed_on.go:29`），
运行时只需要 PostgreSQL 18 + Redis 8。Compose 部署为三容器（app / postgres / redis），后两者不对宿主暴露端口。

### 请求主链路

```
客户端 → 反向代理(:443) → Gin 中间件链(:8080) → 网关 Handler(/v1/*)
    → 协议转换 → 调度器选号 → 上游 AI 服务
    → 用量记录 → 计费 → PostgreSQL
```

### 已确认的关键事实

| 主题 | 事实 | 证据 |
|---|---|---|
| 图片生成 | **三条独立链路**：同步 / 异步 Redis(24h TTL) / 批量 PostgreSQL | `routes/gateway.go:253-266` |
| 视频生成 | **确实存在**，但只有 Grok/xAI 与 Seedance/Ark 两种原生透传 | `routes/gateway.go:270-281`、`migrations/220` |
| 视频的致命缺口 | **无本地任务表 + 被动结算**（客户端不轮询就不扣费） | `service/grok_media.go:431-440` |
| 视频平台限制 | `migrations/220` 明确 "Videos are Grok/xAI-only" | `migrations/220:1` |
| 计费兜底风险 | 未知视频模型回落到 **2K 图片价** | `billing_service.go:2385` |
| 兜底分组 | **机制已存在**：`fallback_group_id`、`fallback_group_id_on_invalid_request` | `ent/schema/group.go:202-209` |
| 前端构建门禁 | `build` = `check:i18n && vue-tsc -b && vite build` | `frontend/package.json:9` |
| 前端路由 | 57 条，权限 fail-open 到后端兜底 | `frontend/src/router/` |
| CNY 计价 | fork 已实现，默认**关闭**，缺价拒绝转发 | `service/pricing_native_cny.go` |

### 需求归属

| 需求 | 归属 |
|---|---|
| 1 国模 RMB / Claude·GPT 美元 | fork 已实现 → **验证补漏** |
| 2 调度并发 | **我们** |
| 3 卡网嵌入 + 自动充值 | **我们**（部分基础已有） |
| 4 兜底分组 | **我们**（机制已有，配置/扩展） |
| 5 DeepSeek 峰谷 | fork 已实现 → **验证补漏** |
| 6 图片/视频兼容 | **我们** |

详见 [../13-requirements-mapping.md](../13-requirements-mapping.md)。
