# 下一轮交接（压缩版）

> 给**接手的人**用，不需要读之前的对话。本轮（PR #18）已交付并验证的内容见 `HANDOFF.md`；
> 本文件只讲**还没做完的事**：做什么、改哪个文件、判据是什么、做不到会怎样。
>
> 阅读顺序：§1 一页总览 → 挑一项 → 该项的"实施"与"必过的测试" → §6 通用验收命令。

## 1. 一页总览

| 编号 | 事 | 量级 | 风险 | 独立 PR？ | 输入是否就绪 |
|---|---|---|---|---|---|
| **F-05** | `/groups/available` 字段收敛到 15 个 | 中（后端 2 文件 + 前端 6 文件） | 中：漏字段→前端 undefined | 可合 | ✅ `FIELD-INVENTORY.md` §2/§4 |
| **F-04** | `/channel-monitor-v2` 用户域 config 只剩 `refresh_interval_seconds` | 中 | 低 | 可与 F-05 同 PR | ✅ 同文件 §3/§4 |
| **F-06** | 未知模型 404 细分 | 小改动 / 大风险 | **高：一行就能破坏缺价红线** | **必须单独 PR** | ✅ `ACTION-ITEMS.md` D-1 |
| **I-01b** | SMTP 未配置时发码返回 `EMAIL_NOT_CONFIGURED` | 小 | 低 | 可合 | ✅ 见 §3.1 |
| **N-03** | `messages: []` 应 400，现被接受并计费 | 小 | 低但触及计费路径 | 建议单独 | ✅ 见 §3.2 |
| **I-02** | `/setup/status` 硬编码 | 小 | 低 | 可合 | ✅ 见 §3.3 |
| **R-01** | service 层 429 缺 `Retry-After` | 极小 | 低 | 可合 | ✅ 见 §3.4 |
| **P3-01** | 分页参数越界静默钳制 | 小 | 低（改契约需同步前端） | 可合 | ✅ 见 §3.5 |
| **O-01** | `PUT /user` 冷路径 5.0s→522 | 待定位 | 中（性能） | 单独 | ⚠️ 仅现象，未定位 |
| **PR #7** | 前端视觉重设计（CLOSED） | 大（跨 105 文件） | 视觉回归 | 独立一轮 | ⚠️ 需先 rebase 到 `develop` |
| **人工** | 上线三步 + 数据清理授权 | — | 🔴 | — | ✅ `ACTION-ITEMS.md` A/B/C |

推荐顺序：**F-05+F-04 一个 PR → F-06 单独 PR → I-01b/N-03/R-01/I-02/P3-01 一个 PR → O-01 单独 → PR #7 最后**。
理由：前两项有完整输入且收益最确定；F-06 风险最高必须隔离；杂项打包减少评审轮次。

## 2. F-05 / F-04 字段收敛

**完整实施依据：`FIELD-INVENTORY.md`（同目录）。本节只列不得违反的硬约束，细节去读那份。**

### 2.1 三条硬约束

1. **不裁 `dto.Group` 本体**（`backend/internal/handler/dto/types.go:107`）。它经 `GroupFromServiceShallow`（`dto/mappers.go:130`）被 6 处内嵌复用：`APIKey`(:92)、`AccountGroup`(:464)、`RedeemCode`(:556)、`UsageLog`(:705)、`UserSubscription`(:809)、`Account.groups[]`(mappers.go:492)。裁了会同时坏订阅进度条分母、用量表分组列、批量生图入口、Claude Code 入口。
2. **新增用户面 mapper，只接 `api_key_handler.go:500` 一处**（`GetAvailableGroups`，路由 `routes/user.go:93`）。
3. **`Localized(Accept-Language)` 必须仍在用户面路径上**（`dto/group_localize.go:8`）：否则用户侧文案不本地化且多出 `i18n` 字段。

### 2.2 白名单（15 必需 + 3 可选）

```text
id, name, description, platform, rate_multiplier, subscription_type, status,
peak_rate_enabled, peak_start, peak_end, peak_rate_multiplier,
deepseek_pricing_state, allow_live, audio_realtime_price_per_min, allow_high_availability
可选保留: is_exclusive, allow_image_generation, allow_batch_image_generation
```

**极易踩的认知陷阱**（本轮复核纠正，务必照抄）：`long_context_pricing_enabled`、`image_rate_independent`、`video_rate_independent` **不在本响应里需要**——它们由 `/model-plaza` 的自有白名单 `modelPlazaGroup`（`model_plaza_handler.go:81`）下发；`claude_code_only`/`allow_messages_dispatch` 来自 `dto.APIKey.Group`，也不在本响应。判定字段是否必需，必须同时满足：① 读取点的类型是 `Group`；② 该 `Group` 来自 `getAvailable()`。

### 2.3 必过的测试

| 断言 | 形态 |
|---|---|
| 响应元素 JSON key 集合 ⊇ 15 个必需字段 | 新增单测或改写 `api_contract_test.go:318` 的 `wantJSON` |
| 响应**不含** `rpm_limit` / `max_reasoning_effort` / `image_price_1k` / `daily_limit_usd` / `claude_code_only` / `created_at` / `i18n` | 同上 |
| 用户域 snapshot `config` 的 key 集合 **==** `["refresh_interval_seconds"]` | 新增 |
| 管理域 snapshot `config` 仍含 `version/enabled/platforms/group_ids/health_thresholds/ignored_error_categories` | 新增 |
| 变异 1：删掉 `allow_high_availability` → key 集合断言必须红 | 手工做一次，记录 |
| 变异 2：换回 `GroupFromService` → "不含 rpm_limit" 断言必须红 | 手工做一次，记录 |

### 2.4 F-04 的实现选型（别改老结构）

`ChannelMonitorV2Snapshot`（`service/channel_monitor_v2.go:211`）是单结构体，直接改它的 `Config` 类型会波及 `repository/channel_monitor_v2_repo_test.go:239`（断言 `snapshot.Config == service.ChannelMonitorV2Config{}`）。
做法：新增 `ChannelMonitorV2PublicConfig{ RefreshIntervalSeconds int }` + `ChannelMonitorV2PublicSnapshot` + `SnapshotPublic()`；`handler` 的 `snapshot(c,false)`（`channel_monitor_v2_handler.go:102`）改调它，`admin=true` 走原 `Snapshot`。随后删 `redactChannelMonitorV2PublicConfig`（`service:621`）与 `redactChannelMonitorV2Snapshot` 里的 config 行；**metric 脱敏（:637）保留**。

安全性依据：`errors[].ignored` 由服务端查询时算出（`repository/channel_monitor_v2_repo.go:729-730`），不依赖响应里的 `config`，因此剔除不会让 UI 置灰失效。

## 3. 其余技术债（每条自包含）

### 3.1 I-01b：发码真实性（后端补完）

- 现状：`service/auth_service.go:345` `SendVerifyCodeAsync` 只判 `emailQueueService == nil`（:372），入队失败也只 `fmt.Errorf`（:385）；SMTP 未配置时前端已能阻断（本轮完成），后端仍会"假成功"。
- 改：在 :371 之前插入 `EmailService.IsConfigured` 检查，未配置直接返回 `service.ErrEmailNotConfigured`（已存在：`service/email_service.go:27`，`ServiceUnavailable("EMAIL_NOT_CONFIGURED", …)`）。同步 `SendVerifyCode`（:341 调用点）。
- 必过测试：新增 `TestSendVerifyCodeAsync_ReturnsEmailNotConfigured_WhenSMTPMissing`，断言 `errors.Is(err, service.ErrEmailNotConfigured)` **且** 队列`EnqueueVerifyCode` 未被调用（用 spy）。
- 变异：把检查删掉，测试必须红。

### 3.2 N-03：空 `messages` 必须 400

- 现状：`handler/gateway_handler_chat_completions.go:23` `ChatCompletions` 只在 :59 判空 body、:75 判 `model`，**没有** `messages` 非空校验 → 空数组被转发上游并按 prompt_tokens≈35 计费（qa-live 复现 4 次）。
- 改：在 `model` 校验之后加 `messages` 非空且为数组的检查，返回 `400 invalid_request_error`，文案 `messages is required`。同一份校验要覆盖 `/v1/messages`（Anthropic 口径，`gateway_handler.go:126`）与 `/v1/responses` 对应入口，否则只堵一半。
- 必过测试：各入口各写一条，断言 **400 + `upstream.callList()` 为空 + `ledger.commands` 为空**（与缺价红线同一套断言风格）。
- **不要**只在前端拦：这是网关契约。

### 3.3 I-02：`/setup/status` 硬编码

- 现状：`server/routes/common.go:23-31` 恒返回 `needs_setup:false, step:"completed"`。
- 改：读真实初始化状态（setting 表是否已有站点配置）。**注意**：这是匿名端点，且被 `middleware/logger.go:49` 排除出日志，改动后要确认不泄露内部状态（只暴露布尔 + 阶段枚举）。
- 必过测试：未初始化库 → `needs_setup:true`；已初始化 → `false`。

### 3.4 R-01：service 层 429 补 `Retry-After`

- 现状：`service/redeem_service.go:23` `ErrRedeemRateLimited = infraerrors.TooManyRequests(...)`，:347 返回它；该错误经 `response.ErrorFrom` 输出时**没有** `Retry-After` 头。本轮新增的中间件层（`server/middleware/panel_rate_limit.go:238` `RedeemGuard`）会写该头，因此线上多数情况已被覆盖，但 service 层这条仍是既有缺陷。
- 改：二选一——① 让 429 错误携带 retry-after 元数据并在写响应处统一落头；② 在 `Redeem` handler 里显式 `c.Header("Retry-After", …)`。优先 ①（可复用，其他 429 同样受益）。
- 必过测试：命中限额后断言 `Retry-After` 非空且为正整数。

### 3.5 P3-01：分页参数越界静默钳制

- 现状：`pkg/response/response.go:170` `ParsePagination` 对 `page/page_size` 的非法值静默回落/钳制（92 处调用）。
- 改的前提：**这是契约变更**，会改变 92 个端点的行为。建议只在**明显越界**（`page<=0`、`page_size<=0`、`page_size>上限`）时报 400，其余保持宽松；上线前同步前端调用点，否则会把正常页面打挂。
- 不建议本轮做，除非能一次改完并跑完全量用例。

### 3.6 O-01：`PUT /user` 冷路径 5.0s

- 现象：`handler/user_handler.go:163` `UpdateProfile`，冷启动首次请求 5.0s，命中 EdgeOne 回源超时（522），重试即 200。
- **尚未定位根因**。下一步取证（不是猜）：① 冷路径打点（各 DB 往返耗时）；② 是否存在连接池首次建连/迁移检查在请求路径上；③ 与 `/health` 预热对比。
- 取得证据前**不要改代码**。

## 4. F-06：未知模型 404 细分（最高风险项）

**本轮已尝试并整体回退。必须单独 PR，且必须带红线回归。**

| 项 | 内容 |
|---|---|
| 目标 | 未知模型 → 404 `model_not_found`；未定价模型 **仍** 503 `pricing_unavailable` |
| 已存在的实现参考 | `handler/no_account_error.go` 的 404 `model_not_found` 分类（:34、:101 `classifyNoAccountError`、:132） |
| 本轮事故 | `refinePricingRejection` 一行调用把 `ErrModelPricingUnavailable` 改判成「模型不存在」→ 落入 `writeError` 未知分支 → 兜底 **502 且请求穿透到上游**（5/5 子测试 `expected 503, actual 502 upstream_error`） |
| 因此的硬规则 | ① 路由集合判定与缺价判定**严格分离**，不许在同一处改写错误类型；② 任何新错误类型必须先在 `writeError` 的各协议分支注册，否则兜底 502；③ **不改** `gateway_handler.go:2551-2560` 的 `billingErrorDetails`（缺价恒 503 + `type=pricing_unavailable`） |
| 必过的回归（既有，不需新写） | `TestOpenAIChatCompletionsLockedPricingMissingRejectedBeforeUpstream`（`handler/openai_chat_completions_locked_pricing_test.go:159`）：5 种缺价形态 ×（503 + `upstream.callList()` 空 + `ledger.commands` 空） |
| 做之前先跑一次 | 在 `develop` 上跑该测试确认基线绿，再动手 |
| 教训 | 本轮 7 条分支级探针全部通过，但端到端仍是 502。**分支级探针不能替代端到端断言** |

## 5. 人工事项（不做则不生效）

详见 `ACTION-ITEMS.md`。压缩为 5 条：

1. 🔴 **填 EdgeOne 回源网段** → `deploy/Caddyfile.edgeone:74` 的 `REPLACE_WITH_EDGEONE_ORIGIN_CIDRS`（当前是 `198.51.100.0/24` 占位）。不填不报错，只会让全站 client_ip 坍缩成一个。
2. 🔴 **两步上线**：第 1 步只上"可观测"部分；第 2 步（`server.trusted_proxies` 生效 + `SECURITY_TRUST_FORWARDED_IP_FOR_API_KEY_ACL=false`）必须等回源网段验证通过。回滚 = 把该环境变量改回 `true` 并重启。
3. 🔴 **发布后立刻**：`pwsh -File tools\verify-deployed-build.ps1 -BaseUrl https://sub2.lansn.icu`（退出 0 才算通过；1=DRIFT 回滚；2=连通性失败），再跑 `project-docs/08-runbook.md` §8 的 5 条限流自检（其中第 1 条"轮换 X-Real-IP 第 21 次必须 429"是 F-01 的回归判据）。
4. 🔴 **数据清理需授权**：`uid=5,6,7,8`（`@example.invalid`）、`uid=9`、`uid=3` 下的 `probe-7255`/`probe-g3`、兑换码 `id=1/2/3`。走后台软删除，不直接 `DELETE FROM users`。
5. ⚪ EdgeOne 网段换新后重跑 `08-runbook.md` §9.4 的头部语义回归。

## 6. 通用验收命令（每项改动都要跑，给出退出码）

```powershell
cd F:\sub2\backend
go build -o bin/server.exe ./cmd/server          # 必须 0
go test -tags=unit -count=1 ./...                # 见下方"已知红"
gofmt -l internal cmd                            # 必须空
cd F:\sub2\frontend
.\node_modules\.bin\vue-tsc.cmd -b               # 必须 0
node node_modules/vitest/vitest.mjs run          # 必须全绿
```

**已知可接受的失败（不要当成自己引入）**：

| 失败 | 原因 | 处置 |
|---|---|---|
| 3 条 `TestPgDumper*`（`repository/backup_pg_dumper_test.go`） | 本机 Windows 无 `sh` | 环境性，CI（Linux）不会红 |
| 1 条满载 flaky（`TestInflightEstimate_AccountMappingNoDBAndBoundedMemory` 或 `TestOpenAICompatibleScannerRequestContract`） | 已知 flaky | 隔离 `-count=3` 通过即可 |
| `verify-deployed-build.ps1` 报 dist hash DRIFT | 重建 dist 改变入口 hash（构建不可复现） | 弱判据，默认不影响退出码；强判据是后端 `version` == HEAD |

**工作纪律**（本轮踩过，别再犯）：

- 改共享文件前先 `git show develop:<path>` 取基线全文，**按行插入，绝不整文件覆盖**（本轮一次整文件覆盖删掉了 `apply` 与 `validateSharedGeminiImagePricing`）。
- 多文件改动一次写完再落盘；跨包改动先落被依赖符号（本轮 4 次编译红都是"引用先于定义"）。
- 不要在 `main` 提交；不要改已存在的迁移文件；新迁移从 **259** 起。
- 判定"既有失败"必须做基线对照（`git show develop:` 或 A/B 只换回单个文件），不能靠"文件没被改过"推断。

## 7. 本轮交付索引（需要背景时再读）

| 文件 | 用途 |
|---|---|
| `HANDOFF.md` | 本轮做了什么、最终数字、三条教训 |
| `FIELD-INVENTORY.md` | **F-05/F-04 唯一实施依据**（字段表 + 白名单 + 硬约束 + 验收） |
| `ACTION-ITEMS.md` | 人工事项全文（EdgeOne 网段怎么取、脚本退出码含义、数据清理清单） |
| `VERIFICATION.md` | 验证记录（命令、退出码、通过数、变异验证） |
| `REVIEW-FINAL-VERDICT.md` | 独立审查裁决 |
| `EVIDENCE.md` | 线上实测证据（含 F-02 阈值更正、N-01/N-02/N-03/R-01/O-01/D-01..03） |
| `PLAN.md` | 本轮整改计划与两步上线顺序 |
| `E2E-REPORT.md` | 17 页 E2E 基线与改动清单 |
| `WORKSPACE-CLEANUP.md` | 工作区与 Git 清理记录（含隔离目录位置） |
| `QA-FINAL-REPORT.json` / `QA-CONSOLIDATED.json` | 线上功能矩阵原始结论 |
