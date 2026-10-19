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

