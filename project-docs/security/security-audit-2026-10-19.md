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
