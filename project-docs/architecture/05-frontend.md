# 05 · 前端结构与构建（只读侦察）

| 项 | 值 |
|---|---|
| 状态 | 已完成（只读侦察，未修改任何源码） |
| 侦察范围 | `frontend/`（Vue 3 SPA）+ `backend/internal/web/`（前端产物嵌入） |
| 分支 / 基线 | `develop` @ `9150a7a8`；上游基线 `fork-base` = `3a6fd1c9`（v0.2.15） |
| 侦察时间 | 2026-10-10 |
| 产出 | 本文件（唯一写入） |

**证据来源**（全部为仓库内实际文件与 git 对象）

- `frontend/package.json`、`vite.config.ts`、`vitest.config.ts`、`tailwind.config.js`、`.npmrc`、`index.html`
- `frontend/src/` 源码树（`router/`、`api/`、`stores/`、`i18n/`、`utils/`、`views/`、`components/`）
- `backend/internal/web/embed_on.go`、`embed_off.go`；`backend/internal/web/dist/`
- `Makefile`（根目录，上游自带，与 `fork-base` 逐字节一致）
- git：`git show 11109acd -- frontend`、`git show 0e6f5efb -- frontend`、`git show fork-base:Makefile`

**未确认项**（明确标注，不猜测）

1. 未实际执行 `pnpm install` / `typecheck` / `test:run` / `build`；本文件对"能否通过"只陈述脚本定义与静态结构，**不声明已通过**。
2. `backend/internal/web/dist/` 现存产物的构建时间与对应源码 revision **未确认**（未比对 dist 哈希与当前源码）。
3. 前端路由守卫的权限判定全部依赖后端返回的 `user.role`；后端侧的实际校验点由其他侦察文档覆盖，本文不重复。
4. `vitest` 覆盖率阈值（80%）**未确认**是否在 CI 中被强制（`test:coverage` 未出现在 `Makefile` 或 `package.json` 的 `build` 链路中）。
5. `frontend/src/router/README.md` 与 `frontend/src/stores/README.md` 内容**明显滞后于代码**（router README 只列了 6 条 admin 路由，实际 23 条）；本文以 `index.ts` 源码为准。

---

## 1. 技术栈与构建

### 1.1 依赖版本（`frontend/package.json`）

| 分类 | 包 | 版本（package.json） |
|---|---|---|
| 框架 | `vue` | ^3.5.43 |
| 路由 | `vue-router` | ^4.2.5 |
| 状态 | `pinia` | ^2.1.7 |
| i18n | `vue-i18n` | ^9.14.5 |
| HTTP | `axios` | ^1.20.0 |
| 工具 | `@vueuse/core` | ^10.7.0 |
| 图表 | `chart.js` / `vue-chartjs` | ^4.4.1 / ^5.3.0 |
| 支付 SDK | `@stripe/stripe-js`、`@airwallex/components-sdk` | ^9.0.1 / ^1.30.2 |
| 表格/导出 | `@tanstack/vue-virtual`、`xlsx`、`file-saver` | ^3.13.23 / ^0.18.5 / ^2.0.5 |
| 富文本 | `marked`、`dompurify` | ^17.0.1 / ^3.4.14 |
| 构建 | `vite`、`@vitejs/plugin-vue`、`vue-tsc`、`typescript` | ^5.0.10 / ^5.2.3 / ^2.2.0 / ~5.6.0 |
| 测试 | `vitest`、`@vue/test-utils`、`jsdom`、`@vitest/coverage-v8` | ^2.1.9 / ^2.4.6 / ^24.1.3 / ^2.1.9 |
| 样式 | `tailwindcss`、`postcss`、`autoprefixer` | ^3.4.0 / ^8.4.32 / ^10.4.16 |
| 质量 | `eslint` ^8.57.0、`eslint-plugin-vue` ^9.25.0、`vite-plugin-checker` ^0.9.1 | |

`package.json` **未声明 `packageManager` 字段**；仓库规则（`AGENTS.md`）要求使用 `corepack pnpm@9`，因为全局 pnpm 11.x 与 `pnpm-lock.yaml` 不兼容（`ERR_PNPM_LOCKFILE_CONFIG_MISMATCH`）。`frontend/.npmrc` 设置 `legacy-peer-deps=true`、`ignore-scripts=false`。

### 1.2 npm scripts（`package.json:6-17`）

| script | 命令 | 说明 |
|---|---|---|
| `dev` | `vite` | 开发服务器 |
| `check:i18n` | `vitest run src/i18n/__tests__/localeKeyCompleteness.spec.ts` | **独立可跑的 i18n 门禁** |
| `build` | `pnpm run check:i18n && vue-tsc -b && vite build` | i18n → 类型检查 → 打包 |
| `typecheck` | `vue-tsc --noEmit` | 只做类型检查，不产出 |
| `test` / `test:run` / `test:coverage` | `vitest` / `vitest run` / `vitest run --coverage` | |
| `lint` / `lint:check` | `eslint . --ext .vue,.js,...,.mts` | `lint` 带 `--fix` |

> 关键点：**`build` 会先跑 `check:i18n`**。任何新增文案若只加在 `zh` 或只加在 `en`，打包会直接失败（见 §5.3）。

### 1.3 构建配置（`frontend/vite.config.ts`，176 行）

| 配置项 | 值 | 行号 |
|---|---|---|
| 产物目录 | `outDir: '../backend/internal/web/dist'` | 107 |
| 清空产物 | `emptyOutDir: true` | 108 |
| 别名 | `@` → `src`；`vue-i18n` → `vue-i18n/dist/vue-i18n.runtime.esm-bundler.js` | 95-99 |
| define | `__INTLIFY_JIT_COMPILATION__: true` | 104 |
| dev server | `host 0.0.0.0`，`port = VITE_DEV_PORT 或 3000` | 157-159 |
| dev proxy | `/api`、`/v1`、`/setup` → `VITE_DEV_PROXY_TARGET 或 http://localhost:8080` | 160-173 |
| 类型检查插件 | `checker({ vueTsc: true })` | 89-91 |
| 品牌注入插件 | `injectPublicSettings(backendUrl)`，仅 `apply: 'serve'` | 53-78, 92 |

`manualChunks`（109-155）按库分包：`vendor-vue`（vue/vue-router/pinia/@vue）、`vendor-ui`（@vueuse/xlsx）、`vendor-chart`（chart.js/vue-chartjs）、`vendor-i18n`（vue-i18n/@intlify）、`vendor-stripe`，其余归 `vendor-misc`；应用代码不手工干预，交给 Rollup 按入口分包。

**CSP 相关设计**（重要约束）：别名强制使用 vue-i18n 运行时版本 + `__INTLIFY_JIT_COMPILATION__ = true`，注释明确目的是避免 `unsafe-eval`（`vite.config.ts:97-104`）。

**开发/生产一致性**：dev 模式由 Vite 插件 fetch 后端 `/api/v1/settings/public`（拼在 backendUrl 之后）并注入 `window.__APP_CONFIG__`（超时 2s，失败回退到运行时 API 调用）；生产由后端把同一份 JSON 注入 HTML。前端 `main.ts:44-45` 在 `app.use(router)` 之前同步调用 `appStore.initFromInjectedConfig()`，消除首屏闪烁（`frontend/src/stores/app.ts:428-437`）。

### 1.4 产物去向与后端嵌入

| 文件 | 构建标签 | 行为 |
|---|---|---|
| `backend/internal/web/embed_on.go:1,29` | `//go:build embed` | `//go:embed all:dist` 把 `dist` 整个嵌入二进制；`fs.Sub(frontendFS,"dist")` + `http.FileServer`；SPA 回退到 `dist/index.html` |
| `backend/internal/web/embed_off.go:1,24-26` | `//go:build !embed` | `NewFrontendServer` 直接返回 `errors.New("frontend not embedded")`；`InvalidateCache` 为 no-op |

`backend/internal/web/dist/` **当前实际存在**（`index.html`、`logo.svg`、`assets/*.js|css`，含按路由切分的 `ChannelsView-*.js` 等）。`backend/cmd/server/main.go:106` 有 "Serve embedded frontend if available" 分支。

---

## 2. 目录结构

`frontend/src/` 共 14 个一级目录：

| 目录 | 职责 | 规模/代表文件 |
|---|---|---|
| `views/` | 路由级页面组件 | `user/`(22 文件)、`admin/`(29 文件)、`auth/`(13)、`setup/`(1)、`public/`(1)、顶层 `HomeView.vue`/`KeyUsageView.vue`/`ModelPlazaView.vue`/`NotFoundView.vue` |
| `components/` | 可复用组件；`admin/` 下按域再分（account、channel、group、monitor、payment、proxy、settings、subscription、usage、user、announcements） | `components/layout/AppLayout.vue`、`components/common/`、`components/channels/` |
| `api/` | 接口层：`client.ts` 统一 axios 实例 + 各业务模块 | 用户端 23 文件；`api/admin/` 40 文件 |
| `stores/` | Pinia store | 8 个 store + `index.ts` barrel + `README.md` |
| `utils/` | 纯函数工具 | 44 个 `.ts`（`format.ts`、`pricing.ts`、`billingCredits.ts`、`apiError.ts` 等） |
| `i18n/` | 国际化实例 + locales | `index.ts`、`locales/{zh,en}/`、`__tests__/`(9 个 spec) |
| `types/` | 全局类型 | `index.ts`（含 `PublicSettings`、`UsageLog`）、`global.d.ts`（`window.__APP_CONFIG__`）、`payment.ts`、`openaiReferrals.ts` |
| `router/` | 路由表 + 守卫 + 标题解析 | `index.ts`(1028 行)、`meta.d.ts`、`title.ts`、`setupRedirect.ts`、`README.md` |
| `composables/` | 组合式函数 | 21 个，如 `useRoutePrefetch.ts`、`useNavigationLoading.ts`、`useTableLoader.ts`、`useStepUp.ts` |
| `constants/` | 常量与平台清单 | `platformCatalog.ts` + `platformCatalog.builtin.json`、`platforms.ts`、`channel.ts` |
| `features/` | 大型功能子应用 | `channel-monitor-v2/`、`prompt-audit/`（各自带 `__tests__/`） |
| `styles/`、`assets/`、`__tests__/` | 样式、图标、测试环境 | `__tests__/setup.ts` 为 vitest setupFiles |

`src/` 下 **365 个** `*.spec.ts`/`*.test.ts` 文件（`Get-ChildItem -Recurse -Include *.spec.ts,*.test.ts` 实测）。

---

## 3. 路由结构

`frontend/src/router/index.ts`（1028 行）：`createWebHistory(import.meta.env.BASE_URL)`，全部路由组件用 `() => import()` 懒加载（`732-743`）。

### 3.1 路由分组（按 `index.ts` 实际定义统计）

| 分组 | 条数 | 代表路径（行号） | meta |
|---|---|---|---|
| Setup | 1 | `/setup`(23) | `requiresAuth:false` |
| 公开 | 13 | `/home`(34)、`/login`(43)、`/register`(53)、`/email-verify`(63)、`/auth/...callback`(72-136)、`/forgot-password`(142)、`/reset-password`(152)、`/key-usage`(161)、`/legal/:documentId`(170)、`/model-plaza`(179) | `requiresAuth:false` |
| 用户端 | 19 | `/`→`/home`(191)、`/dashboard`(195)、`/keys`(207)、`/batch-image`(219)、`/usage`(232)、`/redeem`(244)、`/affiliate`(256)、`/available-channels`(268)、`/profile`(280)、`/subscriptions`(292)、`/purchase`(305)、`/orders`(318)、`/payment/*`(330-378)、`/custom/:id`(389)、`/monitor`(494) | `requiresAuth:true, requiresAdmin:false` |
| 管理端 | 23 | `/admin`→`/admin/dashboard`(402)、`/admin/{dashboard,ops,audit-logs,users,groups,channels/pricing,channels/monitor,subscriptions,accounts,plugins,announcements,proxies,redeem,promo-codes,settings,risk-control,prompt-audit,usage}`、`/admin/affiliates/{invites,rebates,transfers}`、`/admin/orders{,/dashboard,/plans}` | `requiresAuth:true, requiresAdmin:true` |
| 404 | 1 | `/:pathMatch(.*)*`(720) | 仅 `title` |

> 注意：`/admin/channels` 无独立路由记录，实际入口是 `/admin/channels/pricing`（`name: 'AdminChannels'`，466-475）。

### 3.2 meta 字段（`router/meta.d.ts`）

| 字段 | 类型 | 用途 |
|---|---|---|
| `requiresAuth` | `boolean?` | 默认 `true`（守卫按 `!== false` 判定） |
| `requiresAdmin` | `boolean?` | 默认 `false` |
| `requiresPayment` | `boolean?` | 需 `public_settings.payment_enabled !== false` |
| `requiresRiskControl` | `boolean?` | 需 `risk_control_enabled !== false` |
| `requiresSubscription` | `boolean?` | 订阅为 opt-out，仅显式 `false` 拦截 |
| `title` / `titleKey` / `descriptionKey` | `string?` | 静态标题 / i18n key（由 `router/title.ts:resolveRouteDocumentTitle` 解析） |
| `breadcrumbs` / `icon` / `hideInMenu` | — | 声明了类型，但本文件未在守卫中消费 |

### 3.3 权限控制（`router.beforeEach`，782-980）

执行顺序（同一函数内串行，任一分支 `next(...)` 后 `return`）：

1. `navigationLoading.startNavigation()`（784）
2. **仅首次导航** `authStore.checkAuth()`（`authInitialized` 标志，789-792）→ 从 localStorage 恢复会话
3. `document.title` 由 `resolveRouteDocumentTitle(to, appStore.siteName, customMenuItems)` 计算（797-801）
4. `/setup`：调 `getSetupStatus()`，若 `!needs_setup` 则按登录/管理员态重定向（807-817）
5. **公开路由**（`requiresAuth === false`）：
   - 已登录访问 `/login`、`/register` → 跳 `/admin/dashboard` 或 `/dashboard`；但 `backendModeEnabled && !isAdmin` 时**不跳转**（避免死循环，822-832）
   - `/model-plaza` 双重开关：`model_plaza_enabled === false` 拦截、`model_plaza_require_auth === true` 未登录跳登录；仅在设置**成功加载**时才拦截（fail-open 到后端 404 兜底，833-863）
   - backend mode 下未登录用户只放行白名单（`BACKEND_MODE_ALLOWED_PATHS` + 回调路径 + 待验证会话，754-780、864-871）
6. **需鉴权**：未登录 → `/login?redirect=<fullPath>`（877-884）
7. **管理员**：`requiresAdmin && !isAdmin` → `/dashboard`（887-891）；是管理员则拉 `adminComplianceStore.fetchStatus()`，遇 `423 + ADMIN_COMPLIANCE_ACK_REQUIRED` 记录待确认（893-905）
8. **功能开关**（911-947）：先确保 `publicSettingsLoaded`（避免"设置未加载 = 未启用"的误拦截），再按 `requiresPayment`/`requiresRiskControl`/`requiresSubscription` 判 `=== false`
9. **简易模式**（`isSimpleMode`，950-963）：限制 `/admin/subscriptions`、`/admin/redeem`、`/subscriptions`、`/redeem`
10. **backend mode**（966-976）：管理员全放行，非管理员只走白名单
11. `next()`

`router.afterEach`（985-995）：`endNavigation()` + 懒初始化 `useRoutePrefetch(router)` 并在浏览器空闲时预取。
`router.onError`（1001-1026）：识别 chunk 加载失败（`Failed to fetch dynamically imported module` 等），用 `sessionStorage['chunk_reload_attempted']` 做 10 秒节流后整页 reload，避免部署更新后白屏死循环。

守卫测试：`src/router/__tests__/guards.spec.ts`(16.5 KB)、`feature-access.spec.ts`、`title.spec.ts`、`wechat-route.spec.ts`。

---

## 4. API 层约定

### 4.1 `frontend/src/api/client.ts`（277 行）

- 实例（21-28）：`baseURL = getAPIBaseURL()`（`api/url.ts`，`VITE_API_BASE_URL` 默认 `/api/v1`）、`withCredentials: true`、`timeout: 30000`、`Content-Type: application/json`。
- **请求拦截器**（41-77）：
  - `Authorization: Bearer <localStorage.auth_token>`（44-47）
  - `Accept-Language: getLocale()`（供后端翻译，50-52）
  - **所有 GET** 自动附加 `params.timezone = Intl.DateTimeFormat().resolvedOptions().timeZone`（55-60）
  - `X-Admin-UI-Request` / `X-User-UI-Request` 标记头（62-70），判定逻辑在 `api/adminUIRequest.ts:70-78`
- **响应拦截器 · 成功路径**（82-102）：解包后端统一信封 `{code,message,data}` —— `code === 0` 时把 `response.data` 替换为 `data`；`code !== 0` 时 `Promise.reject({status,code,message,reason,metadata})`；非信封响应原样返回。
- **响应拦截器 · 错误路径**（103-274）：
  - 取消请求（`ERR_CANCELED`）原样抛出，避免被误判成网络错误（106-108）
  - `404 + message === 'Ops monitoring is disabled'` → 写 `localStorage.ops_monitoring_enabled_cached=false`、派发 `ops-monitoring-disabled` 事件、在 `/admin/ops` 时跳 `/admin/settings`，reject code `OPS_DISABLED`（122-144）
  - `423 + ADMIN_COMPLIANCE_ACK_REQUIRED` → 派发 `admin-compliance-required` 事件（146-161）
  - **401 刷新流程**（165-255）：见 §4.2
  - 其余错误统一 reject `{status,code,reason,error,message,metadata}`（258-265）；无响应时 reject `{status:0, code:'ERR_NETWORK'}`（269-273）

### 4.2 Token 刷新（`frontend/src/api/tokenRefresh.ts`，229 行）

| 机制 | 实现 |
|---|---|
| 单文档去重 | 模块级 `inFlightRefresh` promise 复用（34、216-228） |
| 跨标签页串行 | `navigator.locks.request('sub2api-auth-token-refresh', ...)`（200-202） |
| 无 Web Locks 回退 | `mayHaveUncoordinatedPeer = true` 走 `waitForPeerRefresh`（204、176-182） |
| 轮换 refresh token 的对端采纳 | `readPeerRefreshResult` 比对 localStorage 中的 `refresh_token`/`access_token`/`userID`，采纳对端结果而非登出（87-109） |
| 提交标记 | `persistTokenPair` 最后写 `refresh_token`，让其他标签页把它的变化当作提交点（127-132） |
| 超时常量 | `TOKEN_REFRESH_TIMEOUT_MS=30s`、`PEER_REFRESH_WAIT_MS=1s`、`PEER_REFRESH_GRACE_MS=1s`、`PEER_REFRESH_POLL_MS=25ms`（10-13） |
| localStorage 键 | `auth_token`、`refresh_token`、`auth_user`、`token_expires_at`（5-8） |

`client.ts` 侧：401 且非 auth 端点且存在 refresh token 时置 `originalRequest._retry`，刷新后重放原请求（165-188）；刷新期间会话被替换（`AUTH_SESSION_CHANGED`）或刷新不可用（`TOKEN_REFRESH_UNAVAILABLE`，状态 0/429/5xx）时不登出；否则清 4 个 localStorage 键、写 `sessionStorage.auth_expired='1'`、跳 `/login`（189-230）。测试：`src/api/__tests__/tokenRefresh.spec.ts`、`client.spec.ts`（17.8 KB）。

### 4.3 admin API 组织方式

- 一域一文件：`frontend/src/api/admin/*.ts` 共 **40 个**模块（`accounts.ts` 35 KB、`ops.ts` 37 KB、`settings.ts` 54 KB 为最大三块）。
- 每个模块默认导出一个对象（如 `api/admin/channels.ts:210`：`const channelsAPI = { list, getById, create, update, remove, getModelDefaultPricing, syncPricingModels }`）。
- 统一 barrel：`api/admin/index.ts` 组装 `adminAPI` 聚合对象并具名再导出（43-113），同时 re-export 组件常用类型（118-129）。上层通过 `api/index.ts:27` 的 `export { adminAPI }` 使用。
- **已知不一致**：`api/admin/pricing.ts`（fork 新增）**没有**注册进 `adminAPI` barrel，由 `components/admin/PricingSettingsPanel.vue:45` 直接 `import ... from '@/api/admin/pricing'`。这是唯一一个绕过聚合入口的 admin 模块（`grep 'admin/pricing'` 仅命中该处与它的测试）。
- `api/index.ts` 同时导出用户端模块：`authAPI`、`keysAPI`、`usageAPI`、`userAPI`、`redeemAPI`、`paymentAPI`、`userGroupsAPI`、`userChannelsAPI`、`batchImageAPI`、`totpAPI`、`passkeyAPI`、`announcementsAPI`、`channelMonitorUserAPI`。

---

## 5. 国际化

### 5.1 实例（`frontend/src/i18n/index.ts`，99 行）

- `createI18n({ legacy:false, locale:getDefaultLocale(), fallbackLocale:'en', messages:{}, warnHtmlMessage:false })`（33-41）—— **Composition API 模式**，初始 `messages` 为空。
- 语言码只有 `'en' | 'zh'`（3）；持久化键 `sub2api_locale`（7）；默认 `'en'`（8）；未存过时按 `navigator.language.startsWith('zh')` 选 `zh`（25-30）。
- **懒加载**：`localeLoaders` 动态 `import('./locales/en'|'./locales/zh')`，`loadedLocales` Set 去重（10-13、43-54）。`initI18n()` 在 `main.ts:53` 于 `app.mount` 前 await。
- `setLocale()`（62-87）除切语言外还会**重算 `document.title`**（动态 import `router`/`stores` 以避免循环依赖）。

### 5.2 locales 目录组织

```
src/i18n/locales/
├── zh/index.ts   # spread: landing, common, dashboard, channelMonitorV2, batchImage, admin(命名空间), misc
├── en/index.ts   # 同上，键集合必须与 zh 完全一致
├── zh|en/landing.ts        (8.2 KB / 8.3 KB)
├── zh|en/common.ts         (19.8 / 21.3 KB)
├── zh|en/dashboard.ts      (43.4 / 44.8 KB)
├── zh|en/channelMonitorV2.ts
├── zh|en/batchImage.ts
├── zh|en/misc.ts
└── zh|en/admin/index.ts    # 9 个模块: overview, channels, accounts, resources, ops, settings, audit, promptAudit, plugins
```

注意 `admin` 是**嵌套命名空间**（`...admin` 而非 spread 平铺，`zh/index.ts:15`），所以 admin 文案路径形如 `admin.channels.form.autoMapping`；而 `settings.ts` 自身再嵌套一层 `settings`（`admin.settings.pricing.title`）。

### 5.3 `check:i18n` 工作原理（`src/i18n/__tests__/localeKeyCompleteness.spec.ts`，90 行）

| 断言 | 逻辑 | 行号 |
|---|---|---|
| en/zh schema 完全相同 | `flattenLeafKeys(en)` 与 `flattenLeafKeys(zh)` 双向差集均为空 | 68-71 |
| 每个叶子非空字符串 | 遍历所有叶子，类型非 string 或 trim 后为空即失败 | 73-84 |
| 源码静态引用的 key 全部存在 | 扫描源码收集 key，与 en/zh 叶子集合比对 | 86-89 |

key 收集规则（19-57），三条正则：

| 用途 | 模式（源文件原文） |
|---|---|
| 翻译调用 | `(?:\bi18n\.t|\$t|\bt)\s*\(\s*(['"])([^'"\r\n]+)\1` —— 覆盖 `t()`、`$t()`、`i18n.t()` |
| meta / keypath 引用 | `(?:keypath|titleKey|descriptionKey)\s*[:=]\s*(['"])...` |
| `<i18n-t>` 组件 | 匹配 `keypath="..."` 属性 |

扫描面：`import.meta.glob('../../**/*.{ts,vue}', { query:'?raw', eager:true })`，排除 `__tests__/` 与 `*.spec.ts`。

**新增文案规范**：① zh 与 en 同步加同一 key；② 值必须是非空字符串；③ 组件里用 `t('...')` 字面量（不要先拼变量再传，正则匹配不到会导致漏检）；④ 动态后缀 key 无法静态校验，需自行保证。其他 i18n 测试：`localesMessageCompile.spec.ts`、`localesNoKeyCollision.spec.ts`、`opsLocaleKeys.spec.ts`、`riskControlLocales.spec.ts` 等共 9 个。

---

## 6. 状态管理（Pinia）

`stores/index.ts` 具名导出全部 8 个 store。全部使用 **setup 语法**（`defineStore('x', () => {...})`）。

| Store | id | 核心 state / getter | 职责 | 文件 |
|---|---|---|---|---|
| `useAuthStore` | `auth` | `user`、`token`、`runMode`、`pendingAuthSession`；`isAuthenticated`、`isAdmin`、`isSimpleMode`、`hasPendingAuthSession` | 登录/注册/登出、localStorage 会话恢复（`checkAuth`）、刷新用户 | `auth.ts`(16.2 KB) |
| `useAppStore` | `app` | `cachedPublicSettings`、`publicSettingsLoaded`、`siteName`、`siteLogo`、`toasts`；`backendModeEnabled`、`hasActiveToasts` | 公共设置注入/拉取、站点品牌、Toast 与全局 loading；`initFromInjectedConfig`、`fetchPublicSettings`、`reset` | `app.ts`(14.2 KB) |
| `useAdminSettingsStore` | `adminSettings` | `opsMonitoringEnabled`、`opsRealtimeMonitoringEnabled`、`opsQueryModeDefault`、`paymentEnabled`、`customMenuItems` | 管理端全局开关；值带 localStorage 缓存以减首屏闪烁；监听 `ops-monitoring-disabled` 事件 | `adminSettings.ts`(4.8 KB) |
| `useAdminComplianceStore` | `adminCompliance` | `status`、`forceVisible`；`required`、`shouldShow`、`expectedPhrase` | 管理员合规确认拦截（配合 423 流程） | `adminCompliance.ts`(3.6 KB) |
| `useAnnouncementStore` | `announcements` | `loading`、`lastFetchTime`；`unreadCount` | 公告列表与未读数 | `announcements.ts`(4.0 KB) |
| `usePaymentStore` | `payment` | `configLoading`、`configLoaded` | 支付配置加载状态 | `payment.ts`(3.3 KB) |
| `useSubscriptionStore` | `subscriptions` | `loading`、`loaded`；`hasActiveSubscriptions` | 用户订阅列表 | `subscriptions.ts`(3.4 KB) |
| `useOnboardingStore` | `onboarding` | 引导流程状态 | driver.js 新手引导 | `onboarding.ts`(2.3 KB) |

> `stores/README.md` 只文档化了 `auth` 与 `app` 两个 store（**已滞后**）。

---

## 7. fork 两个魔改提交的前端改动

### 7.1 总量

| 提交 | 标题 | 前端文件数 | 增删 |
|---|---|---|---|
| `11109acd` | feat(pricing): add native CNY prices and credit billing controls | 41 | +375 / -127 |
| `0e6f5efb` | feat(channels): add model mapping, upstream sync and pricing snapshots | 11 | +526 / -57 |

### 7.2 `11109acd`：原生 CNY 价卡 + 额度计费显示

**新增文件**

| 文件 | 行数 | 作用 |
|---|---|---|
| `src/components/admin/PricingSettingsPanel.vue` | 90 | 设置页"模型价格与额度计费"面板：`native_cny_enabled` / `remote_enabled` 两个开关、USD/CNY 模型计数与最后同步时间、`cny_error` 提示、来源链接（basellm、LLM Abacus）、手动刷新按钮 |
| `src/api/admin/pricing.ts` | 23 | `GET/PUT /admin/channels/pricing/settings`（PUT `timeout: 65000`）、`POST /admin/channels/pricing/refresh`（`timeout: 65000`）；导出 `PricingControls` / `PricingStatus` |
| `src/utils/billingCredits.ts` | 12 | 全局开关 `billingCreditsEnabled`（`ref`）+ `configureBillingCredits()` + `formatBillingAmount()`（开启时输出「12.00 额度」/「12.00 credits」，关闭时「$12.00」；字符串入参按小数位数自适应精度，上限 12 位） |

`PricingSettingsPanel.vue` 行为要点：`revision` 计数器防止过期响应覆盖（54、63-73）；`onMounted` 起 **5 秒轮询**，仅当 `remote_enabled && native_cny_enabled` 且无进行中操作时才拉取（80-89）；保存后调 `app.fetchPublicSettings(true)` 强制刷新公共设置（70）。

**改动文件（按域）**

| 域 | 文件 | 改动要点 |
|---|---|---|
| 类型 | `api/admin/channels.ts`、`api/channels.ts`、`api/modelPlaza.ts`、`components/admin/channel/types.ts` | 各定价结构体新增 `currency?: 'USD' 或 'CNY'` |
| 渠道定价表单 | `components/admin/channel/PricingEntryCard.vue`、`IntervalRow.vue` | `PricingEntryCard` 新增币种下拉（仅 `allowNativeCurrency` 为真时给 CNY 选项，默认 `true`）、CNY 时显示 `manualCNYHint`、所有 `$/M` 标签改为按币种显示 ¥/M 或 $/M；`IntervalRow` 新增 `currency` prop 并透传 |
| 分组 | `views/admin/GroupsView.vue` | `groupPricingFromAPI`/`groupPricingToAPI` 双向补 `currency: entry.currency 或 "USD"`（4406、4432 附近） |
| 设置页 | `views/admin/SettingsView.vue` | 功能开关 tab 插入 `<PricingSettingsPanel />`（7162 附近）；`payment_balance_recharge_multiplier` 在额度模式下 `disabled` 且强制显示 1，并切换为 `rechargeHint` 文案 |
| 全局状态 | `stores/app.ts` | `applySettings` 内调 `configureBillingCredits(config.billing_credits_enabled === true)`；`reset` 中关闭 |
| 公共设置类型 | `types/index.ts` | `PublicSettings.billing_credits_enabled?: boolean`；`UsageLog.pricing_snapshot?`（`currency/source/quota_unit/input_per_token/output_per_token/cache_read_per_token/cache_write_per_token`） |
| 金额格式化 | `utils/format.ts`、`utils/pricing.ts` | `formatCurrency(amount, currency?)` 在 currency 为 undefined 且额度模式开启时走 `formatBillingAmount`；`formatScaled(value, scale, minFractionDigits, currency='USD')` 按币种输出 ¥ 或 $；新增 `formatCreditScaled()` |
| 展示替换 | `KeysView.vue`(37 行)、`admin/usage/UsageTable.vue`、`user/PlatformCostCell.vue`、`user/PlatformUsageBreakdown.vue`、`layout/AppHeader.vue`、`modelPlaza/PlazaModelPricingTable.vue`、`payment/AmountInput.vue`、`user/dashboard/UserDashboard{Charts,RecentUsage,Stats}.vue`、`user/profile/ProfileInfoCard.vue`、`user/{PaymentView,PaymentResultView,RedeemView,SubscriptionsView,UserOrdersView}.vue` | 硬编码美元符号的模板全部替换为 `formatBillingAmount(...)`；`KeysView` 的 4 个金额输入框前缀改为 `billingCreditsEnabled ? t('admin.settings.pricing.creditUnit') : '$'`；`UsageTable` 给实际费用加 `title` 显示「currency · source」快照；`PlazaModelPricingTable.official()` 新增 currency 参数 |
| i18n | `zh/admin/settings.ts`、`en/admin/settings.ts` | 各新增 `settings.pricing.*` 共 15 个 key（title、description、nativeCNY(+Hint)、remote(+Hint)、offline、noCNY、sources、refresh、retry、never、failed、saved、creditUnit、rechargeHint、currency、manualCNYHint） |
| i18n | `zh/dashboard.ts`、`en/dashboard.ts` | `quotaAmount`、`rateLimit5h/1d/7d` 去掉 "(USD)" 后缀（改为纯「额度」口径） |
| 测试 | `components/admin/__tests__/PricingSettingsPanel.spec.ts`(33)、`utils/__tests__/billingCredits.spec.ts`(20)、`components/payment/__tests__/AmountInput.spec.ts`(+1) | 开关联动、CNY 独立保存、额度格式化与 `formatScaled` 币种符号 |

### 7.3 `0e6f5efb`：模型映射 / 上游同步 / 价格快照

**新增文件**

| 文件 | 行数 | 导出 | 作用 |
|---|---|---|---|
| `src/utils/channelModelMapping.ts` | 17 | `supplementIdentityMappings(existing, candidates)` | 从上游候选模型补「同名映射」，保留已有精确/通配规则；跳过 `*` 与重复；用 `Object.defineProperty` 写入，**防 `__proto__` 原型污染**（有专门测试）；返回 `{mapping, added}` |
| `src/utils/channelPricingSnapshot.ts` | 80 | `pricingModelNames`、`emptyModelPricing`、`hasManualPricing`、`uncoveredPricingModels`、`usablePriceSnapshot`、`fillPricingSnapshots` | 价格快照填充核心：`billing_model_source === 'channel_mapped'` 时用映射目标名（精确优先、其次最长通配）；只填「未被手动改过」的条目（`hasManualPricing` 检查 `billing_mode`、8 个价格字段、`fast/flex_multiplier`、`intervals`、`time_pricing.periods`、`reasoning_effort_multipliers`）；`fillPricingSnapshots` **把多模型条目按模型拆开**，避免不同币种/价格被共享；CNY 缺 `cache_write/cache_read` 或含 `NaN`/负值视为不可用，**绝不回落到 0** |
| `src/utils/__tests__/channelModelMapping.spec.ts` | 24 | — | 通配符覆盖、`__proto__`/`constructor` 原型键处理 |
| `src/utils/__tests__/channelPricingSnapshot.spec.ts` | 54 | — | 币种拆分、显式 0 保留、不完整/非法价格不落 0、跨分组去重、映射目标名 |
| `src/views/admin/__tests__/ChannelsView.modelSync.spec.ts` | 扩充至 162+ 行 | — | 见 §8.3 |

**改动文件**

| 文件 | 改动要点 |
|---|---|
| `api/admin/channels.ts` | `ModelDefaultPricing` 新增 `snapshot_ready?: boolean`；`cache_write_price`/`cache_read_price` 放宽为 `number 或 null` |
| `api/admin/groups.ts` | 新增 `getModelMappingCandidates(id, platform)` → `GET /admin/groups/{id}/model-mapping-candidates`；新增 `syncUpstreamModels(group_ids, platform)` → `POST /admin/groups/models/sync-upstream`，返回 `{models, failed_accounts, total_accounts}`；两者注册进 `groupsAPI` |
| `components/admin/channel/PricingEntryCard.vue` | `onModelsUpdate` 的落价条件收紧为 `found && snapshot_ready !== false && (currency !== 'CNY' 或 cache_write_price 与 cache_read_price 均非 null)` |
| `views/admin/ChannelsView.vue` | +180 行，核心改动（详见 §7.4） |
| `i18n/locales/{zh,en}/admin/channels.ts` | 各新增 20 行 key：`autoMapping`、`autoMappingLoading`、`autoMappingHint`、`autoMappingSuccess`、`autoMappingEmpty`、`autoMappingUpToDate`、`autoMappingGroupsChanged`、`autoMappingError`、`syncModelsEmpty`、`syncModelsPartial`、`syncPricingHint`、`modelActionChanged`、`autoPricing`、`autoPricingLoading`、`autoPricingSuccess`、`autoPricingMissing`、`autoPricingNoModels`、`autoPricingError` |

### 7.4 `ChannelsView.vue` 的三个新动作（`0e6f5efb`）

| 动作 | 触发 | 数据流 | 关键防线 |
|---|---|---|---|
| 一键映射 `autoMapModels(sIdx)` | 「一键映射」按钮，要求已选分组 | `Promise.all(groupIDs.map(getModelMappingCandidates))` → `flat()` → `supplementIdentityMappings(section.model_mapping, candidates)` 写回 `section.model_mapping` | 只改映射、**不新增定价条目**；分组变化时用排序后的 id 数组比对，不一致则报 `autoMappingGroupsChanged` 并丢弃结果 |
| 同步上游模型 `syncLatestModels(sIdx)` | 「同步模型」按钮（**移除了 `supportsPricingModelSync(platform)` 前置条件**，任何平台都可见） | `adminAPI.groups.syncUpstreamModels([...new Set(group_ids)], platform)` → `pricingModelNames(...)` → `uncoveredPricingModels(...)` → `section.model_pricing.push(...newModels.map(emptyModelPricing))`（**每个模型一个条目**，旧实现是合并成一个大条目） | `failed_accounts > 0` 报 `syncModelsPartial`；`models.length === 0` 报 `syncModelsEmpty`；`sectionModelState` 快照比对丢弃过期结果 |
| 自动定价 `autoPriceModels(sIdx)` | 「自动定价」按钮 | 收集「无手动定价」的模型（映射目标 + 未覆盖模型）→ **并发上限 4** 逐个 `getModelDefaultPricing(model)` → `fillPricingSnapshots(entries, snapshots)` → 过滤回写 `section.model_pricing` | 单个模型请求失败记 `{found:false}`；`modelResultIsCurrent` 校验分组/映射/定价/来源四项快照，被用户改动则报 `modelActionChanged` 并整体放弃；仅保留「新填了价或原本就有手动价」的条目 |

配套的并发/一致性机制：
- `modelActionPending = computed(() => !!syncingPlatform || !!pricingPlatform || !!autoMappingPlatform)`，三个动作互斥，同时禁用保存按钮（`:disabled="submitting || modelActionPending"`）与表单提交路径（`handleSubmit` 提前返回）。
- `sectionModelState(section)` 把「去重排序后的 groups、mapping、pricing、billing_model_source」序列化为 JSON 字符串做 CAS 快照；`modelResultIsCurrent` 额外检查弹窗仍打开、该 section 仍在 `form.platforms` 中且 `enabled`，防止关闭/重开弹窗后收到迟到结果。

### 7.5 风格噪音（非功能性，供后续 merge 注意）

两处插入代码使用了 **Tab 缩进**，与文件其余部分（2 空格）不一致：

- `frontend/src/i18n/locales/zh/admin/settings.ts:3-10`、`frontend/src/i18n/locales/en/admin/settings.ts:3-10`
- `frontend/src/api/channels.ts:41`、`frontend/src/api/modelPlaza.ts:13`、`frontend/src/types/index.ts:213`（行首为「Tab + 空格」）

不影响 `check:i18n` 与 `vue-tsc`，但 `eslint` 的 indent 规则可能报错；`lint:check` 在 `make test-frontend` 链路中。

---

## 8. 测试

### 8.1 vitest 配置（`frontend/vitest.config.ts`，40 行）

| 项 | 值 |
|---|---|
| plugins | `@vitejs/plugin-vue` |
| alias | 与 `vite.config.ts` 一致（`@`、`vue-i18n` 运行时版本） |
| `globals` | `true` |
| `environment` | `jsdom` |
| `setupFiles` | `./src/__tests__/setup.ts`（内存版 localStorage、`requestIdleCallback`/`cancelIdleCallback` polyfill、`matchMedia` mock 等） |
| `include` | `src/**/*.{test,spec}.{js,ts,jsx,tsx}` |
| `exclude` | `node_modules`、`dist` |
| coverage | provider `v8`，reporter `text/json/html`，阈值全局 **statements/branches/functions/lines = 80** |

测试文件组织：**就近放置** —— 每个源码目录下 `__tests__/`（如 `src/api/__tests__/`、`src/components/admin/channel/__tests__/`、`src/router/__tests__/`），另有跨模块的 `src/__tests__/integration/`。实测 `src/` 下共 365 个 spec/test 文件。

### 8.2 `Makefile` 关键测试清单（`FRONTEND_CRITICAL_VITEST`，`Makefile:3-32`，共 29 条）

```
src/i18n/__tests__/localeKeyCompleteness.spec.ts
src/api/__tests__/client.spec.ts
src/api/__tests__/tokenRefresh.spec.ts
src/api/__tests__/keys.bulkUpdate.spec.ts
src/components/account/__tests__/[OI]ReferralCell.spec.ts
src/components/account/__tests__/[OI]ReferralCell.transport.spec.ts
src/components/account/__tests__/[OI]QuotaResetCell.spark_shadow.spec.ts
src/constants/__tests__/platforms.spec.ts
src/components/account/__tests__/credentialsBuilder.platformCatalog.spec.ts
src/components/account/__tests__/CreateAccountModal.spec.ts
src/components/account/__tests__/EditAccountModal.spec.ts
src/components/account/__tests__/credentialsBuilder.spec.ts
src/components/account/__tests__/OpenCodeGoProtocolRulesEditor.spec.ts
src/components/keys/__tests__/BulkEditKeysModal.spec.ts
src/components/admin/user/__tests__/UserPlatformQuotaModal.spec.ts
src/views/user/__tests__/KeysView.spec.ts
src/api/__tests__/channelMonitorV2.spec.ts
src/views/auth/__tests__/LinuxDoCallbackView.spec.ts
src/views/auth/__tests__/WechatCallbackView.spec.ts
src/views/user/__tests__/PaymentView.spec.ts
src/views/user/__tests__/PaymentResultView.spec.ts
src/views/user/__tests__/ChannelStatusView.mode.spec.ts
src/components/user/profile/__tests__/ProfileInfoCard.spec.ts
src/components/user/profile/__tests__/ProfileIdentityBindingsSection.spec.ts
src/views/admin/__tests__/SettingsView.spec.ts
src/features/channel-monitor-v2/__tests__/designSystem.structure.spec.ts
src/features/channel-monitor-v2/__tests__/monitorFormat.spec.ts
src/features/channel-monitor-v2/__tests__/monitorZoom.spec.ts
src/components/admin/channel/__tests__/PricingEntryCard.modelDefaultPrice.spec.ts
```

（前 3 个文件名含**字面方括号**：`[OI]ReferralCell.spec.ts` 等，已在 `git ls-files` 中确认是真实文件名，不是通配符。）

相关 make 目标（`Makefile:34-57`）：

| target | 命令 |
|---|---|
| `build-frontend` | `pnpm --dir frontend run build` |
| `test-frontend` | `pnpm --dir frontend run lint:check` → `typecheck` → `make test-frontend-critical` |
| `test-frontend-critical` | `pnpm --dir frontend exec vitest run $(FRONTEND_CRITICAL_VITEST)` |

> ⚠️ 两点需要注意：
> 1. `Makefile` 直接用 **`pnpm`**（非 `corepack pnpm@9`）。本机全局 pnpm 为 11.x 时与锁文件不兼容，故按项目规则应改用 `corepack pnpm@9` 或确认 PATH 中的 pnpm 版本。
> 2. `Makefile` 与 `fork-base` **逐字节一致**（`git show fork-base:Makefile` 实测），因此该 29 条清单**不包含 fork 新增的 5 个 spec**：`PricingSettingsPanel.spec.ts`、`channelModelMapping.spec.ts`、`channelPricingSnapshot.spec.ts`、`billingCredits.spec.ts`、`ChannelsView.modelSync.spec.ts`。这两个提交的回归保护只在 `pnpm test:run`（全量）下生效，`make test-frontend-critical` 不会跑到。

### 8.3 fork 新增测试覆盖的行为

| spec | 覆盖点 |
|---|---|
| `components/admin/__tests__/PricingSettingsPanel.spec.ts` | 刷新进行中禁用拉取开关；手动刷新未结束时关掉 remote 仍能正确保存；CNY 开关独立于 remote 保存且不触发 refresh |
| `utils/__tests__/billingCredits.spec.ts` | 额度模式下极小值（0.00000123）不被截断、保留 8 位小数；`formatCreditScaled` 与 `formatScaled` 的币种符号/单位；关闭时回落 `$100.00` |
| `utils/__tests__/channelModelMapping.spec.ts` | 保留自定义/通配映射、忽略空值与重复、`*` 全覆盖时不新增、`__proto__` 不被当作原型 |
| `utils/__tests__/channelPricingSnapshot.spec.ts` | USD/CNY 条目拆分；显式 0 保留；`found:false`/`snapshot_ready:false`/CNY 缺缓存价/`NaN` 一律不落价；跨分组去重；`billing_model_source` 非 `channel_mapped` 时不做名称映射 |
| `views/admin/__tests__/ChannelsView.modelSync.spec.ts` | 平台无需定价目录也能看到同步按钮；请求在飞时禁止二次同步且只调一次；同步结果按币种分别落价；CNY 不完整时保持未定价并报 missing；加载中改动关联分组则丢弃结果；自动定价加载中的人工编辑不被覆盖；一键映射不新增定价条目、重复点击幂等 |

---

## 9. 结论摘要

1. **构建链路清晰且单向**：`frontend` → `vite build` → `backend/internal/web/dist` → `//go:embed all:dist`（`-tags embed`）→ 单二进制。非 embed 构建走 `embed_off.go` 桩，`NewFrontendServer` 直接报错。
2. **两道硬门禁**：`check:i18n`（en/zh schema 一致 + 非空 + 静态引用 key 全覆盖）已挂在 `build` 前；`vue-tsc -b` 紧随其后。`make test-frontend` 额外加 `eslint` 与 29 条关键 vitest。
3. **路由权限是纯前端收敛 + 后端兜底**：`requiresAuth`/`requiresAdmin` + 三个功能开关 meta，全部依赖 `public_settings` 成功加载才拦截（避免瞬时失败误伤），最终由后端 404/403 兜底。
4. **API 层是单一 axios 实例 + 信封解包**：鉴权、locale、timezone、UI 标记头都在请求拦截器；401 刷新用 Web Locks 跨标签页串行并采纳对端轮换 token。
5. **fork 前端改动可归纳为两条线**：① 币种/额度显示口径（`currency` 字段贯穿类型 → 表单 → 展示，`formatBillingAmount` 统一出口）；② 渠道模型映射与价格快照（`channelModelMapping.ts` + `channelPricingSnapshot.ts` 纯函数 + `ChannelsView` 三个带 CAS 快照防护的异步动作）。**「缺价不得放行」在前端体现为 `usablePriceSnapshot` 绝不把缺失/不完整价格落成 0**。
6. **可跟进的三个小问题**：① `api/admin/pricing.ts` 未纳入 `adminAPI` barrel；② `Makefile` 的关键测试清单未纳入 fork 新增的 5 个 spec，且调用裸 `pnpm`；③ 两处插入代码 Tab 缩进不一致。另外 `router/README.md` 与 `stores/README.md` 已明显滞后于实现。
