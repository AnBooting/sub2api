package service

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

// TS-02 S5（C-5 故障分类）—— 在 UpstreamFailoverError 出口给出五分类。
//
// 契约（project-docs/prd/11-共享契约.md:90-102）：上游尝试结果统一归入下表，
// 只有 retry_* 两类允许换号/换组/兜底。
//
//	retry_account   上游账号鉴权失败、账号限流、账号满载、渠道能力不匹配 → 标记账号、换号
//	retry_transient 连接失败、上游临时 5xx，且确认未执行或可幂等          → 换号
//	fail_client     本站 Key 无效、余额不足、参数错误、模型名无效          → 直接返回
//	fail_policy     内容审核、明确业务拒绝                                 → 直接返回，不换渠道
//	fail_uncertain  超时且无法确认上游是否执行；流已向客户端输出            → 不重发，返回明确失败
//
// 定位（本期不改语义）：FailureClass 只是记录 + 结构化日志 + S6 组循环的输入。
// 重试/换号决策仍然完全由 ShouldRetryNextAccount() 决定，本文件不改变任何既有
// 401/403/429/5xx 行为，也不参与任何分支判断（handler 目前不读它）。
//
// 硬不变量（Classify 的实现保证 + 测试钉死）：
//
//	ShouldRetryNextAccount() == false  → Classify() 绝不返回 retry_account/retry_transient
//	ShouldRetryNextAccount() == true   → Classify() 绝不返回 fail_client/fail_policy/fail_uncertain
//
// 因此 IsRetryableClass() 与 ShouldRetryNextAccount() 恒等；S6 若要用分类驱动组循环，
// 读 IsRetryableClass() 与读 ShouldRetryNextAccount() 是同一个判断。
type FailureClass string

const (
	// FailureClassRetryAccount 账号级故障（鉴权/限流/满载/账号余额），换号有帮助。
	FailureClassRetryAccount FailureClass = "retry_account"
	// FailureClassRetryTransient 连接失败或上游临时 5xx，确认未执行或可幂等，换号有帮助。
	FailureClassRetryTransient FailureClass = "retry_transient"
	// FailureClassFailClient 本站侧客户端错误（Key/余额/参数/模型名），直接返回。
	FailureClassFailClient FailureClass = "fail_client"
	// FailureClassFailPolicy 内容审核或明确业务拒绝，直接返回且不换渠道。
	FailureClassFailPolicy FailureClass = "fail_policy"
	// FailureClassFailUncertain 超时且无法确认上游是否执行，或流已向客户端输出，不重发。
	FailureClassFailUncertain FailureClass = "fail_uncertain"
)

// IsRetryable 报告该分类是否允许换号/换组/兜底（C-5：只有 retry_* 两类允许）。
func (c FailureClass) IsRetryable() bool {
	switch c {
	case FailureClassRetryAccount, FailureClassRetryTransient:
		return true
	default:
		return false
	}
}

// IsValid 报告该值是否是五个契约取值之一（零值/拼写错误返回 false）。
func (c FailureClass) IsValid() bool {
	switch c {
	case FailureClassRetryAccount, FailureClassRetryTransient,
		FailureClassFailClient, FailureClassFailPolicy, FailureClassFailUncertain:
		return true
	default:
		return false
	}
}

// IsRetryableClass 报告该 failover 错误的分类是否允许换号/换组。nil 错误返回 false。
//
// 这是 S6 组循环唯一允许读取的分类谓词：它不替代 ShouldRetryNextAccount()，
// 而是与其保持恒等（见文件头硬不变量），并额外把「为什么不能重试」暴露给日志。
func (e *UpstreamFailoverError) IsRetryableClass() bool {
	if e == nil {
		return false
	}
	return e.Classify().IsRetryable()
}

// failureClassPolicyReasons 是「明确业务拒绝/策略」类 reason：命中即 fail_policy。
// 内容审核类：cyber_policy（OpenAI 硬阻断）。策略/提供方配置类：Grok 凭据侧
// provider 状态不可用与 failover 预算耗尽——都不是换号能解决的账号故障。
var failureClassPolicyReasons = map[GatewayFailureReason]struct{}{
	GatewayFailureReason("cyber_policy"): {},
	GrokCredentialReasonProviderDown:     {},
	GrokCredentialReasonProviderConfig:   {},
	GrokCredentialReasonStateUpdate:      {},
	GrokCredentialReasonFailoverTimeout:  {},
}

// failureClassPolicyReasonTokens 是 reason 文本里的策略/审核信号（大小写不敏感）。
var failureClassPolicyReasonTokens = []string{
	"cyber_policy",
	"content_policy",
	"policy_violation",
	"content_filter",
	"moderation",
	"safety",
}

// failureClassUncertainReasonTokens 是「无法确认上游是否执行」的信号。
var failureClassUncertainReasonTokens = []string{
	"timeout",
	"timed_out",
	"deadline",
	"uncertain",
}

// failureClassClientStatuses 是本站侧客户端错误状态（C-5 fail_client）。
// 只在错误已声明不可重试（NextAccountStop）时才归入 fail_client：若出口仍然
// 允许换号（例如按账号区分的 413 request body too large、上游图片账号 402
// 余额不足），换号语义优先，分类落到 retry_*——见 Classify 的硬不变量。
var failureClassClientStatuses = map[int]struct{}{
	http.StatusBadRequest:            {}, // 400 参数错误/模型名无效
	http.StatusPaymentRequired:       {}, // 402 本站余额不足
	http.StatusNotFound:              {}, // 404
	http.StatusMethodNotAllowed:      {}, // 405
	http.StatusNotAcceptable:         {}, // 406
	http.StatusConflict:              {}, // 409
	http.StatusRequestEntityTooLarge: {}, // 413
	http.StatusUnsupportedMediaType:  {}, // 415
	http.StatusUnprocessableEntity:   {}, // 422
}

// failureClassPolicyStatuses 是策略类状态：451 Unavailable For Legal Reasons
// （内容审核/合规拒绝）。403 不在其中：它按账号鉴权归入 retry_account，保持
// 既有 403 换号语义；仅当 reason 已显式指向策略时才由 reason 分支判为 fail_policy。
var failureClassPolicyStatuses = map[int]struct{}{
	http.StatusUnavailableForLegalReasons: {}, // 451
}

// failureClassAccountStatuses 是账号级可换号状态：鉴权/限流/满载/账号余额。
var failureClassAccountStatuses = map[int]struct{}{
	http.StatusUnauthorized:    {}, // 401 账号鉴权失败
	http.StatusPaymentRequired: {}, // 402 上游账号余额不足（可换号时）
	http.StatusForbidden:       {}, // 403 账号被拒
	http.StatusTooManyRequests: {}, // 429 账号限流
	529:                        {}, // 529 上游满载（Anthropic/Gemini 既有语义）
}

// Classify 把一次上游尝试的失败归入 C-5 五分类。nil 返回 fail_uncertain（保守：
// 没有证据说明换号有帮助，绝不换号）。
//
// 判定顺序（先到先判，理由优先于状态码，显式不确定信号优先于兜底）：
//  1. nil → fail_uncertain
//  2. 本站策略终止 reason（预算耗尽 RequestAttemptBudgetReason、cyber_policy、
//     Grok provider 配置/状态类、Grok 凭据 failover 超时）→ fail_policy
//  3. 策略/审核类 reason 文本或 451 → fail_policy
//  4. 超时/无法确认上游是否执行（reason 文本含 timeout/deadline/uncertain，或 408/504）→ fail_uncertain
//  5. 本站客户端错误状态（400/402/404/405/406/409/413/415/422）→ fail_client
//  6. Stage==account_auth、401/402/403/429/529，或「另一个账号可能挂载该模型」的
//     400 模型不存在（读 ResponseBody 判定）→ retry_account
//  7. 其余可重试（5xx、连接类、请求级瞬时降载）→ retry_transient
//  8. 兜底：可重试 → retry_transient；不可重试 → fail_policy
//
// 第 2-5 步（fail_* 分支）全部以「错误已声明不可重试」为前提，第 6-8 步（retry_*
// 分支）全部只在可重试时产生——这是文件头硬不变量的实现方式，不是额外约定。
// 分类只读被分类对象，绝不回写它的任何语义字段（Stage/Scope/Reason/NextAccountAction）。
func (e *UpstreamFailoverError) Classify() FailureClass {
	if e == nil {
		return FailureClassFailUncertain
	}
	// 决策权仍在 ShouldRetryNextAccount()：本方法只描述原因，不改变该判断。
	retryable := e.ShouldRetryNextAccount()

	// 出口快照优先：显式设置且合法的 FailureClass 直接采用，避免与出口记录漂移。
	// 但快照必须与决策一致——不一致（陈旧值或写错）时回退到下面的动态推导，
	// 使文件头的硬不变量在结构上不可能被违反。
	if stored := e.FailureClass; stored.IsValid() && stored.IsRetryable() == retryable {
		return stored
	}

	// 终止型错误（NextAccountStop）：只可能落在 fail_client / fail_policy /
	// fail_uncertain 三类，绝不会返回 retry_*——即使状态码/阶段看起来像账号故障。
	// 这是硬不变量在代码结构上的保证，不依赖后面每个分支的细心排序。
	if !retryable {
		return e.classifyTerminal()
	}

	// 6. 账号级可换号故障：凭据阶段失败、账号鉴权/限流/满载状态，或
	//    「另一个账号可能挂载了该模型」的 400 模型不存在（OpenAI 兼容路径，
	//    换号有帮助；判定完全由响应体决定，不依赖也不改写出口的语义字段）。
	if e.Stage == GatewayFailureStageAccountAuth {
		return FailureClassRetryAccount
	}
	if _, ok := failureClassAccountStatuses[e.StatusCode]; ok {
		return FailureClassRetryAccount
	}
	if e.StatusCode == http.StatusBadRequest && isOpenAICompatibleModelNotFound400(e.ResponseBody) {
		return FailureClassRetryAccount
	}
	// 7. 其余可重试：临时 5xx、连接类、请求级瞬时降载（故障与账号无关）。
	return FailureClassRetryTransient
}

// classifyTerminal 把「已声明不换号」（NextAccountStop）的错误归入三类 fail_*。
// 调用方必须先确认 !ShouldRetryNextAccount()；本方法不可能返回 retry_*。
//
// 判定顺序（理由优先于状态码）：
//  1. 本站策略终止 reason（预算耗尽、cyber_policy、Grok provider 配置/状态类、
//     Grok 凭据 failover 超时）→ fail_policy
//  2. 策略/审核类 reason 文本或 451 → fail_policy
//  3. 超时/无法确认上游是否执行（reason 文本或 408/504）→ fail_uncertain
//  4. 本站客户端错误状态（400/402/404/405/406/409/413/415/422）→ fail_client
//  5. 兜底 → fail_policy（本站策略终止，不换渠道）
func (e *UpstreamFailoverError) classifyTerminal() FailureClass {
	// 1. 本站策略终止 reason。
	if e.Reason == RequestAttemptBudgetReason || isRequestAttemptBudgetFailoverReason(e.Reason) {
		return FailureClassFailPolicy
	}
	if _, ok := failureClassPolicyReasons[e.Reason]; ok {
		return FailureClassFailPolicy
	}
	// 2. 策略/审核类 reason 文本与 451。
	if reasonHasAnyToken(string(e.Reason), failureClassPolicyReasonTokens) {
		return FailureClassFailPolicy
	}
	if _, ok := failureClassPolicyStatuses[e.StatusCode]; ok {
		return FailureClassFailPolicy
	}
	// 3. 超时/无法确认上游是否执行。
	if reasonHasAnyToken(string(e.Reason), failureClassUncertainReasonTokens) ||
		e.StatusCode == http.StatusRequestTimeout || // 408
		e.StatusCode == http.StatusGatewayTimeout { // 504
		return FailureClassFailUncertain
	}
	// 4. 本站客户端错误。
	if _, ok := failureClassClientStatuses[e.StatusCode]; ok {
		return FailureClassFailClient
	}
	// 5. 兜底：已声明不换号但没有更具体原因 → 本站策略终止。
	return FailureClassFailPolicy
}

// isRequestAttemptBudgetFailoverReason 报告 reason 是否为预算耗尽转成的终止型错误。
// 预算耗尽的构造点唯一（NewRequestAttemptBudgetFailoverError 写 RequestAttemptBudgetReason），
// 这里额外识别「预算哨兵」语义，避免未来新增 reason 时静默落入 fail_policy 兜底之外的类。
func isRequestAttemptBudgetFailoverReason(reason GatewayFailureReason) bool {
	if reason == RequestAttemptBudgetReason {
		return true
	}
	return strings.Contains(string(reason), "attempt_budget")
}

// reasonHasAnyToken 报告 reason 文本是否命中任一信号（大小写不敏感）。
func reasonHasAnyToken(reason string, tokens []string) bool {
	if reason == "" {
		return false
	}
	lower := strings.ToLower(reason)
	for _, token := range tokens {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

// gatewayFailureClassLogFields 构造一次分类日志的字段集。字段刻意与 C-5 分类依据
// 一一对应，便于在不读代码的情况下复盘「为什么这一类」；抽成纯函数是为了让测试
// 能在不安装全局 logger 的前提下断言日志载荷。nil 错误返回 nil。
func gatewayFailureClassLogFields(err *UpstreamFailoverError, event string) []zap.Field {
	if err == nil {
		return nil
	}
	class := err.Classify()
	return []zap.Field{
		zap.String("event", event),
		zap.String("failure_class", string(class)),
		zap.Bool("failure_class_retryable", class.IsRetryable()),
		zap.Bool("retry_next_account", err.ShouldRetryNextAccount()),
		zap.Int("upstream_status", err.StatusCode),
		zap.String("failure_stage", string(err.Stage)),
		zap.String("failure_scope", string(err.Scope)),
		zap.String("failure_reason", string(err.Reason)),
		zap.Bool("safe_to_failover_after_write", err.SafeToFailoverAfterWrite),
		zap.Bool("request_scoped_transient", err.RequestScopedTransient),
	}
}

// logGatewayFailureClass 在 failover 出口记录一次结构化分类。nil 错误直接返回
// （出口可能在没有错误上下文时被调用）；extra 字段由调用点补充账号等上下文。
func logGatewayFailureClass(err *UpstreamFailoverError, event string, extra ...zap.Field) {
	fields := gatewayFailureClassLogFields(err, event)
	if fields == nil {
		return
	}
	logger.L().With(zap.String("component", "service.gateway")).Warn(
		"gateway.failure_classified",
		append(fields, extra...)...,
	)
}
