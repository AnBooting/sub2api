# 交付小结与下一轮交接（2026-10-30）

本文件是 `PLAN.md` / `EVIDENCE.md` / `FIELD-INVENTORY.md` / `E2E-REPORT.md` / `VERIFICATION.md` 的入口摘要。**交付前的最终数字以 `VERIFICATION.md` 为准**（含 Lead 与 verifier 双方实跑）。

- **分支**：`fix/security-remediation-20261030`
- **PR**：https://github.com/AnBooting/sub2api/pull/18 （base `develop`，10 个提交，99 文件 +14724/−116）
- **需要人做的事**：见 `ACTION-ITEMS.md`（EdgeOne 网段、两步上线、上线后自检、数据清理授权、下一轮技术债）

## 1. 本轮做了什么

| 编号 | 项 | 状态 | 归属 |
|---|---|---|---|
| F-01 | 转发头信任模型反转 + 限流账号维度 + 账号锁死治理 | **已修复** | sec-ip |
| F-02 | `/redeem` 独立限流（原判「无限流」已更正为「有 30/10min 阈值」） | **已修复**（另加 10/10min 一层） | limiter |
| F-03 | `admin@sub2api.local` 整域保留 | **已修复** | api-surface |
| F-06 | 未知模型 404 细分 | **本轮不交付**（尝试后被证明可破坏缺价红线，已整体回退） | api-surface + reviewer |
| F-07 | `/usage/:id` 越权与不存在同码同体 | **已修复** | api-surface |
| N-01 | 未定价模型 503 的 `message` 从 `internal error` 改为可行动文案 | **已修复** | api-surface |
| N-02 | 统计未对账被映射成 500 / 静默空数组 → 统一 503 + `usage_stats_incomplete` | **已修复** | api-surface |
| I-01 | `send-verify-code` 假成功 | **部分**（前端降级已完成；后端 `smtp_configured` 下发见 §3） | api-surface + frontend |
| F-04 / F-05 | 监控配置与分组字段收敛 | **未做，推下一轮**（前端已备好白名单与降级骨架） | api-surface + frontend |
| I-02 | `/setup/status` 硬编码 | **未做，推下一轮** | — |
| N-03 | 空 `messages` 被接受并计费 | **未做，推下一轮**（已定性） | — |
| D-01 | 本地内嵌 dist 陈旧导致误判部署 | **已修复**（文档 + 校验脚本） | deploy-ops |
| D-02 | 内联 `__APP_CONFIG__.version` 空串 | **已修复**（版本单一事实源，需下次构建生效） | deploy-ops |
| D-03 | CDN 拓扑下 IP 坍缩 | **已修复**（`Caddyfile.edgeone` + compose 变量 + runbook） | deploy-ops |
| R-01 | `/redeem` 既有 429 缺 `Retry-After` | **已知未修**（既有缺陷，不在本轮写域） | — |
| P3-02 | `/admin/security-audit` 缺 redirect、`/admin/payment/channels` 死代码 | **已修复** | frontend |
| O-01 | `PUT /user` 冷路径 5s 触发 522 | **观察项，未处理** | — |

## 2. 三条必须记住的教训

### 2.1 一次「一行改动破坏最高红线」

F-06 的 `refinePricingRejection` 只用**一行调用**就把缺价 `ErrModelPricingUnavailable` 改判成 `ErrModelNotFoundForGroup`，落入 `writeError` 未知分支 → 兜底 502 且**请求穿透到上游**。当时已有的 7 条分支级探针**全部通过**。

结论：**分支级探针不能替代端到端断言**。下一轮任何触碰缺价路径的改动，必须先跑 `TestOpenAIChatCompletionsLockedPricingMissingRejectedBeforeUpstream` 这类端到端测试（断言 503 + `upstream.callList()` 为空 + `ledger.commands` 为空）。

### 2.2 Lead 的判定也会错，必须给抽样设计阈值前提

- F-02：我抽 25 次全 404 就写成「无限流」，实际阈值是 30 次/10 分钟 —— **判定「有无限流」必须先读阈值常量再设计抽样**。
- `TestLogCodexCLIOnlyDetection_RejectedIncludesRequestDetails`：我先判「既有失败」，verifier 用 A/B/C 三组对照（只换回 `ip.go` 就绿）证明是本轮修复导致、且该测试把旧的可伪造语义固定住了。**「既有失败」的判定必须做基线对照**，不能靠「文件没被改过」推断。

### 2.3 多文件改动的中间态会互相阻塞

本轮发生 4 次主工作区编译红，全部是「引用先于定义落盘」：`RecordAccountOutcome`、`writeModelNotRoutableInsteadOfPricing`、`gatewayLockedBillingPricingDiagnoser`、`gatewayLockedBilling.apply`（后者是整文件覆盖导致既有函数被删）。

规则（已写入团队纪律）：多文件改动一次写完再落盘；跨包改动先落被依赖符号；**改共享文件前先 `git show develop:<path>` 取基线全文，只按行插入，不整文件覆盖**（项目明文红线）。

## 3. 部署前必须做的三件事

1. **先取 EdgeOne 回源网段**并替换 `deploy/Caddyfile.edgeone` 里的 `REPLACE_WITH_EDGEONE_ORIGIN_CIDRS`；不替换不会报错，只会让全站客户端 IP 坍缩成一个（症状：日志里 `client_ip` 恒等于 `remote_ip`）。
2. **首次上线只开「可观测」那一半**（账号维度限流、`/redeem` 限流、API 面收敛、文档与工具），把 `server.trusted_proxies` + `trust_forwarded_ip_for_api_key_acl=false` 的信任模型切换留到 Caddy 配置确认后（见 `PLAN.md` §4 的两步上线）。
3. **发布后立刻跑** `pwsh -File tools\verify-deployed-build.ps1`，退出码非 0 即回滚；再跑 `project-docs/08-runbook.md` 第 8 节的 5 条限流绕过自检。

## 4. 下一轮建议顺序（按风险/收益）

1. **F-06 单独 PR**：路由集合判定与缺价判定严格分离；mandatory 回归 = 5 种缺价形态 ×（503 + 上游零调用 + ledger 零命令）；附 reviewer 的失败 fixture。
2. **I-01 后半**：SMTP 未配置时 `SendVerifyCodeAsync` 返回 `EMAIL_NOT_CONFIGURED`。
3. **F-05 / F-04 字段收敛**：开工看 `NEXT-ROUND-HANDOFF.md` §2，字段依据看 `FIELD-INVENTORY.md` §2.5（新增用户面 mapper，**不要裁 `dto.Group` 本体**，它被 6 处内嵌复用）。
4. **N-03 空 messages**、**I-02 setup/status**、**R-01 Retry-After**、**P3-01 分页参数校验**、**O-01 PUT /user 冷路径**。
5. **PR #7 前端视觉重设计**：独立 rebase 到 `develop` 后单开一轮（本轮未纳入）。

## 5. 数据清理待授权

外部评估报告在线上创建的对象，本轮**只读未动**：

- 用户 `uid=5,6,7,8`（`@example.invalid`）、`uid=9`（`admin@sub2api.local`，已由 F-03 阻断今后注册同域）；
- `uid=3` 下攻击者留下的 API Key `probe-7255`、`probe-g3`；
- 兑换码 `id=1/2/3`（历史遗留）。

删除这些需要你授权（后台删用户/删 Key），本轮未执行。
## 6. 冻结快照的最终数字（Lead 实跑，21:38–21:45，工作树自 21:35 冻结）

| 检查 | 命令 | 结果 |
|---|---|---|
| 后端构建 | `go build -o bin/server.exe ./cmd/server` | **0** |
| 后端单测 | `go test -tags=unit -count=1 ./...` | **1**（仅 2 类红，均非本轮引入，见下） |
| 前端类型 | `vue-tsc -b` | **0** |
| 前端单测 | `vitest run` | **0**（435 files / 3456 tests 全过） |
| i18n 门禁 | `check:i18n` | **0**（3 passed） |

`./...` 的两类红：
1. `internal/repository` 的 3 条 `TestPgDumper*` —— 本机 Windows 无 `sh`（**既有环境性失败**）。
2. `internal/service` 的 `TestInflightEstimate_AccountMappingNoDBAndBoundedMemory` —— AGENTS.md 明文记录的**已知 flaky**（满载偶发）；隔离 `-count=3` → ok, EXIT=0。

其余全部 ok，包含此前红过并已修复的：`internal/handler`（缺价红线端到端 5/5）、`internal/handler/admin`、`internal/server`（`TestAPIContracts`）、`internal/server/routes`、`internal/server/middleware`、`internal/middleware`、`internal/pkg/ip`。

## 7. 文档索引（同目录）

| 文件 | 内容 |
|---|---|
| `PLAN.md` | 整改计划（缺陷清单、方案、验收标准、两步上线顺序） |
| `EVIDENCE.md` | 线上实测证据（含 F-02 更正、新增发现 N-01/N-02/N-03/R-01/O-01/D-01..D-03） |
| `VERIFICATION.md` | 验证记录（命令、退出码、通过数、变异验证、既有失败口径） |
| `FIELD-INVENTORY.md` | **字段消费清单与收敛交付说明**（F-05/F-04 的唯一实施依据：字段表 + 白名单 + 硬约束 + 实施步骤 + 验收 + 回滚） |
| `NEXT-ROUND-HANDOFF.md` | **下一轮交接（压缩版）**：F-05/F-04/F-06/I-01b/N-03/I-02/R-01/P3-01/O-01 每项的改哪里、必过的测试、已知可接受失败、工作纪律 |
| `E2E-REPORT.md` | 17 页基线 E2E、改动清单、方法论、未完成项 |
| `REVIEW-FINAL-VERDICT.md` | 独立审查最终裁决（6 条必答、6 点变异、红线合规结论） |
| `QA-FINAL-REPORT.json` / `QA-CONSOLIDATED.json` | 线上功能矩阵与受控探测的原始结论（含扣费一致性、幂等、限流） |

> 线上截图与逐请求 JSON 留痕在 `.deployprobe/`（未纳入版本控制，可复现）；本目录只保留结论性文档。
