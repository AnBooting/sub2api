# 线上验证证据（sub2.lansn.icu）

采集时间：2026-10-30 18:13–19:05（Asia/Shanghai）
采集者：Lead
站点：https://sub2.lansn.icu/ （EdgeOne → Caddy → Sub2API，响应头 `via: 1.1 Caddy`、`Server: TencentEdgeOne`）
线上版本：`GET /api/v1/settings/public` → `data.version = "0.2.17-fork.f9d8bc855"`

## 1. 前端部署一致性（回答「前端到底部署没」）

### 1.1 版本对齐
| 项 | 值 |
|---|---|
| 线上 `data.version` | `0.2.17-fork.f9d8bc855` |
| 本地 `git rev-parse --short HEAD`（develop） | `f9d8bc855` |
| 结论 | 一致 |

### 1.2 产物级比对
线上 `/` 引用的主包：`/assets/index-DEkhF0Ff.js`（194,820 B）。
本地 `backend/internal/web/dist/index.html` 引用：`/assets/index--depgilV.js`，产物时间 `2026-10-21 19:16:14`。
→ **本地内嵌 dist 是陈旧构建**（被 .gitignore，不随仓库更新）。

拉取线上全部 178 个静态产物（`/assets/*`）后逐串检索 HEAD 独有标记：

| 标记 | 引入提交 | 线上出现位置 | 命中数 |
|---|---|---|---|
| `partialUsage` | `f9d8bc855`（Live 结算/部分可用仪表盘） | `assets/DashboardView-DfRYY1N7.js` | 1 |
| `chartsUnavailable` | 同上 | 同上 | 1 |
| `usage_availability` | 同上 | 同上 | 1 |
| `unavailable_sections` | 同上 | 同上 | 1 |
| `processingUpdate` | `9098e84a0`（视频任务错误口径） | `assets/VideoTasksView-Dw0XMuqi.js`、`assets/index-Dw97W2f_.js`(zh) | 1 / 1 |
| `last_error_code` | 同上 | 同上 | 1 / 1 |
| `processing_delayed` | 同上 | `assets/index-Dw97W2f_.js`(zh)、`index-CySmwDoE.js`(en) | 1 / 1 |

**结论：线上前端 = 仓库 HEAD 的构建产物，两笔最新前端改动都在线。**

### 1.3 接口与产物自洽
`GET /api/v1/admin/dashboard/snapshot-v2?start_date=2026-10-23&end_date=2026-10-30`（管理员 token）
→ `200`，`data.unavailable_sections = ["trend","models"]`，与前端新口径一致。

### 1.4 为什么「看起来没部署」
1. PR #7（`feat/frontend-fluent-redesign-clean`，Fluent/Azure + 暖色电影感重设计）在 GitHub 状态 **CLOSED**，从未合并：
   `git rev-list --left-right --count develop...origin/feat/frontend-fluent-redesign-clean` → `259  5`（develop 领先 259，该分支领先 5）。
   线上 `index-hEMTp4QF.css`（279,889 B）中 `#14b8a6`（上游默认 Teal）出现 30 次，PR #7 的 Azure 主色 `#1a9dce` **0 次**。
2. 本地 `backend/internal/web/dist/` 陈旧（见 1.2），任何本地 `go build` 都会内嵌旧前端。

## 2. F-01 全站 IP 限流可绕过（已复现）

请求：`POST /api/v1/auth/login`，body `{"email":"nobody@example.invalid","password":"wrong"}`。
同秒对照（同一客户端，连续请求，keep-alive）：

| 条件 | 状态序列 | 判定 |
|---|---|---|
| 不带转发头 | 401×20 → **429**（第 21 起） | 限流生效 |
| 固定 `X-Real-IP: 203.0.113.200` | 401×20 → 429 | 该头参与分桶 |
| 固定私网 `X-Real-IP: 10.9.9.200` | 401×25，无 429 | 私网值走 fallback |
| **每请求轮换 `X-Real-IP: 198.51.100.10..34`** | **401×25，0 次 429** | **绕过成立** |
| 每请求轮换 `CF-Connecting-IP` | 401×23，0 次 429 | 同源问题 |

根因（源码定位）：
- `backend/internal/middleware/rate_limiter.go:139`：`r.Allow(ctx, key+":"+clientIPForRateLimit(c), ...)`，`RateLimitOptions` 只有 `FailureMode`，**无账号维度**。
- `backend/internal/middleware/rate_limiter.go:116-122` → `ippkg.GetSecurityClientIP(c,false)`。
- `backend/internal/pkg/ip/ip.go:164-172`：快照 `trustForwarded=true` 时走 `GetClientIP`。
- `backend/internal/pkg/ip/ip.go:104-148` `resolveLegacyForwardedHeaderIP`：`CF-Connecting-IP` → `X-Real-IP` → `X-Forwarded-For`，**非私网立即返回**（信任方向反转）。
- 默认值：`backend/internal/config/config.go:2130` `viper.SetDefault("security.trust_forwarded_ip_for_api_key_acl", true)`；线上 `GET /api/v1/admin/settings` → `api_key_acl_trust_forwarded_ip=true`、`forwarded_client_ip_headers=[]`。
- 另有 `backend/internal/service/setting_service.go:352-359`：mode 标记缺失且未配 `trusted_proxies` 时**强制把开关改回 true**，会覆盖运维加固意图。

## 3. 其余条目裁决（逐条实测）

| 编号 | 裁决 | 证据 |
|---|---|---|
| F-02 `/redeem` 限流 | **修正：原判定错误，实际有限流**（见 §3.1） | Lead 抽样 25 次全 404 被判成「无限流」，是因为**未触达阈值**；qa-live 复测到第 30 次起 429 |
| F-03 `admin@sub2api.local` 可抢注 | **成立** | `GET /api/v1/admin/users` → `9:admin@sub2api.local:user:active`（报告创建的 uid=9 确实存在）；`isReservedEmail`（`service/auth_service.go:1404`）只挡 4 个 OAuth 合成域 |
| F-04 `/channel-monitor-v2/*` 对普通用户全开 | **成立** | 普通用户 token：`/snapshot` 200（含 `config.version=3`、`refresh_interval_seconds=60`、`platforms[].enabled`）、`/matrix` 200 |
| F-05 `/groups/available` 泄露运营字段 | **成立** | 返回 `daily_limit_usd/weekly_limit_usd/monthly_limit_usd`、`is_exclusive`、`fallback_group_id*`、`image_price_1k..4k`、`video_price_480p..1080p`、`search_price_per_1k`、`audio_*`、`deepseek_pricing_state` |
| F-06 模型存在性泄露 | **部分不成立** | `GET /v1/models/gpt-5.5` 与 `GET /v1/models/totally-fake-model-zzz` **同为 404 model_not_found**，无差异；但 `POST /v1/chat/completions` 对二者**同为 503 {"message":"internal error","type":"pricing_unavailable"}**（与 `/v1/models` 口径不一致 + 文案误导）→ 记为**新发现缺陷 N-01** |
| F-07 `/usage/:id` 403/404 差异 | **成立** | `/usage/1`→403 `Not authorized to access this record`；`/usage/2`(自己)→200；`/usage/999999`→404 `USAGE_LOG_NOT_FOUND` |
| I-01 `send-verify-code` 假成功 | **成立（代码级）** | `service/auth_service.go:345-394` 仅 `EnqueueVerifyCode` 入队即返回成功；投递失败只在 `service/email_queue_service.go:80-100` 打日志，API 不可见 |
| I-02 `/setup/status` 硬编码 | **成立** | `server/routes/common.go:21-31` 恒返回 `needs_ready:false/step:"completed"` |
| I-04 认证体长度指纹 | **接受风险** | 三种 401 体长度差异来自不同中间件语义，统一会破坏前端错误分支 |
| R-01..R-12 | 与报告一致 | `role=user` token 访问 `/api/v1/admin/users` → 403；管理员 → 200；无越权/注入迹象 |

### 端点矩阵（本次抽样，均为线上真实请求）
| 端点 | 用户 | 管理员 |
|---|---|---|
| `/api/v1/keys` | 200 | — |
| `/api/v1/usage`、`/usage/stats`、`/usage/dashboard/stats` | 200 | — |
| `/api/v1/videos/tasks` | 200（空列表） | — |
| `/api/v1/channels/available` | 200 | — |
| `/api/v1/model-plaza` | 200 | — |
| `/api/v1/channel-monitor-v2/{snapshot,matrix}` | **200** | 200 |
| `/api/v1/admin/{users,dashboard/stats,dashboard/snapshot-v2,accounts,groups,channels,settings,redeem-codes,usage}` | **403** | 200 |
| `/api/v1/admin/payment/orders` | — | 200 |
| `/api/v1/dashboard/stats`、`/api/v1/admin/orders`、`/api/v1/admin/ops/overview` | 404（未注册，前端未使用） | 同 |


## 3.1 F-02 更正（Lead 判定被 qa-live 实测推翻）

**首次判定（错误）**：Lead 连续 25 次无效兑换码得到 `404 REDEEM_CODE_NOT_FOUND`、0 次 429，据此写成「`/redeem` 无任何速率限制」。

**复测（qa-live，同日 20:18）**：
- 前 **29** 次 → `404 REDEEM_CODE_NOT_FOUND`；第 **30** 次起 → `429 REDEEM_RATE_LIMITED`；
- 阈值常量 `service/redeem_service.go:28` `redeemMaxFailedAttempts = 30`，窗口 10 分钟（`repository/redeem_cache.go:15`）；
- 证据：`.deployprobe/qa/10-billing-redeem.json`、`17-429-contract.json`。

**更正结论**：`/redeem` **本就有按用户的失败限流**（30 次 / 10 分钟），Lead 的抽样（25 < 30）不足，属**误判**。报告的 F-02「无任何速率限制」不成立。

**但由此暴露的真实缺陷（记为 R-01，P2）**：
1. `429 REDEEM_RATE_LIMITED` **缺少 `Retry-After` 头**（无 retry-after、无 rate-limit-*），客户端只能盲重试；
2. 计数口径是「窗口内累计、成功后不清零」，正常用户误输几次 + 后续重试可能较早触顶；
3. 阈值 30 次对自动化枚举几乎不构成约束（30/10min = 180 次/小时，若码值熵不足仍可枚举）—— 本轮按 P2 处理：新增一层按用户 5/min 的独立限流（task-9），并把「Retry-After 缺失」单列为遗留项。

**教训（写进验证纪律）**：判定「有限流/无限流」必须先确认**阈值常量**再设计抽样；抽样次数不足会把「有阈值」误判成「无限制」。本条是本轮唯一一处 Lead 自己判定被推翻的结论，已同步修订 `PLAN.md`。

## 3.2 新增发现（本轮复测产生，非原报告条目）

| 编号 | 级别 | 现象 | 证据 |
|---|---|---|---|
| N-01 | P1 | `POST /v1/chat/completions` 对未定价模型返回 `503 {"type":"pricing_unavailable","message":"internal error"}` —— type 说缺价、message 说内部错误，且对未知模型与未定价模型**同响应** | Lead 与 qa-live 双向复现 |
| N-02 | P2 | 统计未对账（`ErrUsageStatsIncomplete`）被映射成 **500**：`/admin/dashboard/trend`（裸调用 500，带 user_id/group_id 200）、`/admin/groups/usage-summary`（恒 500）；用户端同源接口则**静默返回空数组**（被读成「今天没用量」） | qa-live `21-trend-rootcause.json`；Lead 复现 groups/usage-summary |
| N-03 | P2 | `POST /v1/chat/completions` 传 `messages: []` 被接受并计费（prompt_tokens≈35），应 400 | qa-live 4 次复现 |
| R-01 | P2 | `/redeem` 的 429 缺 `Retry-After` | qa-live |
| D-01 | P2 | 本地 `backend/internal/web/dist/` 是陈旧构建产物且被 gitignore，本地 `go build` 会内嵌旧前端 | Lead 产物级比对 |
| D-02 | P2 | HTML 内联 `__APP_CONFIG__.version` 为空串（`/api/v1/settings/public` 的 version 正常） | Lead；deploy-ops 复核 |
| D-03 | P2 | `deploy/Caddyfile` 是直连基线；CDN 拓扑下会把所有用户坍缩到一个 IP 桶 | deploy-ops |
| O-01 | 观察 | `PUT /api/v1/user` 冷路径 5.0s，触发 EdgeOne 回源超时返回 **522**（非应用 5xx），重试即 200 | qa-live |
| P3-01 | 建议 | 多个列表接口对 `page/page_size/start_date/end_date/days` 越界值静默忽略或钳制（不报 400），契约松散但无安全影响 | qa-live 283 用例 |
| P3-02 | 建议 | `/admin/security-audit` 侧栏死菜单项（点进去白屏）；`/admin/payment/channels` 前端死代码 | qa-live |

## 4. 复现命令（供复核）

```powershell
# F-01 绕过（期望：轮换后 25 次全 401）
node -e "const b=JSON.stringify({email:'nobody@example.invalid',password:'wrong'});(async()=>{const o=[];for(let i=0;i<25;i++){const r=await fetch('https://sub2.lansn.icu/api/v1/auth/login',{method:'POST',headers:{'Content-Type':'application/json','X-Real-IP':'198.51.100.'+(10+i)},body:b});o.push(r.status)}console.log(o.join(','))})()"

# F-01 对照（期望：第 21 次起 429）
node -e "...同上但去掉 X-Real-IP..."

# F-02（期望：全 404，无 429）
# F-07（期望：403 / 200 / 404 三种）
```

## 5. 未验证项（本轮不覆盖）
- 并发竞态（兑换/下单/扣费）：需有效兑换码或可用支付通道，线上 `payment_enabled=false`。
- 文件上传/插件/备份恢复：均在 admin 之后，且本轮不触碰这些面。
- SSRF：线上三个分组均未开放生图，无法触达 0.2.18 修复的异步生图转存路径。
- 兑换码熵值：无有效样本，F-02 的实际可利用性无法定量。