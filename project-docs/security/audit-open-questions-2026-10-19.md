# 审计未决项决策提问（2026-10-19）

> 目的：把本轮全量审查中**已复现、但因涉及产品口径/安全模型/跨版本契约而未擅自改动**的项，整理成需要你拍板的问题。
> 每个问题都给出：现状证据（文件:行）、影响、可选方案、我的建议。你只需回答选择项即可。
> 已修复项与 PR 见 [PR #5](https://github.com/AnBooting/sub2api/pull/5)；完整审计记录见 `project-docs/security/security-audit-2026-10-19.md`。

---

## Q1（Critical，最高优先）OpenAI Live 会话要不要计费？

**现状证据**
- 入口：`backend/internal/server/routes/gateway.go:237` → `POST /live`；另有 `/backend-api/codex/realtime/calls`。
- 落库：`backend/internal/service/openai_live.go:827-853` 明确写着 `TODO(billing)`，`TotalCost`/`ActualCost` 恒为 0，`RateMultiplier: 1`，完全绕过 `recordUsageCore`/`applyUsageBilling`。
- 会话上限：`openai_live.go:26` `defaultLiveMaxSessionDuration = time.Hour`。
- 准入：`backend/internal/handler/openai_live.go:248` 仅由 `apiKey.Group.AllowLive` 控制。

**影响**
余额模式下，极低余额用户可反复开启最长 1 小时的 Live 会话且不扣费；用量日志会写入一条 0 成本记录，账单与成本报表失真。

**需要你决定**
- A. Live 按时长计费（推荐）：冻结开播时的价格 quote，会话结束时按实际 `DurationMs` 走现有 usage/billing 管道。
- B. Live 有意免费：保留零成本，但删除 `BillingType` 误导性赋值，并在文档/后台明确标注“免费能力”。
- C. 暂时关闭 Live：`AllowLive` 默认 false，直到计费方案落地。

**我的建议**：选 A；若短期无法接管道，选 C 而不是继续 B 的模糊状态。

---

## Q2（High）Refresh Token 复用检测要不要真正撤销整个 family？

**现状证据**
- `backend/internal/service/auth_service.go:1797-1801`：`GetRefreshToken` 返回 `ErrRefreshTokenNotFound` 时只打印 `possible reuse attack` 日志，然后返回 `ErrRefreshTokenInvalid`。
- 同一函数其他受损路径（用户删除/停用、TokenVersion 不匹配、绑定不匹配）都会调用 `DeleteTokenFamily`（`:1820/1830/1837/1845`）。
- `ErrRefreshTokenReused`（`:38`）已定义但全仓库未使用。

**影响**
token 被窃取后，攻击者持有已轮转的旧 token 时不会被 containment；合法用户被迫重新登录，攻击者可能继续持有活跃会话。

**需要你决定**
- A. 补 tombstone（推荐）：轮转时写入 `consumed:{tokenHash} -> FamilyID`（原 TTL），命中复用即 `DeleteTokenFamily` 并返回 `ErrRefreshTokenReused`。需要改 refresh token cache 接口 + Redis 实现 + 内存实现。
- B. 维持现状：仅记录日志，接受无 containment。
- C. 改用绝对会话 TTL/单会话模型，放弃 family 轮转（改动更大）。

**我的建议**：选 A，属于标准 rotation-with-reuse-detection 应有行为；成本约 1 个接口 + 2 个实现 + 测试。

---

## Q3（High）管理端 SVG 图标：只在渲染端消毒，还是在存储边界做结构校验？

**现状证据**
- 渲染端已消毒：`frontend/src/components/layout/AppSidebar.vue:97/122/142` 均为 `v-html="sanitizeSvg(...)"`；`KeyUsageView.vue:260` 同样已消毒。
- 存储端未见白名单校验：`backend/internal/handler/admin/setting_handler_update.go` 保存管理端配置时无 SVG 结构校验。

**影响**
DOMPurify 是可靠兜底，但“存储端无约束”意味着任何新增渲染点若漏用 `sanitizeSvg` 就会直接 XSS；且恶意 SVG 会长期留在配置里。

**需要你决定**
- A. 存储边界加白名单（推荐）：仅允许 `<svg>` 子集标签/属性，长度上限，写入即校验。
- B. 改为不接受 SVG，只接受图标 ID/URL（最安全，但会破坏现有自定义图标能力）。
- C. 维持渲染端消毒，接受“新增渲染点需自觉”的风险。

**我的建议**：选 A。

---

## Q4（High）自定义外部页 `CustomPageView` 把 JWT 放进 iframe URL，如何处理？

**现状证据**
- `frontend/src/views/user/CustomPageView.vue:250` 取 `authStore.token`；`:300` 用 `Authorization: Bearer` 拉取资源；`:117-121` 以 `:src="embeddedUrl"` 嵌入 iframe。

**影响**
JWT 进入 URL 会落入浏览器历史、Referer、代理与访问日志；iframe 目标域一旦不可信，token 即可被第三方页面读取。

**需要你决定**
- A. 服务端 allowlist + 短时效非认证 token（推荐）：iframe 用一次性/短 TTL 的 embed token，而非用户 JWT；目标域白名单。
- B. 仅服务端 allowlist，仍用 JWT。
- C. 关闭自定义外部页能力。

**我的建议**：选 A；若短期只做 B，也应在文档标注残余风险。

---

## Q5（High）插件 UI 的 bridge token 与 CSP `unsafe-inline` 要不要收紧？

**现状证据**
- `frontend/src/views/admin/PluginsView.vue:589/612` 使用 `uiSession.bridge_token` 校验 postMessage 来源。
- 后端 `plugin_handler.go` / `plugin_manager.go` 负责下发 UI session。

**影响**
bridge token 若与管理员会话绑定过弱、且 CSP 允许 `unsafe-inline`，恶意插件页面在拿到 token 后可调用桥接能力。

**需要你决定**
- A. token 与会话强绑定 + 缩短 TTL + 移除 `unsafe-inline`（推荐）。
- B. 仅缩短 TTL。
- C. 维持现状，记录为已知风险。

**我的建议**：选 A；这是管理端权限面，收益高于改造成本。

---

## Q6（Medium）代理导入：同名覆盖与 `backup_proxy_name` 缺失降级

**现状证据**
- `backend/internal/handler/admin/account_data.go:261-267` 用 `proxyNameToID[p.Name] = p.ID` 建反查表；`:379` 新建后再覆盖写入。
- `:304-349` 若 `backup_proxy_name` 找不到，仅追加一条 error 并把 `fallback_mode` 降级为 `none`，导入继续。

**影响**
同名代理会让后写覆盖先写，可能导致账号绑到非预期的备用代理；备用代理缺失时静默降级，运维不易察觉。

**需要你决定**
- A. 同名直接拒绝该条（推荐）：导入报错并跳过，保持幂等语义清晰。
- B. 同名按“先到先得”，后者改名/报错。
- C. 维持覆盖行为，仅在结果里增加 warning。

**我的建议**：选 A；导入是批量操作，静默覆盖比失败更危险。

---

## Q7（Medium）`cleanup-ingress-reject-logs` 的删除谓词与未来时间保护

**现状证据**
- 收尾 SQL：`backend/scripts/finalize-ingress-reject-cleanup.sql`，注释要求“滚动发布结束后、备份已验证”才可执行，且 `DROP TABLE`/`DROP COLUMN` 不可逆。
- 清理脚本本身未见对未来时间戳的防护。

**影响**
若清理条件写错或误在滚动发布期间执行，会删除仍需审计的拒绝日志，或让未来时间的记录被当作历史数据清掉。

**需要你决定**
- A. 加 `created_at <= now()` 与保留窗口双条件（推荐），并在脚本里强制 dry-run 输出计数。
- B. 维持现状，仅在 runbook 强调。

**我的建议**：选 A。

---

## Q8（Medium）第三方 IP 地理查询默认外发用户 IP

**现状证据**
- `frontend/src/utils/ipGeoLookup.ts:27-28` 硬编码 `https://get.geojs.io/v1/ip/geo`，`:165` 直接 `fetch`。

**影响**
管理端查看用户 IP 时，用户 IP 会被发送到第三方服务；属隐私外泄与合规风险。

**需要你决定**
- A. 改为后端代理（推荐）：由后端查询并缓存，前端不直连第三方。
- B. 默认关闭，管理员显式开启。
- C. 维持现状，文档标注。

**我的建议**：选 A 或 B。

---

## Q9（Medium）`home_content` 与自定义页面内容缺存储边界校验

**现状证据**
- `backend/internal/handler/admin/setting_handler_update.go:162` 声明 `HomeContent string`；`:1689` 直接 `HomeContent: req.HomeContent` 落库，未见内容白名单/长度校验。

**影响**
与 Q3 同源：存储端不校验，渲染端一旦漏消毒即 XSS。

**需要你决定**：与 Q3 合并处理（A/B/C 同 Q3）。

---

## Q10（Medium）localStorage 存 JWT / refresh token

**现状证据**：前端认证态持久化在 localStorage（`authStore`）。

**影响**：任意 XSS 可直接读取长期凭据。

**需要你决定**
- A. 改 HttpOnly cookie（推荐，改动涉及后端 CORS/CSRF）。
- B. 保留 localStorage，但缩短 refresh 有效期 + 绑定会话指纹。
- C. 维持现状。

**我的建议**：短期 B，中期 A。

---

## Q11（Low/流程）`jwtgen` 把 token 打到 stdout

**现状证据**：`backend/cmd/jwtgen/main.go` 生成后直接打印。

**影响**：CLI 契约要求可管道输出，但会进入终端历史/CI 日志。

**需要你决定**
- A. 保留 stdout，但增加 `--output` 选项与“敏感输出”警告（推荐）。
- B. 强制写文件。
- C. 维持现状。

---

## Q12（流程）OCR 覆盖仍不完整，是否继续补扫？

**现状证据**
- 成功：`auth_service.go`、`gateway_service.go`、`account.go`、`api_key_handler.go`、admin/dto/cmd/config 等批次。
- 失败/未产出：`backend/internal/server`、`handler/yunmao`、`ent/schema`、`migrations`、部分 `service` 大文件、前端剩余视图。
- 失败原因多为 OCR 生成的 `git grep` 正则不完整或 provider 会话超时，非仓库缺陷。

**需要你决定**
- A. 继续用单文件无预算模式补扫剩余目录（成本高、耗时长）。
- B. 剩余目录改由人工/代理精读关键文件（推荐，命中率更高）。
- C. 接受当前覆盖，文档已如实标注。

---

## 回答方式

按 `Q编号: 选项` 回复即可，例如：

```
Q1: A
Q2: A
Q3: A
Q4: A
Q5: A
Q6: A
Q7: A
Q8: A
Q9: A
Q10: B
Q11: A
Q12: B
```

未回答的项我会保持现状并在文档中标注“待决策”，不会擅自改动。
