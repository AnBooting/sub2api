# 需要人工执行与决策的事项（交接清单）

> 本文件只列**代码之外**必须由人来完成的事。代码已全部就绪并通过验收（见 `VERIFICATION.md`），
> 但以下事项**没有人工介入不会生效**，或**必须由有权限的人决策**。
>
> 顺序即推荐执行顺序。标 🔴 的是「不做就会出事」，标 🟡 的是「不做功能不完整」，标 ⚪ 的是「技术债」。

---

## A. 上线前（必做，🔴）

### A-1 🔴 填入 EdgeOne 真实回源网段

**文件**：`deploy/Caddyfile.edgeone`（约第 74 行）

```caddyfile
trusted_proxies static 198.51.100.0/24 2001:db8:beef::/48   # REPLACE_WITH_EDGEONE_ORIGIN_CIDRS
```

**怎么拿**：腾讯云控制台 → EdgeOne → 站点加速 → 该站点的「回源配置 / 源站信息」，或工单索取**回源 IP 段**（不是客户端 IP 段）。

**不做的后果**（不会报错，只会静默出错）：Caddy 不认为请求来自可信代理 → 不采信 `EO-Connecting-IP` / `X-Forwarded-For` → 全站客户端 IP 坍缩成 EdgeOne 回源节点的那几个 IP。表现为：
- 限流把所有用户算成同一个人（容易全站 429）；
- 审计日志里 `client_ip` 恒等于 `remote_ip`；
- 账号维度的「按 IP 限流」完全失效。

**怎么验证填对了**（上线后立刻做，见 `project-docs/08-runbook.md` §9.3）：
```powershell
# 从两个不同出口 IP 各发一次请求，看后端日志的 client_ip 是否不同
# 若两次都是同一个 IP，说明网段填错或 Caddy 没重启
```

### A-2 🔴 决定「两步上线」的节奏，不要一次全上

本轮改动里有一处**会改变全站行为**的开关：`security.trust_forwarded_ip_for_api_key_acl`。

| 步骤 | 上什么 | 风险 |
|---|---|---|
| **第 1 步（可观测，不改判定）** | 账号维度限流、`/redeem` 限流、API 面收敛、N-01/N-02 错误口径、前端降级、部署脚本与文档 | 低。最坏情况是限流偏紧或提示文案变化 |
| **第 2 步（改判定）** | `server.trusted_proxies` 生效 + `trust_forwarded_ip_for_api_key_acl=false` | **高**。若 Caddy/EdgeOne 配置不对，所有客户端 IP 坍缩成一个 → 全站 429 |

**第 2 步的前置条件**（三条全满足才做）：
1. A-1 已完成且验证过（两个出口 IP 得到两个不同 client_ip）；
2. 第 1 步已稳定运行至少一天，没有异常 429；
3. 回滚方案已就绪：把 `SECURITY_TRUST_FORWARDED_IP_FOR_API_KEY_ACL` 改回 `true` 并重启即可（不需要回滚镜像）。

### A-3 🔴 设置两个环境变量（`deploy/.env.example` 已给模板）

```dotenv
# Caddy 后端的真实内网网段，逗号分隔。Docker compose 默认 172.18.0.0/16
SERVER_TRUSTED_PROXIES=172.18.0.0/16

# 第 1 步保持 true，第 2 步改 false
SECURITY_TRUST_FORWARDED_IP_FOR_API_KEY_ACL=true
```

**注意**：`SERVER_TRUSTED_PROXIES` 填的是**Caddy 容器的网段**，不是 EdgeOne 的网段。EdgeOne 的网段填在 Caddyfile 里（A-1），两者是两层信任链。

---

## B. 上线（🔴）

### B-1 🔴 用 `deploy/Caddyfile.edgeone` 替换现有 Caddy 配置

**不要直接改 `deploy/Caddyfile`** —— 那份是「无 CDN、直连」拓扑的版本，仍然保留。EdgeOne 拓扑用 `deploy/Caddyfile.edgeone`。

关键差异（已写在文件注释里）：
- `auto_https off`（TLS 由 EdgeOne 终结）；
- `client_ip_headers X-Forwarded-For EO-Connecting-IP` —— **顺序不能反**。实测：把 `EO-Connecting-IP` 放第一位时，Caddy「取第一个非空头」的语义会让攻击者用一个伪造的 `EO-Connecting-IP` 直接决定 client_ip。
- `trusted_proxies_strict` 开启；
- 剥离 `EO-*` 头再转发给后端，避免后端误采信。

### B-2 🔴 发布后立刻跑一致性校验

```powershell
pwsh -File tools\verify-deployed-build.ps1 -BaseUrl https://sub2.lansn.icu
```

**退出码含义**：
| 码 | 含义 | 处置 |
|---|---|---|
| 0 | 强判据通过（后端 `version` 里的 commit == 本地 HEAD） | 继续 |
| 1 | DRIFT：线上跑的不是你构建的代码 | **回滚**，不要继续 |
| 2 | 前置条件失败（连不上 / 认证失败） | 先排查连通性 |

> 脚本默认把「前端 dist hash 不一致」当 **INFO**，因为同一 commit 在本机重建的入口 hash 与线上不同（构建不可复现）。只有加 `-StrictDistHash` 才会升级成失败。原因见 `VERIFICATION.md` §5.2。

### B-3 🔴 跑限流绕过自检（`project-docs/08-runbook.md` §8 的 5 条）

这是本轮最核心的安全修复，**必须在线确认生效**：

```powershell
# 1) 不带任何转发头，连续 21 次错误登录 → 第 21 次必须是 429
# 2) 固定一个公网 X-Real-IP，连续 21 次 → 必须 429（说明攻击者无法用固定头换桶）
# 3) 每次轮换一个不同的公网 X-Real-IP，连续 25 次 → 必须出现 429（修复前是 25 次全 401、0 次 429）
# 4) 用私网值（如 10.0.0.1）做 X-Real-IP → 必须被忽略
# 5) 同一邮箱从 70 个不同 IP 登录 → 不得因为「请求次数」被 429（只能因失败次数被锁）
```

第 3 条是**回归判据**：如果它变成「25 次全 401、0 次 429」，说明 F-01 修复没生效，立刻停下来查 `SERVER_TRUSTED_PROXIES`。

---

## C. 需要你授权的数据清理（🔴 涉及删数据，我不动手）

外部安全评估在**线上**创建了以下对象，本轮全程**只读未动**。删除需要你的明确授权：

| 对象 | 标识 | 说明 | 建议 |
|---|---|---|---|
| 测试用户 | `uid=5,6,7,8` | 邮箱域 `@example.invalid` | 可删 |
| 测试用户 | `uid=9` | `admin@sub2api.local`，`role=user` | 可删。F-03 已阻断今后注册同域 |
| API Key | `uid=3` 下的 `probe-7255`、`probe-g3` | 攻击者留下的探针 Key | 可删 |
| 兑换码 | `id=1,2,3` | 历史遗留，与本轮无关 | 先确认无用户依赖再删 |
| 用量日志 | 上述 Key 产生的 `usage_logs` | 会影响统计口径 | **建议保留**，仅作标记 |

**不要直接 `DELETE FROM users`** —— 先按后台「用户管理 → 删除」走软删除，确认级联行为符合预期。

---

## D. 需要你决策的技术债（⚪ 下一轮）

按优先级排序，每条都写清了「为什么现在不做」。

### D-1 F-06 未知模型 404 细分 —— **必须单独 PR**

本轮尝试过并**已整体回退**。原因：它的一行调用就能把「缺价」改判成「模型不存在」，导致缺价请求**穿透到上游**（实测 502 + `upstream.callList()` 非空），直接破坏项目红线「缺价不得放行」。

下一轮的硬要求：
1. 路由集合判定与缺价判定**严格分离**；
2. 任何新增错误类型必须先在 `writeError` 的各协议分支注册，否则会兜底 502 并继续走上游；
3. mandatory 回归 = `TestOpenAIChatCompletionsLockedPricingMissingRejectedBeforeUpstream`（既有测试，已用变异验证证明它能抓住这个回归），不需要新写。

### D-2 账号锁定的残余 DoS 风险

现状：失败 5 次锁 60 秒 / 10 次锁 5 分钟 / 20 次锁 15 分钟，成功登录清零。

**残余风险**：攻击者仍可让受害者在锁定期内无法登录（60 秒粒度骚扰）。已消除的是「长期锁定」和「按请求次数锁定」。

**可选改进**（需要产品决策）：锁定期内仍允许用**正确凭据**登录并清零；或把 5 次阈值降为「仅告警不锁定」。

### D-3 secure 路径的空身份

`GetTrustedClientIP` 在 `RemoteAddr` 不可解析时返回空串，而 `GetClientIP` 已回落到 `unknown`。生产可达性低（真实 TCP 一定有 `host:port`），建议下一轮与 `unknownClientIP` 对齐。

### D-4 `injectedVersion` 的两条路径

`/settings/public` 用 `main.Version`（已修，单一事实源），HTML 内联 `__APP_CONFIG__.version` 用同一来源。但**本次修复要下次构建才生效** —— 当前线上 HTML 内联 version 仍是空串，`verify-deployed-build.ps1` 会持续 `[WARN]` 提示，属预期行为。

### D-5 F-05 / F-04 字段收敛 —— 有明确输入，可直接开工

`FIELD-INVENTORY.md` 已备好白名单与硬约束，`HANDOFF-COMPLETE.md` §3.1 是可执行的开工清单。两条**必须遵守**：
1. **不要裁 `dto.Group` 本体** —— 它被 6 处内嵌复用（`APIKey`/`AccountGroup`/`RedeemCode`/`UsageLog`/`UserSubscription`/`Account.groups[]`），裁了会连带影响订阅进度条、用量表分组列、批量生图入口；应新增独立用户面 mapper，只给 `GetAvailableGroups`（`api_key_handler.go:500`）用。
2. **白名单以 `FIELD-INVENTORY.md` §2.5 的 15 个必需字段为准**。注意 2026-10-31 复核纠正了初版的一处误判：`long_context_pricing_enabled`/`image_rate_independent`/`video_rate_independent` 由 `/model-plaza` 的自有白名单下发，`claude_code_only`/`allow_messages_dispatch` 来自 `dto.APIKey.Group`，**都不在 `/groups/available` 的必需集里**；保留它们无害，但不要因为"别的地方也在读"就把它们当成该响应的必需字段。真正的必需集是：`id/name/description/platform/rate_multiplier/subscription_type/status/peak_rate_enabled/peak_start/peak_end/peak_rate_multiplier/deepseek_pricing_state/allow_live/audio_realtime_price_per_min/allow_high_availability`。

### D-6 其余

- `N-03` 空 `messages` 被接受并计费（已定性，未修）；
- `I-02` `/setup/status` 硬编码；
- `R-01` `/redeem` 既有 429 缺 `Retry-After`（既有缺陷，不在本轮写域）；
- `O-01` `PUT /user` 冷路径 5.0s 触发 EdgeOne 522；
- PR #7 前端视觉重设计（CLOSED 未合并，需独立 rebase 到 `develop` 后单开一轮）。

---

## E. 本轮已知的两个「不是缺陷」

1. **`go test -tags=unit ./...` 退出码是 1**：3 条 `TestPgDumper*`（本机 Windows 无 `sh`）+ 1 条满载 flaky（隔离 `-count=3` 通过）。CI 在 Linux 上不会有第一条。
2. **`verify-deployed-build.ps1` 报 dist hash DRIFT**：跑验证脚本会重建 `backend/internal/web/dist/`，而该目录被 gitignore，重建产物与线上构建产物 hash 天然不同。这是脚本的**弱判据**，默认不影响退出码。
