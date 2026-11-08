# PR #19 评审 — I-01b：SMTP 未配置时后端拒绝发码

> 评审人：pr19-smtp-reviewer（共享 task-7）
> 快照：F:\sub2\.agents\worktrees\pr19-review-20261108，HEAD 849c14a7b（PR head），base origin/develop=4a0064318（PR #18 合并后基线），工作树评审前后 git status --porcelain 均为空。
> 规格基准：project-docs/23-handoff-security-remediation-2026-10-30.md §3.3:231-236、project-docs/reviews/2026-11-01-pr18-review/RESOLUTION.md:11；FIELD-INVENTORY.md 中无 I-01 条目。
> 只读评审：未修改业务源码、worktree 文件、Git 索引/提交/推送。

## 0. 裁决建议

**Approve（建议合入）**：**0 Critical、0 Required（阻断项）**。实现与 handoff §3.3 的规格逐项一致（同步/异步两条路径都在读写验证码缓存前拒绝、复用既有 ErrEmailNotConfigured），新测试调用生产代码且三态 + 错误路径覆盖成立，完整 internal/service 单元测试包无回归（exit 0）。

非阻断发现：3 条 Nit、3 条 Optional；另有 6 条 FYI（含 I-01b 同类缺陷仍在其他发码路径、判据只校验存在性不校验连通性等既有边界，均不是本 diff 引入，建议后续轮次收口，不阻断本 PR）。

| 级别 | 编号 | 标题 |
|---|---|---|
| Nit | Nit-1 | 未认证用户看到的 503 文案是英文内部状态，前端无 i18n/可行动映射 |
| Nit | Nit-2 | IsConfigured 的 reason 第二返回值在发码路径被丢弃，PR18 注释宣称的契约未落地 |
| Nit | Nit-3 | 异步“配置齐全”用例里的 cache.reads/writes==0 断言是空转信号（未启动 worker） |
| Optional | Opt-1 | 建议补 HTTP 入口级 503/EMAIL_NOT_CONFIGURED 契约测试（服务层测试未绑定入口） |
| Optional | Opt-2 | 建议补“配置中途变化/恢复”测试，直接证明每请求读 DB |
| Optional | Opt-3 | 建议补前置错误优先级回归（注册关闭/邮箱已存在不得被 SMTP 检查抢占） |
| FYI | FYI-1 | 删 emailService == nil 守卫后不会 panic（依赖 IsConfigured 的 nil-receiver 契约，已有测试锁定）——Lead 复核问题 1 |
| FYI | FYI-2 | SMTP 检查放在队列 nil 检查之前：理论上会掩盖“队列未配置”诊断，但生产 wire 恒提供队列——Lead 复核问题 3 |
| FYI | FYI-3 | OAuth pending / 邮箱绑定发码仍直接走 EmailService.SendVerifyCode：先写码+冷却再失败（有 503、非假成功） |
| FYI | FYI-4 | /user/totp/send-code（2FA 邮件码）仍“未配置也返回 200”（I-01b 同类缺陷，既有；TOTP 未被本 PR 误伤） |
| FYI | FYI-5 | 判据是“host+from 存在”而非可投递；配置 TOCTOU/投递失败仍只写 worker 日志 |
| FYI | FYI-6 | 同步 AuthService.SendVerifyCode 无生产调用者，本轮同步修复是防御性契约补齐 |

---

## 1. 改动事实（当前源码行号）

PR 在 backend/internal/service/auth_service.go 只动了 3 个位置（diff：+8/−3）：

1. 同步 SendVerifyCode：删除旧守卫 if s.emailService == nil { return errors.New("email service not configured") }，改为在 exists/quota 检查之后、读写缓存之前（auth_service.go:330-333）：

       // 在读写验证码缓存前检查发送必需配置，避免未配置时消耗验证码冷却期。
       if configured, _ := s.emailService.IsConfigured(ctx); !configured {
           return ErrEmailNotConfigured
       }

2. 异步 SendVerifyCodeAsync：在 emailQueueService == nil 检查之前插入同一检查（auth_service.go:371-374），注释为“入队成功不代表能够发信，必须先同步检查 SMTP 必需配置”。

3. 判据本身未改（PR18 引入）：EmailService.IsConfigured（email_service.go:206-223）只看 smtp_host + smtp_from 且 strings.TrimSpace 后非空；nil service/nil repo/读设置失败一律 fail-closed 返回 (false, reason)。错误常量 ErrEmailNotConfigured = infraerrors.ServiceUnavailable("EMAIL_NOT_CONFIGURED", "email service not configured")（email_service.go:27），经 infraerrors.ToHTTP 映射 503（pkg/errors/types.go:84-87、pkg/errors/http.go:9-31），response.ErrorFrom 输出 {code:503, reason:"EMAIL_NOT_CONFIGURED", message:"email service not configured"} 并记录 5xx 日志（pkg/response/response.go:80-96）。

旧基线核对（git show origin/develop:backend/internal/service/auth_service.go）：

- 同步函数**确有** emailService == nil 守卫（被本 PR 删除）；
- 异步函数**本来就没有**该守卫（它此前完全不引用 emailService）。

## 2. 必查清单逐项结论

| # | 检查项 | 结论 | 关键证据 |
|---|---|---|---|
| 1 | 拒绝环节 / 判据同源 / 错误码 | **PASS**。拒绝在“校验邮箱存在性与域名配额之后、读缓存/入队之前”；与 PR18 管理端 smtp_configured 调用同一 EmailService.IsConfigured，口径一致；错误 503 + 稳定 reason，不泄露 host/凭据（只泄露“邮件服务未配置”这一运营状态，敏感性低） | auth_service.go:330-333,371-374；email_service.go:27,206-223；setting_handler.go:102-110,148,179；setting_handler_update.go:2296 |
| 2 | 与 captcha/限流顺序、fail-close 相互作用 | **PASS**。路由组先过 BackendModeAuthGuard，再 LimitWithOptions（IP 5/min、账号 10/min、RateLimitFailClose），handler 内先 VerifyCaptcha 再调用 service。SMTP 拒绝发生在全部前置之后：未启用 captcha 时受 5/min/IP 约束；启用 captcha 时还必须先过验证码。Redis 故障时限流 fail-close 直接 429，新 503 不可达。**没有发现用 SMTP 拒绝绕过或放大滥用的路径**（拒绝前无写操作，仅按限流额度增加一次 GetMultiple 读） | routes/auth.go:69,99-103,16-34；middleware/rate_limiter.go:27-33,542-599；handler/auth_handler.go:214-237 |
| 3 | 三态与中途变化 | **PASS（行为）/ Opt-2（测试）**。完全未配、只有 host、只有 from、空白值均被新测试覆盖（含 settings_error 读失败 fail-closed）；配置齐全走既有冷却/入队路径。IsConfigured 每次直接 settingRepo.GetMultiple（repository/setting_repo.go:85-99 每次 SQL 查询，无进程内缓存），因此管理端保存后下一次请求即生效，无需重启。**当前没有直接测试证明“中途变化”**，见 Opt-2 | email_service.go:206-223；repository/setting_repo.go:85-99；auth_service_smtp_config_test.go:38-77 |
| 4 | 新测试质量（118 行） | **PASS**。调用真实生产方法 AuthService.SendVerifyCode/SendVerifyCodeAsync 与真实 EmailService.IsConfigured（只替换 EmailCache spy、真实 EmailQueueService 类型、settingRepoStub 数据），不是复述逻辑；三态各 6 种缺配/错误 × sync/async 共 12 子用例，另加 2 条配置齐全用例；命名描述行为（mode/state 子测试自解释）。小瑕：顶层测试名 SendVerifyCode_SMTPConfiguration 同时覆盖 Async；异步用例两条缓存计数断言是空转（Nit-3） | auth_service_smtp_config_test.go:1-118；helper auth_service_register_test.go:17-72,235-272；运行结果见 §8 |
| 5 | 与 PR18 叠加一致性 | **PASS**。管理端 GET（setting_handler.go:148,179）与 PUT（setting_handler_update.go:2237-2242 先重读设置，:2296 再计算）共用 h.smtpConfigured → EmailService.IsConfigured；发码路径调同一方法；前端 isSmtpConfigured 只读 smtp_configured 布尔（utils/smtp.ts:14-19），PUT 响应经 SettingsView.vue:11956-11965 合并回 form，因此“保存后误报”已修。**方向一致性**：不存在“设置页说不可用但发码通过”（同一函数）；“设置页说可用但发码被拒”在当前判据下也不会出现（除 FYI-5 的连通性/TOCTOU 边界）。前端**没有** EMAIL_NOT_CONFIGURED 的 i18n 映射（见 Nit-1） | setting_handler.go:102-110；setting_smtp_configured_test.go:21-73（PR18，含同判据断言）；SettingsView.vue:9248-9255,11956-11965；frontend/src/utils/smtp.ts:14-19 |
| 6 | 影响面（register / 2FA / OAuth 待验证 / TOTP） | **PASS（无回归）**，但存在同源未覆盖路径（FYI-3/4）。TOTP 未被本 PR 误伤（不走新检查）；backend_mode、注册关闭等前置均在新检查之前 | §3 影响面表 |

## 3. 影响面（哪些流程走 SMTP、本 PR 后的行为）

| 入口 | 调用链 | 新检查点 | 本 PR 后行为 | 证据 |
|---|---|---|---|---|
| 匿名 POST /api/v1/auth/send-verify-code | AuthHandler.SendVerifyCode → VerifyCaptcha → AuthService.SendVerifyCodeAsync → IsConfigured → EnqueueVerifyCode → worker → EmailService.SendVerifyCode | auth_service.go:371-374 | **本 PR 目标**：未配置 → 503 EMAIL_NOT_CONFIGURED，不入队、不消耗冷却；配置齐全 → 200 + countdown | handler/auth_handler.go:214-237；routes/auth.go:99-103 |
| AuthService.SendVerifyCode（同步） | 直接 → EmailService.SendVerifyCode | auth_service.go:330-333 | 未配置 → 503；配置齐全走既有冷却 | 生产**无调用者**（grep 仅测试），见 FYI-6 |
| 匿名(带 pending 会话) POST /api/v1/auth/oauth/pending/send-verify-code | SendPendingOAuthVerifyCode → 直接 EmailService.SendVerifyCode | **无** | 先写验证码 + 1 分钟冷却，随后 SendEmail 失败；包装错误含 ErrEmailNotConfigured，仍映射 503。非假成功，但浪费一次冷却/写码 | auth_oauth_email_flow.go:31-59；auth_oauth_pending_flow.go:564-618；routes/auth.go:180-183 |
| 已登录 POST /api/v1/user/account-bindings/email/send-code | SendEmailIdentityBindCode → 直接 EmailService.SendVerifyCode | **无** | 同上（写码+冷却后 503） | auth_email_binding.go:95-131；routes/user.go:36 |
| 已登录 POST /api/v1/user/totp/send-code（邮件 2FA） | TotpService.SendVerifyCode → EnqueueVerifyCode | **无** | 未配置仍 200 {success:true}；worker 发信失败只写日志——I-01b 同类缺陷仍在（既有） | totp_service.go:543-560；totp_handler.go:178-191；routes/user.go:57；email_queue_service.go:64-100 |
| 已登录 POST /api/v1/user/notify-email/send-code | SendNotifyEmailCode → 先发信成功才写缓存 | 无（但顺序本来就是先发信） | 未配置时返回错误且不写码/不计冷却，无假成功 | user_service.go:1218-1245 |
| 注册提交 RegisterWithVerification | 提交阶段只校验已发验证码/开关 | 不涉及 | 不受影响（邮件验证开启且 emailService nil 时注册阶段已有 ErrServiceUnavailable 守卫） | auth_service.go Register 路径（base 同） |

顺序核对：backend_mode 组守卫（routes/auth.go:69）→ 限流（:99-103）→ handler captcha（auth_handler.go:221-225）→ SendVerifyCodeAsync 内注册开关（:348-352）→ 保留域（:354-356）→ 邮箱存在性（:358-366）→ 域名配额（:367-369）→ **新 SMTP 检查**（:371-374）。新检查没有抢占任何既有前置错误，也不会把注册关闭等错误改写成 503（Opt-3 建议补回归锁定）。

## 4. Lead 复核问题（重点结论）

### Q1（nil 解引用 panic？）→ **FYI-1，不成立，无 panic，无需修改**

- NewAuthService 的 emailService *EmailService 参数确实允许 nil（auth_service.go:109-139 只赋值不校验）；wire 生产装配必经 ProvideAuthService（service/wire.go:69-104），queue 由 ProvideEmailQueueService 恒构造（wire.go:62-65，注册于 :997）。
- 但 IsConfigured 是**指针接收者方法**，首行 if s == nil || s.settingRepo == nil（email_service.go:206-209）**先判 nil 再解引用**，所以 nil 接收者调用安全返回 (false, reason)，不会 panic。Go 允许对 nil 指针调用方法，崩溃只会发生在无 nil 检查的解引用处。
- 新测试 missing_service 场景显式 svc.emailService = nil（auth_service_smtp_config_test.go:56-57），sync/async 两个子用例均 PASS（§8 运行输出），直接证明该路径可达且不 panic。
- 事实修正：旧代码**只有同步 SendVerifyCode 有** if s.emailService == nil 守卫（已被本 PR 删除）；异步 SendVerifyCodeAsync 基线本来就没有该守卫（其代码在此之前不引用 emailService），因此“删守卫导致 panic”的前提只对同步路径成立，且已被 nil-receiver 契约兜住。
- 残余风险（非阻断）：若后续有人删掉 IsConfigured 的 s == nil 分支，该路径会 panic；现有 missing_service 用例可以拦住（变异 M5）。
- **团队既有 nil 守卫约定核对（Lead 补充，定级 FYI）**：AuthService 其他入口仍用显式 nil 守卫并以 ErrServiceUnavailable（503 SERVICE_UNAVAILABLE）拒绝——auth_service.go:197（注册提交）、1604/1654（密码重置）、auth_email_binding.go:111、auth_oauth_email_flow.go:42/97；本 PR 在 331/372 改为直接调用 IsConfigured。定级 **FYI（不阻断、无需修改）**：nil 服务属装配缺失，两种写法结果都是 503（旧守卫通用 SERVICE_UNAVAILABLE，新路径 EMAIL_NOT_CONFIGURED），新行为语义更具体、不 panic、不新增泄露；若追求代码库写法统一，可在 331/372 前补 if s.emailService == nil { return ErrEmailNotConfigured }（纯风格，非必需）。

### Q2（前端口径 / 用户是否看到可行动提示？）→ **Nit-1，显式失败成立，但文案不可行动**

- 后端 503 body：{code:503, reason:"EMAIL_NOT_CONFIGURED", message:"email service not configured"}（email_service.go:27 + pkg/errors/http.go:19-30；response.ErrorFrom :82-95）。
- 前端拦截器把错误 rejection 成 {status, code, message, reason, metadata}（frontend/src/api/client.ts:92-98）；EmailVerifyView.vue:584-587 显示 buildRegistrationErrorMessage 的返回值，该函数只对 EMAIL_DOMAIN_REGISTRATION_LIMIT 做专门映射（:785-790），其余回落到 buildAuthErrorMessage（utils/authError.ts:11-24），后者直接返回后端 message；frontend/src/i18n 全库 grep **无** EMAIL_NOT_CONFIGURED key（仅有 sendCodeFailed 兜底文案，en/common.ts:287、zh/common.ts:286，但后端 message 非空时不会使用兜底）。
- 因此用户会看到英文 email service not configured：**失败是显式的（比修复前 200 + “已发送”好）**，但不可行动、非本地化，且属于运营配置状态。结论：不阻断合并，建议在本 PR 或紧随补前端映射（如 auth.emailServiceNotConfigured → “邮箱验证服务暂不可用，请联系管理员” / “Email verification is temporarily unavailable. Please contact the administrator.”），或后端用 metadata 携带 reason 供前端区分 smtp_host_missing/smtp_from_missing。
- 泄露评估：只暴露“邮件服务未配置”的存在性，不暴露 host/from/凭据；对匿名用户可接受，但配合 i18n 改成通用可行动文案更妥。

### Q3（SMTP 检查放在 emailQueueService==nil 之前，诊断是否被掩盖？）→ **FYI-2，理论风险，可接受**

- 基线 async 顺序：emailQueueService == nil → 500 email queue service not configured（git show origin/develop 核对）；本 PR 顺序：SMTP 检查（:371-374）→ 队列检查（:376-380）。若两者同时缺失，返回 503 EMAIL_NOT_CONFIGURED，“队列未配置”这一接线诊断被推后。
- 生产装配中队列**恒非 nil**（wire.go:62-65 构造 + :997 注册，ProvideAuthService 必收该参数），能走到“队列 nil”的只有测试或错误 wire；而 handoff §3.3:234 原文要求“在 :371 之前插入”（即队列检查之前），实现与规格一致。故仅记录为诊断顺序 FYI，不要求改。
- 若将来想保留队列诊断，可先判 emailQueueService == nil 再判 SMTP（顺序反转不影响 I-01b 契约，但与本轮规格文字相反）；由 Lead 决定。

## 5. 发现详情

### Nit-1（前端文案/可行动性）— 见 Q2
证据：frontend/src/api/client.ts:92-98、frontend/src/views/auth/EmailVerifyView.vue:584-587,785-790、frontend/src/utils/authError.ts:11-24、frontend/src/i18n 无匹配 key、backend/internal/service/email_service.go:27。
建议：前端 i18n 映射（zh/en 同步，check:i18n 门禁）或后端 metadata reason；不阻断。

### Nit-2（reason 契约未落地）
证据：email_service.go:186-193 注释明确写“管理端设置页与**发码错误路径**都用它给出可行动的提示”，但所有 EmailConfigReasonHostMissing/EmailConfigReasonFromMissing 的读取点只存在于 email_service.go:208-220 自身（backend 全库 grep 无其他消费）；管理端只用 bool（setting_handler.go:108）；本 PR 两处调用均写 configured, _（auth_service.go:331,372）。
影响：新增调用点继续固化“reason 是死契约”；若按 Nit-1 做前端区分，本可直接用 reason。建议二选一：返回 ErrEmailNotConfigured.WithMetadata(map[string]string{"reason": reason})；或删除第二返回值/修正 PR18 注释。属 Nit。

### Nit-3（异步缓存计数断言空转）
证据：TestAuthService_SendVerifyCodeAsync_ConfiguredSMTPEnqueues 用 &EmailQueueService{taskChan: make(chan EmailTask, 1)}（auth_service_smtp_config_test.go:105-106）**没有启动 worker**（worker 只在 NewEmailQueueService 里 start()，email_queue_service.go:37-53,64-77），而 cache.reads/writes 只可能被 worker 或 sync 路径触发；因此该用例末尾 require.Zero(cache.reads)/require.Zero(cache.writes)（:116-117）无论生产逻辑是否先检查都成立，是空转断言。真正的“未配置不写冷却”由 12 个未配置子用例的 sync 断言 + 异步 queue.taskChan 空断言覆盖，结论不受影响。
建议：把两条断言换成“EnqueueVerifyCode 恰好调用一次”的 spy，或注释标明仅对 sync 路径有意义。属 Nit。

### Optional-1（建议补 HTTP 入口级契约测试）
现状：新测试只到 service 方法；backend 全库 grep EMAIL_NOT_CONFIGURED 没有 handler/server 级断言（只有本测试与 subscription_expiry_service.go:177）。response.ErrorFrom 是通用 5xx 映射，未来若有人把 auth 路由的错误处理改写/包一层，服务层测试仍绿。本项目已有“探针与入口无绑定”教训（handoff §2 验证纪律；PR18 REVIEW 也要求不复制 handler 逻辑）。
建议（Lead 可执行，不改业务源码，测试放 internal/server/routes 或 internal/handler）：用既有测试脚手架构造带真实 AuthService（settingRepoStub 缺 smtp_host）+ 真实路由注册，POST /api/v1/auth/send-verify-code，断言 503 且 body reason=="EMAIL_NOT_CONFIGURED"、排队队列长度 0；再补一条配置齐全 200/60 的对照。属 Optional（非阻断）。

### Optional-2（建议补“配置中途变化/恢复”测试）
现状：实现每请求读 DB（repository/setting_repo.go:85-99），但无测试；若将来给 IsConfigured 加缓存，现有用例不会红。
建议：同一个 settingRepoStub（map 引用可变）+ async 路径：调用 1（缺 host）→ 503；向 map 写入 host+from → 调用 2 → 200/Countdown=60 且入队；删除 from → 调用 3 → 503。断言三次都不 panic、状态切换即时生效。属 Optional。

### Optional-3（建议补前置错误优先级回归）
建议：SMTP 未配置时断言 ErrRegDisabled（registration_enabled=false 或无 settingService）与 ErrEmailExists 仍优先于 ErrEmailNotConfigured；防止未来把配置检查提前到 DB 查询之前改变枚举/配额行为。属 Optional。

## 6. FYI 明细（既有缺陷/边界，不阻断）

- **FYI-1** 见 Q1：删守卫不 panic；依赖 IsConfigured 的 nil-receiver 契约，missing_service 用例已锁定；事实修正：异步基线本无该守卫。
- **FYI-2** 见 Q3：诊断顺序，生产 wire 恒有队列；与规格文字一致。
- **FYI-3（同源路径未覆盖）** SendPendingOAuthVerifyCode（auth_oauth_email_flow.go:53）与 SendEmailIdentityBindCode（auth_email_binding.go:130）直接调 EmailService.SendVerifyCode，未先查 IsConfigured：会先写码+冷却、再在 SendEmail 处失败（EmailService.SendVerifyCode 内 SendEmail→GetSMTPConfig 返回 ErrEmailNotConfigured 并被 fmt.Errorf("send email: %w",…) 包装，errors.As 仍映射 503，故用户看到失败不是假成功）。与新注释“在读写验证码缓存前检查”的意图不一致。建议后续把同一检查下沉到 EmailService.SendVerifyCode 首部（覆盖全部调用方），或在两个入口显式补检查；属既有缺陷，非本 diff 引入。
- **FYI-4（2FA/TOTP 仍假成功）** TotpService.SendVerifyCode 只 EnqueueVerifyCode（totp_service.go:543-560），handler 直接返回 {success:true}（totp_handler.go:178-191）；SMTP 未配置时 worker 在 email_queue_service.go:80-96 只写日志。这是 I-01b 的同类缺陷，**本 PR 未修且未误伤 TOTP**。建议作为 I-01c 或下一轮项，不阻断本 PR。
- **FYI-5（存在性判据 ≠ 可投递）** smtp_configured 与发码同判 host+from 非空（email_service.go:201-223 注释已声明不校验凭据）：密码错误/主机不可达时管理端仍显示“已配置”，异步发码仍 200 后 worker 静默失败；且异步路径 IsConfigured 与 worker 内 GetSMTPConfig 之间配置若被清空存在窄 TOCTOU，失败只进日志。属 PR18 已接受的取舍，本 PR 不扩大；建议后续对邮件投递失败做告警/审计。
- **FYI-6（同步方法无生产调用者）** 全库 grep SendVerifyCode( 只有测试调用 AuthService.SendVerifyCode（auth_service_register_test.go:531,578,592、auth_reserved_email_test.go:68、本测试）；生产注册/发码入口只走 Async。规格 §3.3:234 明确要求同步方法一并处理，故该改动是防御性契约补齐，无运行时影响（发布影响面只有 Async）。

## 7. 建议变异（交 Lead 执行；M5 同时用于证明 nil 契约）

| # | 改动（业务源码） | 预期变红 | 说明 |
|---|---|---|---|
| M1 | 删除 auth_service.go:371-374（async 检查） | TestAuthService_SendVerifyCode_SMTPConfiguration 的 6 个 async/* 子用例 | ErrorIs 失败；async/missing_service 会入队（200） |
| M2 | 删除 auth_service.go:330-333（sync 检查） | 6 个 sync/* 子用例 | 缺配时先读缓存返回 ErrVerifyCodeTooFrequent；sync/missing_service 会 nil 解引用 panic（同样算红） |
| M3 | email_service.go:219-221 删除 from 检查（只留 host） | 4 个 missing_from/blank_from 子用例（sync+async 各 2） | 证明测试确实锁定 host+from 双判据 |
| M4 | email_service.go:211-215 读设置失败改返回 true, "" | 2 个 settings_error 子用例（sync+async） | 证明 fail-closed 被测试锁定 |
| M5 | email_service.go:207 删除 s == nil（保留 repo nil） | 2 个 missing_service 子用例 | 证明 nil-receiver 契约被测试锁定，同时回答 Lead Q1 的残余风险 |
| M6 | 把 async 检查移到 EnqueueVerifyCode 成功之后（原位置删除，:390 后插入） | 6 个 async/* 子用例 | require.Empty(queue.taskChan) 应失败 |

执行建议（由 Lead 在 worktree 内做、逐条改动后恢复）：

    $env:GOROOT=[Environment]::GetEnvironmentVariable('GOROOT','User'); $env:Path=[Environment]::GetEnvironmentVariable('Path','User') + ';' + [Environment]::GetEnvironmentVariable('Path','Machine')
    cd F:\sub2\.agents\worktrees\pr19-review-20261108\backend
    go test -p 1 -tags=unit ./internal/service -run 'TestAuthService_SendVerifyCode' -count=1 -v

预期每次变异后 exit 1 且对应子测试 --- FAIL；恢复源码后回到 exit 0。变异完成后请 git status --porcelain 确认 worktree 干净（评审快照要求）。

## 8. 实际运行命令与结果（本机，Go 1.27.3）

环境（每条命令显式注入）：$env:GOROOT=[Environment]::GetEnvironmentVariable('GOROOT','User')（=F:\tools\go1.27.3\go）、GOTOOLCHAIN=local、Path=User;Machine。

| # | 命令 | 退出码 | 观察结果 |
|---|---|---|---|
| 1 | go version / go env GOROOT GOTOOLCHAIN（backend 目录） | 0 | go version go1.27.3 windows/amd64；GOROOT=F:\tools\go1.27.3\go；GOTOOLCHAIN=local |
| 2 | go test -p 1 -tags=unit ./internal/service -run 'SMTP|VerifyCode|Register' -count=1 -v | **0** | 211 行输出、全 PASS；其中新增 TestAuthService_SendVerifyCode_SMTPConfiguration 12/12 子用例 PASS、..._ConfiguredSMTPUsesExistingCooldown PASS、...Async_ConfiguredSMTPEnqueues PASS；ok github.com/Wei-Shaw/sub2api/internal/service 4.421s |
| 3 | go test -p 1 -tags=unit ./internal/service -count=1（全包回归；后台作业 pwsh-a1be71cd06234796） | **0** | ok github.com/Wei-Shaw/sub2api/internal/service 291.444s（外层 311.3s；超过 Lead 3 分钟建议阈值但已正常完成，无 FAIL/flaky） |
| 4 | gofmt -l internal/service/auth_service.go internal/service/auth_service_smtp_config_test.go | 0 | 输出空（格式干净） |
| 5 | git diff --check origin/develop...pr19-review-20261108 -- backend/internal/service/auth_service.go backend/internal/service/auth_service_smtp_config_test.go | 0 | 输出空（无空白错误） |
| 6 | git status --porcelain（worktree，评审开始/结束各一次） | 0 | 两次均为空（只读评审，未改 worktree） |

**未运行/未验证**：

- 变异验证（M1–M6）——按要求由 Lead 执行；本报告未改任何业务源码。
- HTTP 入口级 503 契约探针——见 Optional-1，建议由 Lead 在测试脚手架中补；本任务未写探针（避免放进 backend 生产树）。
- 前端测试套件（本 PR 未改前端，仅静态 grep 核对错误映射；Lead 已报 frontend typecheck/build exit 0）。
- golangci-lint、go build（本任务未重复跑；Lead 已报 build exit 0；CI 门禁由 Lead 汇总）。
- integration/-tags=integration（需 PostgreSQL+Redis，本机无 docker 且任务未要求）。
- 未覆盖：真实 SMTP 投递、真实 Redis 限流行为、真实 admin 页面交互（均需运行环境）。

## 9. 结论复核清单

- [x] 拒绝在读写验证码缓存/入队之前，未配置不消耗冷却、不入队（sync/async 均有测试）
- [x] 判据与 PR18 smtp_configured 同源（EmailService.IsConfigured，host+from 双判据，trim，fail-closed）
- [x] 错误为 503 EMAIL_NOT_CONFIGURED，不泄露凭据；前端未映射 → Nit-1
- [x] captcha/限流/backend_mode/注册开关前置顺序正确，无绕过或放大
- [x] 三态（未配/半配/齐全）+ 错误路径全覆盖；中途变化实现正确、测试缺（Opt-2）
- [x] 新测试调用生产代码、覆盖错误路径、命名可读；入口绑定与异步断言小瑕（Opt-1/Nit-3）
- [x] PR18 GET/PUT/发码三处同判据；前端 PUT 响应合并正确
- [x] TOTP 未被误伤；OAuth pending / 邮箱绑定 / TOTP 的既有缺口记录为 FYI，不在本 diff 引入
