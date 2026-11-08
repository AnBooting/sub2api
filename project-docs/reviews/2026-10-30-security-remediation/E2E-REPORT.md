# 线上基线 E2E 报告（改前基线）

| 项 | 值 |
|---|---|
| 站点 | https://sub2.lansn.icu |
| 线上版本 | `develop` HEAD `f9d8bc855`（字段收敛改动**尚未部署**） |
| 分支 | `fix/security-remediation-20261030` |
| 浏览器 | Playwright 1.64 + Chromium 1234，headless，1440×1000，locale zh-CN |
| 脚本 | `F:\\sub2\\.deployprobe\\e2e\\baseline.cjs`（17 页主流程）、`baseline-extra.cjs`（补测页面） |
| 原始结果 | `F:\\sub2\\.deployprobe\\e2e\\user-baseline.json`、`admin-baseline.json`、`admin-baseline-extra.json` |
| 截图 | `F:\\sub2\\.deployprobe\\e2e\\*.png`（`.deployprobe/` 已被 git 忽略，不进仓库） |
| 执行时间 | 2026-10-30 约 19:44–19:47 (+08:00) |
| 执行人 | frontend（task-3） |

> 说明：截图与 JSON 体积大且是一次性证据，按团队约定留在 `.deployprobe/e2e/`（git 忽略），
> 本文件只保存**结论与路径引用**。

## 1. 账号

| 角色 | 账号 | 备注 |
|---|---|---|
| 用户 | `3029445478@qq.com` | uid=3，登录后落地 `/dashboard`，余额 105.01 Credit |
| 管理员 | `admin-39bb646744@sub2api.local` | uid=1 |

## 2. 结果总表

| 维度 | 用户端 | 管理端 |
|---|---|---|
| 页面数 | 9 | 8 |
| 通过 | **9 / 9** | **7 / 8** |
| console error | **0** | **2**（同一条 500 的连带） |
| console warning | 0 | 0 |
| pageerror（未捕获异常） | 0 | 0 |
| API 请求数 | 77 | 108 + 28（补测两页） |
| API 非 2xx | **0** | **1**（`/admin/groups/usage-summary` 500） |
| requestfailed | 0 | 0 |
| 页面文本含 undefined/NaN/[object Object] | 否（9/9） | 否（8/8） |

## 3. 用户端逐页

| # | 路由 | 页面 | HTTP | body 字数 | API 非 2xx | console error | 截图 |
|---|---|---|---|---|---|---|---|
| 0 | `/login` | 登录（改前） | 200 | — | 0 | 0 | `user-00-login.png` |
| 1 | `/login` | 登录后跳转 | 200 | 1055 | 0 | 0 | `user-01-login.png` |
| 2 | `/dashboard` | 仪表盘 | 200 | 1066 | 0 | 0 | `user-02-dashboard.png` |
| 3 | `/keys` | API 密钥 | 200 | 656 | 0 | 0 | `user-03-keys.png` |
| 4 | `/usage` | 使用记录 | 200 | 3946 | 0 | 0 | `user-04-usage.png` |
| 5 | `/available-channels` | 可用渠道 | 200 | 346 | 0 | 0 | `user-05-available-channels.png` |
| 6 | `/model-plaza` | 价卡 | 200 | 362 | 0 | 0 | `user-06-model-plaza.png` |
| 7 | `/monitor` | 渠道状态 | 200 | 4819 | 0 | 0 | `user-07-monitor.png` |
| 8 | `/video-tasks` | 视频任务 | 200 | 162 | 0 | 0 | `user-08-video-tasks.png` |
| 9 | `/redeem` | 兑换 | 200 | 385 | 0 | 0 | `user-09-redeem.png` |

**路由注意**：SPA 路由是 `/video-tasks`；`/videos` 是网关媒体路由，直接访问 404 属预期行为，不作为失败项。

**内容偏少不是缺陷**：`/model-plaza` 只有 1 个分组（`2`）1 个模型（`deepseek-v4.1-flash`），
`/available-channels` 只有 1 渠道（`RelayFor`）1 分组 —— 这是线上真实数据量。

## 4. 管理端逐页

| # | 路由 | 页面 | HTTP | body 字数 | API 非 2xx | console error | 截图 |
|---|---|---|---|---|---|---|---|
| 0 | `/login` | 管理员登录 | 200 | — | 0 | 0 | `admin-00-login.png` |
| 1 | `/admin/dashboard` | 仪表盘（含部分可用提示区域） | 200 | 827 | 0 | 0 | `admin-01-admin-dashboard.png` |
| 2 | `/admin/users` | 用户 | 200 | 1668 | 0 | 0 | `admin-02-admin-users.png` |
| 3 | `/admin/accounts` | 账号 | 200 | 785 | 0 | 0 | `admin-03-admin-accounts.png` |
| 4 | `/admin/groups` | 分组 | 200 | 1000 | **1（500）** | **2** | `admin-04-admin-groups.png` |
| 5 | `/admin/channels/pricing` | 渠道 | 200 | 602 | 0 | 0 | `admin-05-admin-channels.png` |
| 6 | `/admin/settings` | 系统设置 | 200 | 1318 | 0 | 0 | `admin-06-admin-settings.png` |
| 7 | `/admin/usage` | 用量明细 | 200 | 5158 | 0 | 0 | `admin-07-usage.png` |
| 8 | `/admin/redeem` | 兑换码（F-02 限流相关） | 200 | 864 | 0 | 0 | `admin-08-redeem-codes.png` |

### 4.1 唯一失败项（改前既有缺陷，非本轮引入）

```
GET /api/v1/admin/groups/usage-summary?timezone=Asia%2FShanghai  → 500
console error #1: Failed to load resource: the server responded with a status of 500 ()
console error #2: Error loading group usage summary: {status:500, code:500, message:'Failed to get group usage summary'}
```

- 页面未白屏：前端已捕获并 toast，分组列表其余部分正常渲染。
- Lead 已复现「与 timezone / 日期参数无关，恒 500」，根因与 N-02 同类
  （`service.ErrUsageStatsIncomplete` 被 handler 映射成 500），**已并入 task-8** 交 api-surface 按
  「200 + degraded/`unavailable_sections`」统一口径修复。
- 回归时本项列为**必测**：api-surface 已把它统一为
  **HTTP 503 + `reason=usage_stats_incomplete`**（`backend/internal/handler/admin/usage_stats_unavailable.go`），
  不是 200。前端收到后应渲染「统计不完整」降级提示，**不画空图表、不显示 0、不当红色故障**，且 console error 归零。

## 5. 回归基线判据（部署后按此比对）

| 判据 | 基线值 | 回归要求 |
|---|---|---|
| 用户端 9 页全部 200 且 body 非空 | 9/9 | 不劣化 |
| 用户端 console error | 0 | 仍为 0 |
| 用户端 API 非 2xx | 0 | 仍为 0 |
| 任何页面出现 undefined / NaN / [object Object] | 无 | 仍无（字段收敛后的**核心风险点**） |
| 管理端 8 页可达 | 8/8 | 不劣化 |
| `/admin/groups` 的 usage-summary | 500 | **应变为 503 + reason=`usage_stats_incomplete`**，页面用量列显示「统计不完整」降级提示（不是红色错误、不显示 0） |
| 管理端 console error | 2 | **应为 0** |
| 设置页 SMTP 未配置阻断提示 | 本基线无（新功能） | 部署后按 `smtp_configured` 实际值渲染 |

## 6. 复现方式

```powershell
cd F:\sub2\.deployprobe\e2e
$env:NODE_PATH='E:\dsh-mcp-runtime\playwright-mcp\node_modules'
node baseline.cjs          # 用户端 9 页 + 管理端 6 页
node baseline-extra.cjs    # /admin/usage、/admin/redeem
```

脚本要点（供 reviewer 核对口径）：

- 每页导航用 `waitUntil: 'domcontentloaded'`（`networkidle` 在长连接页会超时），随后显式
  `waitForLoadState('networkidle', 15s)` + 2.2s 静置，再截图与取文本。
- console 只采集 `error`/`warning`，并按页面切片；`pageerror` 单列（未捕获异常）。
- network 只采集 URL 含 `/api/` 的响应，记录状态码与路径；`requestfailed` 单列。
- 「undefined 泄漏」用 `document.body.innerText` 正则 `undefined|NaN|\[object Object\]` 检测。
## 7. 本轮前端改动清单与验证（改后）

| 项 | 值 |
|---|---|
| typecheck（`vue-tsc -b`） | **EXIT 0** |
| 测试（`vitest run`） | **Test Files 435 passed (435)，Tests 3456 passed (3456)，EXIT 0** |
| i18n 门禁（`check:i18n`） | 通过（新增 key 均 zh/en 同步） |
| 换行符 | 全部改动/新建文件逐个按字节校验，CRLF=0（纯 LF）；`git diff --check` 无告警 |
| git | 未 commit / 未 push（按团队约定由 Lead 统一提交） |

### 7.1 改动文件（13 改 + 14 新）

**修改**

| 文件 | 内容 |
|---|---|
| `frontend/src/api/client.ts` | N-02：把 `503 + reason=usage_stats_incomplete` 收敛为统一 code `USAGE_STATS_INCOMPLETE` |
| `frontend/src/api/admin/settings.ts` | `Settings` 新增可选只读字段 `smtp_configured?: boolean` |
| `frontend/src/api/admin/payment.ts` | 删除 `/admin/payment/channels` 整组 4 个死方法（后端从未注册该路由，前端 0 调用方） |
| `frontend/src/api/__tests__/client.spec.ts` | +2：503+reason 收敛；普通 503 不误判 |
| `frontend/src/components/user/UserErrorRequestsTable.vue` | F-06：错误行接入 `GatewayFailureBadge`（缺价徽标优先，`model_not_found` 走 v-else-if，互斥） |
| `frontend/src/views/user/UsageView.vue` | N-02：`loadChartData`/`loadModelStats` 命中时整块降级；trend/models/group 三图不画空 |
| `frontend/src/views/admin/GroupsView.vue` | N-02：`#cell-usage` 在统计未就绪时显示「统计不完整」，不再显示 0 |
| `frontend/src/views/admin/SettingsView.vue` | I-01：邮件区块前插入 SMTP 未配置阻断提示 |
| `frontend/src/router/index.ts` | `/admin/security-audit` 补 redirect（见 §7.3） |
| `frontend/src/i18n/locales/{zh,en}/dashboard.ts` | `usageStatsIncomplete.*`、`gatewayFailure.*` |
| `frontend/src/i18n/locales/{zh,en}/admin/settings.ts` | `smtp.notConfiguredTitle/notConfiguredHint/notConfiguredBadge` |

**新增**

| 文件 | 作用 |
|---|---|
| `src/utils/smtp.ts` | `isSmtpConfigured()`：服务端只读字段的单一判定入口，缺省→false |
| `src/utils/usageStatsAvailability.ts` | `isUsageStatsIncomplete()`：N-02 的单一判定入口 |
| `src/components/common/UsageStatsIncompleteNotice.vue` | 「统计不完整」提示（`role="status"` + 琥珀色，非红色故障） |
| `src/components/admin/settings/SmtpNotConfiguredNotice.vue` | SMTP 未配置阻断提示（`role="alert"`） |
| `src/components/user/gatewayFailure.ts` | F-06 分类：`model_not_found` vs `pricing_unavailable` |
| `src/components/user/GatewayFailureBadge.vue` | 两类失败的徽标（各自 `data-testid`） |
| `src/components/layout/__tests__/sidebarRoutes.spec.ts` | 侧栏 path → router 可达性（表驱动） |
| 其余 7 个 `__tests__` spec | 见 §7.2 |

### 7.2 新增测试（9 个文件 / 32 条）

| spec | 条数 | 锁住什么 |
|---|---|---|
| `utils/__tests__/smtp.spec.ts` | 4 | 字段缺失→false；非布尔 truthy 不当作已配置；有 host 草稿仍判未配置 |
| `components/admin/settings/__tests__/SmtpNotConfiguredNotice.spec.ts` | 2 | role=alert；zh/en 文案非空且不与面板 description 重名 |
| `components/user/__tests__/gatewayFailure.spec.ts` | 8 | 结构化 reason 优先；message 兜底；两种口径不混淆；普通错误不误判 |
| `components/user/__tests__/GatewayFailureBadge.spec.ts` | 4 | 各自 testid；缺价琥珀/not_found 中性；zh/en 文案互不相同 |
| `utils/__tests__/usageStatsAvailability.spec.ts` | 5 | 拦截器 code 与原始 reason 都认；普通 503 不算 |
| `components/common/__tests__/UsageStatsIncompleteNotice.spec.ts` | 3 | role=status 非 alert、非红色；zh/en 文案含「暂/会恢复」语义 |
| `components/user/__tests__/UserErrorRequestsTable.gatewayFailure.spec.ts` | 4 | 缺价徽标优先且与新徽标互斥；一行最多一个徽标 |
| `api/__tests__/client.spec.ts`（追加） | 2 | 503+reason 收敛；普通 503 不误判 |
| `components/layout/__tests__/sidebarRoutes.spec.ts` | 5 | 见 §7.3 |

### 7.3 方法论：为什么「解析源码」而不是「运行时枚举」（task-10）

`sidebarRoutes.spec.ts` 断言「侧栏每一条 path 都能被 router 解析」，一次性防住所有死菜单项，
而不是只针对 `/admin/security-audit` 写一条。关键设计是**读 `AppSidebar.vue` 源码提取 path 字面量**，
而不是挂载组件后枚举 DOM：

- 侧栏项是 `computed` + `featureFlag` 过滤出来的，**线上 `risk_control_enabled=false` 时整个安全审计分组根本不渲染**；
  运行时枚举根本测不到它 —— 而「被开关藏起来的死菜单」恰恰是最容易长期漏掉的一类。
- 源码解析覆盖全部声明，与线上开关状态无关。

配套的 `expandOnly` 语义断言（第 5 条）来自实测发现：`/admin/security-audit` 是
`expandOnly: true` 的分组父项，渲染为 `<button>`、点击只展开不导航（`AppSidebar.vue:958-963`），
所以「点菜单白屏」不成立；但**直接访问 URL 会落到 404 catch-all**。
仓库 4 个 `expandOnly` 分组里 `/admin/channels`、`/admin/affiliates`、`/admin/orders` 三个都配了
redirect 记录，只有 security-audit 漏了 —— 补 redirect 才是对齐既有约定，删菜单项会连带砍掉
`/admin/risk-control` 与 `/admin/prompt-audit` 两个真实可用页面。

**变异验证三态**（本项目硬要求）：加 redirect 前测试红（精确指出 `/admin/security-audit`）→
回退 redirect 再次红 → 恢复后绿。

### 7.4 已知偶发（如实记录，非本轮引入）

全量跑 `vitest run` 时偶发 1 次 `Unhandled Rejection`，来自
`src/views/admin/__tests__/AccountsView.selectAllResults.spec.ts` 的
「loading all results fails」用例（该用例**故意**让 `list` 抛错，Vue 异步点击处理器在
断言结束后才结算 Promise）。隔离运行 5/5 通过，整包重跑 EXIT=0。
**不在本轮改动文件内，非本轮引入，未修改。**

## 8. 未完成项（如实标注）

| 项 | 状态 | 原因 / 后续 |
|---|---|---|
| F-05（`/groups/available` 字段收敛）前端适配 | **未开始（等待中）** | api-surface 的 handler 侧收敛尚未落盘；按 Lead 裁定**不提前**改类型（先改窄会在后端未收敛时产生「运行时字段缺失但无编译错误」） |
| F-04（`/channel-monitor-v2` 用户域收敛）前端适配 | **未开始（等待中）** | 同上 |
| I-01（`smtp_configured`）真实数据验证 | **部分完成** | 前端判定/提示/测试已就绪；线上该字段尚未下发，故仅验证了「缺失→阻断」的降级路径 |
| 改后 17 页 E2E 回归 | **未执行** | 字段收敛与 N-02 修复均未部署到 https://sub2.lansn.icu；部署后按 §5 判据执行 |
| `/admin/groups` usage-summary 由 500 → 503 的线上确认 | **未执行** | 同上（需部署后复测） |

> 下一轮输入：`FIELD-INVENTORY.md`（白名单 + 内嵌复用点 + 硬约束）与本文 §5 回归判据。
