# PR #2 与 develop 的本地冲突处理

检查日期：2026-10-16（Asia/Singapore）。仓库：AnBooting/sub2api。

## 核实结果与范围

- GitHub PR #2：OPEN，`mergeable=CONFLICTING`、`mergeStateStatus=DIRTY`，没有检查任务；这次是分支内容冲突。
- PR 分支：`fix/pricing-review-hardening`，HEAD `8bdd2113fc83d82e8958e7f03a94b9f4b0870903`。
- 共同基线：`6288d0ed45cab527af4e7b337dd666cf3f562d5c`。
- 实际 develop：`5ea1055ea8487a3c7ec4d33e09d0dfb91c623661`；PR 元数据仍报告旧 base SHA，使用 `git ls-remote` 与 fetch 后的分支对象交叉核实。
- develop 有 112 个本分支没有的提交；PR 有 3 个 develop 没有的提交。`git merge-tree` 预演与实际 `merge --no-commit --no-ff` 均定位到下表 17 个冲突文件。
- 开始时工作区干净。只在本地 PR 分支合入上述 develop 快照，保留其已有功能，没有提交、推送、改远端 PR 或部署。合并停留在待提交状态，分支 HEAD 不移动。

## 来源

PR 三个提交为 `354a178d1`（CNY 防护、日历与同步告警）、`89334609e`（首次审查修复）、`8bdd2113f`（请求日历快照与告警解耦）。

develop 在共同基线之后独立修改相同机制，主要来源：

- `0c6d18465`、`e2728ff2d`：另一套 CNY 校验、DeepSeek 时段策略和年度日历控制；与 PR 的手工价独立、缺年禁峰价规则存在语义差异。
- `3e0d0da4a`、`69638aba1`、`c977cd8f9`、`87906721a`、`bca4734de`：完整目录、分组、映射锁价及各网关消费者；与 PR 对相同请求时间、预留、结算位置的修改重叠。
- `42929b7aa`、`97ce76944`：WebSocket 每轮锁价和上游模型配对；与 PR 的每轮日历快照重叠。
- `463040f21`、`686cddc44`：年度日历编辑器、设置页测试；与 PR 列表编辑器、TOTP 草稿处理重叠。
- `ae162e079`、`4b790ec2b`：同步周期热更新和状态读取锁顺序；与 PR 日历覆盖状态扩展重叠。
- `c2c2f6b64` 及后续计费修复：v2 用量快照。解决冲突时保留其分项复算、倍率及媒体计费逻辑。

## 逐文件处理

| 冲突文件 | 处理 |
|---|---|
| backend/internal/handler/admin/pricing_controls_handler_test.go | 保留列表接口验证，以及年度字段校验、省略保留和显式清空测试，两套测试独立命名。 |
| backend/internal/handler/openai_gateway_handler.go | 使用 develop 每轮 execution/context 槽位，同时让普通与风控结算沿用该轮时间和日历。 |
| backend/internal/service/billing_context_schedule.go | 使用统一标准价探针助手，去掉重复克隆；展示价不叠加渠道 time_pricing，并注入显示取价时间。 |
| backend/internal/service/billing_inflight_reservation.go | 合并 PricingAt、compact 和 WS ingress 模型标志；保留两侧缺价错误及原币校验。 |
| backend/internal/service/billing_service.go | 保留分项快照与完整性检查；官方价用冻结日历，手工售价不自动叠加官方峰价。 |
| backend/internal/service/gateway_usage_billing.go | 保留完整锁价消费者；无 bundle 时使用 PR 的请求时点及日历路径。 |
| backend/internal/service/model_pricing_resolver.go | 兼容固定 resolver 时间、显式 PricingAt 和上下文时间；手工 CNY 四项缺价拒绝，跨币种不继承。 |
| backend/internal/service/openai_gateway_usage.go | 保留冻结账号事实及完整报价消费者；旧路径沿用请求 PricingAt。 |
| backend/internal/service/pricing_controls.go | 同步周期、年度映射和覆盖/错误状态并存；两接口统一持久化和原子发布，数据库写入不持计费读取锁。 |
| backend/internal/service/pricing_native_cny.go | 官方绝对峰价窗口与完整基础价回落保留；用同一不可变日历排除假日、周末及未知年份，峰价只应用一次。 |
| frontend/src/api/admin/pricing.ts | 年度映射与同步周期类型保留，加入 PR 的 USD 错误及日历覆盖字段。 |
| frontend/src/components/admin/PricingSettingsPanel.vue | 保留年度编辑器、同步周期、轮询竞态保护与草稿；日历保存接入 TOTP，去掉重复列表编辑器。 |
| frontend/src/components/admin/__tests__/PricingSettingsPanel.spec.ts | 合并 fixture；TOTP、服务器拒绝后草稿、年度 JSON、迟到响应与周期配置均保留覆盖。 |
| frontend/src/i18n/locales/en/admin/settings.ts | 合并周期和日历文案，与内置回落、缺年禁峰价及手工价规则一致。 |
| frontend/src/i18n/locales/zh/admin/settings.ts | 与英文键同步，明确年度空数组和空对象的不同语义。 |
| frontend/src/views/admin/__tests__/SettingsView.spec.ts | 保留真实价格面板挂载，继续验证设置页实际集成。 |
| project-docs/prd/13-接口文档.md | 合并 develop 新设置字段与 PR 日历接口，说明共享持久化、兼容格式、step-up 与审计。 |

## 跨文件语义处理

- 以 PRD-05 §2.11 为准：手工 Token 价卡是运营者售价；官方峰谷不叠加。一次自动填价保存当前时窗快照，之后按手工售价处理。
- 年度映射为主写入格式；旧日期数组只在新字段缺失时迁移。兼容列表写入同一年度结构，保留当前开关与同步周期。
- 年度 `[]` 表示明确维护无假日；缺年使用内置 2026 年表，双方都缺年时禁用官方峰价。年度 `{}` 清除自定义覆盖并恢复内置回落；兼容列表 `[]` 恢复内置日期。
- 日历保存先持久化，再发布新不可变对象。独立写锁串行化两入口，数据库等待期间不占用 controls/calendar 计费读取锁。
- 完整请求 bundle 拷贝日历并将其纳入 price_version；显式 PricingAt 传入完整锁价路径。WS 下一轮重新取时，新 bundle 后的辅助调用沿用其冻结日历。
- 年度日历不能经通用 settings PUT 绕过 PR 的 step-up。普通开关和周期保存维持既有认证流程，日历审计包含日期和年度映射。
- PR 的缺价拒绝、显式免费项、告警脱敏、原子缓存、抑制重试和告警 I/O 与查价分离继续保留。

## 验证记录

| 检查 | 结果 |
|---|---|
| 定价、日历、锁价、周期与并发专项 | 最终定向 service 回归通过（12.521s），包括新增 PR2Merge 交叉测试。 |
| 前端 typecheck | pnpm 9，退出码 0。 |
| 前端完整 test:run | 379 个测试文件、2993 项测试通过。 |
| 前端生产 build | i18n、vue-tsc -b 与 Vite 均成功；仅已有 Browserslist/包体积提示。嵌套 pnpm 命令也使用 9，临时 shim 在收尾删除。 |
| Wire | go generate ./cmd/server 成功；生成前后 SHA256 同为 A2B135C4B1DBA5BA7ECA68B255CAA48F08E40A3B2929A33BDADB5A4E0B03FEB6。 |
| 后端 embed 构建 | go build -p 1 -tags=embed 成功，使用本次前端 dist；验证程序 SHA256 为 DD2A6BC5AE1140DE5CED843E6B058832E115F86FE3CCDCE9A906208B077122BA，收尾删除。 |
| 完整后端 unit | 首轮 59 个包通过，service 唯一失败为 develop 新增的手工价翻倍展示断言；已按 PRD-05 §2.11 对齐该断言，最终 service 全量复跑通过（213.395s），未跳过测试；其余 59 个包沿用同次全量通过结果。本次没有沿用旧记忆中的 Ollama 失败结论。 |
| 最终 handler/admin/routes 集成回归 | Pricing、Inflight、Cyber、Locked、RequestBilling 相关测试通过：handler 19.614s、admin 0.675s、routes 0.480s。 |
| Git | 未合并条目为 0、冲突标记为 0；相对 develop 的 diff --check 通过。HEAD 与远端 PR head 仍为 8bdd2113f。 |

新增测试先复现了通用 settings 日历写入绕过 step-up（预期 403、实际 200）及完整 bundle 丢失显式 PricingAt，再修复实现。修复后临时绕过 step-up 的变异验证再次出现预期 403/实际 200 的失败，随后按原字节恢复生产文件（SHA256 相同），恢复后同一测试通过（0.488s）。定向回归同时覆盖完整 bundle 的日历版本、新旧请求、每轮重冻、旧格式迁移、年度空数组、长年度日历和 USD 校验兼容。

本次仅本地验证，没有真实数据库/SMTP/HTTP 端到端或部署。GitHub PR 因未提交、未推送，仍显示 CONFLICTING/DIRTY；这不代表本地仍有未解决冲突。
