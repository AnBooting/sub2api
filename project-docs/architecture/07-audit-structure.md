# 审计后的前后端/模块结构与边界（2026-10-19）

> 与 `06-repository-map.md` 配合阅读。本文聚焦全量审查后确认的边界、生成物策略和未实现项。

## 1. 仓库形态

```
F:/sub2
├─ backend/            Go 单模块：cmd + internal/{server,handler,service,repository,ent,pkg}
├─ frontend/           Vue 3 SPA：src/{api,components,views,stores,i18n,utils}
├─ deploy/             Docker Compose、Caddy、部署脚本
├─ tools/              仓库工具（repo-map、repo-intelligence 等）
├─ project-docs/       二开需求/架构/审查文档
├─ _reference/         外部参考材料，只读
└─ ARTEX/              本地只读分析目录（已 gitignore，不入库）
```

## 2. 后端边界

- HTTP 入口：`backend/internal/server/routes`，管理路由在 `admin.go`，网关路由在 `gateway.go`。
- Handler：只做协议/参数/响应转换；`handler/admin` 仅管理 API。
- Service：业务与计费；`service/wire.go` 与 `cmd/server/wire_gen.go` 是装配层。
- Repository：数据访问；Ent 模型 `backend/ent/schema`，生成物 `backend/ent` 不手改。
- Migration：顺序编号 SQL，当前最新 250；新增从 251 起，不修改既有迁移。

## 3. 前端边界

- 页面/组件分层：`views` 页面，`components` 复用组件，`api` 只做 HTTP 契约，`stores` 保存跨页状态。
- `v-html` 仅允许经过 sanitize 的内容；`KeyUsageView.iconSvg` 已接入 `sanitizeSvg`。
- i18n：`src/i18n/locales/zh|en`；新增文案必须两侧同步，`check:i18n` 是 build 前置检查。

## 4. 部署边界

- 生产形态：单 Go 二进制内嵌前端产物，PostgreSQL + Redis 三容器。
- `.env`、`data/`、`postgres_data/`、`redis_data/` 不入库；API Key/密码/SSH 信息不入库。
- 外部回调/重定向 URL 必须按配置 allowlist 校验，避免把 JWT/令牌放入不可控 URL。

## 5. 未实现与待产品决策

- OpenAI Live：`finalizeLiveCall` 目前写零成本 usage，`AllowLive=true` 时可被余额用户使用；需要产品确认按次/按时长计费或关闭该入口。
- admin group/redeem/proxy stats：本轮改为明确 `implemented:false`，不再返回假零；真实聚合需接入 dashboard/usage 数据。
- Yunmao 回调：代码为 fail-closed 占位，未拿到上游签名/报文文档前不能启用。
- DingTalk appToken：进程内缓存，多实例会产生重复 token 请求；多实例上线前应迁移 Redis。
- Plugin UI bridge token、外部 custom page iframe JWT、SVG 存储边界：已由前端审计报告，需后端会话/CSP 模型改动后再实施。

## 6. 生成物与验证

- `backend/ent`、`wire_gen.go` 是生成物；改动 schema/ProviderSet 后必须 `go generate ./ent` / wire。
- 前端 build 必须使用 pnpm 9，命令见根 `AGENTS.md`。
- 本轮输出与 findings 见 `project-docs/security/security-audit-2026-10-19.md`。

