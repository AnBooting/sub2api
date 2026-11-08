# 独立对抗性审查报告｜fix/security-remediation-20261030

审查者：reviewer（只读）。审查对象：sec-ip 与 api-surface 的 diff（含后续 deploy-ops / frontend 的关联改动）。
基线：develop = f9d8bc855。审查方法：逐文件读 diff + 对抗性反例脚本 + 6 点变异验证 + develop 纯净基线对照。

## 一、总体裁决

**有条件合入。** 合入前仅 1 项必修（契约快照未同步）。

## 二、合入前必修

### M-1｜MEDIUM｜TestAPIContracts 红（本轮引入）
- 文件：`backend/internal/server/api_contract_test.go`（期望快照 L838 后、L1196 后各需补一行）
- 现象：I-01 在 `dto.Settings` 新增 `SMTPConfigured bool `json:"smtp_configured"``，但契约快照未同步；
  测试输出 Diff 显示 data map len 310→311，唯一差异 `"smtp_configured": (bool) false`。
- 影响 2 个子用例：`GET /api/v1/admin/settings`、`..._falls_back_to_config_oauth_defaults`。
- 修法：两处 `"site_subtitle": ...` 行后插入 `"smtp_configured": false,`。

## 三、必须记住的两个红线事件（均已修复，但要写进交付教训）

### R-1｜CRITICAL｜缺价红线曾被实质破坏（一行改动）
- 致红：`refinePricingRejection`（F-06）把 `ErrModelPricingUnavailable` 改判为 `ErrModelNotFoundForGroup`。
  该错误不在 `writeError` 已知分支 → `billingErrorDetails` 兜底 502，且调用链继续走上游。
- 实测：`TestOpenAIChatCompletionsLockedPricingMissingRejectedBeforeUpstream` 5 子用例 expected 503 / actual **502 upstream_error**，上游 `callList()` 非空 → **请求已打到上游**。
- 归因证据链：
  1. 从 develop 取纯净副本（git archive）跑同测试 → **ok**；当前分支 → FAIL ⇒ 本轮引入。
  2. 副本内只注释掉那一行 refine 调用 → **ok** ⇒ 归因为该行。
- 现状：该函数已整体移除（全 backend `func refinePricingRejection` 定义数=0），测试 5/5 通过。
- 教训：**分支级探针不能替代端到端断言**。我给 F-06 写的 7 条分支探针全绿，但这 1 条端到端测试立刻抓住。
- 下一轮 F-06 护栏：直接用既有 `TestOpenAIChatCompletionsLockedPricingMissingRejectedBeforeUpstream`（我已用变异 6 证明它能抓住该回归）。

### R-2｜HIGH｜整文件覆盖导致 2 个既有函数被删（编译红）
- 文件：`backend/internal/handler/gateway_locked_billing_helper.go`
- 被删：`gatewayLockedBilling.apply`、`validateSharedGeminiImagePricing`（develop 里有，工作树里没有）→ `go build ./... = 1`。
- 已按行插回；当前 diff 为纯新增，两函数定义回到 L304 / L313，build=0。
- 触碰原因：多文件改动分批落盘 + 未先读基线全文。
- 防复发：改共享文件前先 `git show develop:<path>` 取基线；多文件一次写完；跨包先落被依赖符号；任何时刻 `go build ./...` 必须为 0。

## 四、六条必答裁决（摘要）

| # | 问题 | 裁决 |
|---|---|---|
| 1 | 转发头信任能否被 IPv6 映射/端口/逗号链/重复头/大小写绕过 | **修复前全部可绕过**（我 6 条反例全红）。修复后对端维度闭合；但**受信代理之后**仍可被客户端自带的 `CF-Connecting-IP` 与 XFF 链首绕过（我实测 5 次请求得 5 个身份）→ 已作为 C-1 上报并修复（XFF 改右→左、CF-Connecting-IP 不再默认读取） |
| 2 | 按邮箱锁账号是否成为新 DoS 面 | **是，且严重**。修复前实测：攻击者用受害者邮箱发 3 次（limit=3）即锁死受害者 60s。已改为「只对失败计数 + 分级窗口 + 成功清零」，阈值 5/1min→10/5min→20/15min。**残余风险**：锁定期内受害者无法登录（无法自行清零），且 `accountFailureLockThreshold=5` 偏低；建议下一轮评估「成功尝试不预检锁」或降级为仅告警 |
| 3 | PrereadBody 回填后下游读体是否正常 | **正常**。我 5 条探针全过：完整体回读、中间件读+回填+下游再读、multipart 流式（1MB）、8MB 大 body、gzip 压缩体（无二次解压） |
| 4 | 字段收敛是否漏掉前端实际读取字段 | **有漏**。`allow_high_availability`（KeyHAGroupPicker）、`audio_realtime_price_per_min`（GroupLiveBadge）、`allow_messages_dispatch`、`claude_code_only`、`long_context_pricing_enabled`、`image_rate_independent`、`video_rate_independent` 均被用户端真实读取，**不在 task-2 的收敛白名单内**。已上报 |
| 5 | 未知模型 404 是否破坏「缺价不得放行」 | **是，曾破坏**（见 R-1）。当前实现已回退（F-06 本轮不交付）。设计层评估：`refinePricingRejection` 的 7 条分支逻辑本身正确（含 composite / model_routing / allowlist / 诊断器 nil 的 fail-safe），但**它插在缺价路径上，改判错误类型即等于放行** |
| 6 | 新增测试能否真锁住 bug | **能，已验证 6 点**（见第五节）。但发现测试盲区：分支探针不覆盖端到端 |

## 五、变异验证 6 点（全部有效）

| # | 回退了什么 | 变红的测试 |
|---|---|---|
| 1 | `IsTrustedDirectPeer` → 恒 true | ip 包 8 用例 + `TestConfigureClientIPTrustWiresTrustedProxyPolicy` |
| 2 | XFF 右→左 → 改回左→右 | `TestTrustedPeerPrefersProxyAppendedForwardedForTail`（203.0.113.7 → 1.1.1.1）+ invalid_legacy_values |
| 3 | `isSub2APIInternalEmail` → false | `TestIsReservedEmail_Sub2APIInternalDomain` |
| 4 | 失败计数查锁 → locked=false | `TestAccountFailureMiddlewareAbortsWith429`（429 → 200） |
| 5 | F-06 去掉 `HasAccountsInPool` 保护 | `TestProbeF06_NoAccountsStays503`（容量问题被误判 404） |
| 6 | 把 `refinePricingRejection` 重新加回（一行注入） | `TestOpenAIChatCompletionsLockedPricingMissingRejectedBeforeUpstream` 5/5 红（503 → 502 upstream_error） |

## 六、可留到下一轮

- **F-06**：本轮不交付，下一轮单独 PR。mandatory 回归 = 既有端到端测试（变异 6 已证明有效）。
- **secure 路径空身份**（MEDIUM）：`GetTrustedClientIP` 在 `RemoteAddr` 不可解析时返回空串（`GetClientIP` 已回落 `unknownClientIP`，但 secure 路径未修）。实测：`getTrusted=[]  getSecurityFalse=[]  getClientIP=[unknown]`。生产可达性低（真实 TCP 总有 host:port）。
- **injectedVersion 优先级不一致**（MEDIUM）：`/settings/public` 用 `main.Version`（handler/setting_handler.go:100 + handler/wire.go:175），HTML 用 `serviceBuildVersion`（service/setting_public.go:741）；`SetVersion` **零调用点**；`backend/Makefile:4` 与 `deploy/Makefile:4` 未注入 `serviceBuildVersion`。线上实测：`/settings/public` = `0.2.17-fork.f9d8bc855`，HTML `__APP_CONFIG__.version` = `[]`（空）。
- **SetTrustedProxyPolicy 错误被吞**（LOW）：`http.go` 用 `_ = ippkg.SetTrustedProxyPolicy(...)`，配置写错（如 `localhost`）时静默全站不信任转发头。
- **nil 语义混淆**（LOW）：`TrustedProxyPolicy()` 的 nil 同时表示「显式空列表」与「解析失败」，行为一致（fail-closed）但无可观测性。
- **两套策略组合不一致**（MEDIUM，可用性陷阱）：ip 包受信网段只在启动期设置；运行期改 `trust_forwarded` setting 不会同步。setting 开启但 `trusted_proxies` 仅 loopback、且前面是 CDN 直连时 → 全站 IP 坍缩 → 全站 429。
- **Caddy strict 不删左侧条目**（文档）：XFF 在前之所以安全完全依赖 EO 追加 XFF；strict 只影响从右往左挑选，不删除客户端自带的更左侧条目。需 runbook 给可执行验证命令。
- **N-02 的 4 处死代码**：`GetUserSpendingRanking` / `GetBatchUsersUsage` / `GetBatchAPIKeysUsage` / `GetUserBreakdown` 在 repo 层无 completeness 守卫，因此那些 `respondUsageStatsIncomplete` 永不触发。已按我建议删除（测试 cases 同步删除）。

## 七、红线合规结论

| 红线 | 结论 |
|---|---|
| 缺价不得放行 | **本轮曾实质违反，已修复**（R-1）。当前 5/5 通过，develop 基线与当前一致 |
| 不整文件覆盖 | **本轮发生 1 次，已修复**（R-2）。当前 diff 纯新增，build=0 |
| 不改写已存在迁移 | 未触碰（`git status backend/migrations/` 为空） |
| 不提交凭据 | 无（`.env.example` 是模板且为新增示例行） |
| 不在 main 提交 | 未发生（`develop..HEAD = 0`，本分支尚无提交） |

## 八、审查产物位置

- 反例/探针脚本：`F:\mut-verify-reviewer\`（独立副本，不污染主工作区）
- develop 纯净基线副本：`F:\mut-baseline-develop\`
- 本报告：`F:\sub2\.deployprobe\review\FINAL_VERDICT.md`