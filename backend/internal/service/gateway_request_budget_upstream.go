package service

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

// TS-02（PRD-02）S3：把 S1 的 RequestAttemptBudget 接到真实上游发送点，并按契约拦截。
//
// S2 阶段的语义是「只计不拦」：预算拒绝只记 WARN，请求照常转发。S3 起预算拒绝
// 直接返回错误、不再转发，由调用方的传输错误处理把它转成终止型
// UpstreamFailoverError（NextAccountStop：不重试、不换号）。
//
// nil 预算（尚未接线的入口）语义仍是「无预算 = 不限制」：不计数、不拦截。
// S3 之后只有 OpenAI E7（/v1/responses）路径会真正拿到预算；Anthropic/Gemini/
// Antigravity 的钩子在 S3 仍是 nil 预算 = 不计数，等 S3b 才铺开。

// EnsureRequestAttemptBudget 保证 ctx 上挂了单逻辑请求预算：已有预算时原样返回
// （幂等，对齐 prepareRequestBilling 里 GatewayRequestBillingFromContext 的先例），
// 否则用给定参数构造并挂载。
//
// maxSends <= 0 或 window <= 0 时仍构造预算（S1 的 fail-closed 语义：不允许任何
// 发送），但记 WARN 提示配置异常——S3 起这种配置会真的挡住全部发送。
func EnsureRequestAttemptBudget(ctx context.Context, maxSends int, window time.Duration, clientDeadline time.Time) (context.Context, *RequestAttemptBudget) {
	if ctx == nil {
		ctx = context.Background()
	}
	if existing := RequestAttemptBudgetFromContext(ctx); existing != nil {
		return ctx, existing
	}
	if maxSends <= 0 || window <= 0 {
		slog.Warn("request_attempt_budget_invalid_config",
			"max_sends", maxSends,
			"window", window.String(),
			"note", "fail-closed budget constructed; TS-02 S3 rejects every upstream send")
	}
	budget := NewRequestAttemptBudget(time.Now(), maxSends, window, clientDeadline)
	return WithRequestAttemptBudget(ctx, budget), budget
}

// sendUpstreamWithBudget 是真实上游推理发送的统一入口：先向请求 ctx 上的预算申请
// 一次发送，再按 profile 选择 Do / DoWithTLS 转发（profile == nil 走 Do）。
//
// S3 拦截：预算拒绝（次数或 deadline 用尽）时记 WARN 并返回该错误，不转发。
// nil 预算（尚未接线的入口）不计也不拦，直接转发。
func sendUpstreamWithBudget(
	upstream HTTPUpstream,
	req *http.Request,
	proxyURL string,
	accountID int64,
	accountConcurrency int,
	profile *tlsfingerprint.Profile,
) (*http.Response, error) {
	if budget := RequestAttemptBudgetFromContext(req.Context()); budget != nil {
		if err := budget.AllowSend(); err != nil {
			logRequestAttemptBudgetDenied(budget, err)
			return nil, err
		}
	}
	if profile == nil {
		return upstream.Do(req, proxyURL, accountID, accountConcurrency)
	}
	return upstream.DoWithTLS(req, proxyURL, accountID, accountConcurrency, profile)
}

// allowUpstreamSend 给 OpenAI 家族的唯一发送汇聚点（doOpenAIUpstream）用：nil 预算
// 返回 nil；否则申请一次发送，S3 语义下预算拒绝时返回包装了 S1 哨兵的错误，
// 调用方必须据此放弃本次发送（不得吞掉错误后继续转发）。
func allowUpstreamSend(ctx context.Context) error {
	budget := RequestAttemptBudgetFromContext(ctx)
	if budget == nil {
		return nil
	}
	if err := budget.AllowSend(); err != nil {
		logRequestAttemptBudgetDenied(budget, err)
		return err
	}
	return nil
}

// logRequestAttemptBudgetDenied 记录一次被预算拒绝的发送。S3 起该发送不会发生，
// 错误文本自带 used/max，这里再补上 deadline 与剩余时间。
func logRequestAttemptBudgetDenied(budget *RequestAttemptBudget, err error) {
	slog.Warn("request_attempt_budget_denied",
		"error", err,
		"used", budget.Used(),
		"deadline", budget.Deadline().Format(time.RFC3339Nano),
		"remaining", budget.Remaining().String(),
		"enforced", true,
	)
}

// IsRequestAttemptBudgetError 报告 err 是否为预算拒绝（次数或 deadline 用尽）。
// 传输错误处理用它把预算拒绝从「传输故障」里分流出去：上游从未被调用，
// 既不该记 Ops 上游错误，也不该临时封禁账号。
func IsRequestAttemptBudgetError(err error) bool {
	return errors.Is(err, ErrRequestAttemptBudgetExhausted) || errors.Is(err, ErrRequestAttemptBudgetDeadline)
}

// RequestAttemptBudgetReason 是预算拒绝转成的终止型 failover 错误的 Reason。
// handler 据此复用 handleFailoverExhausted 输出 C-7 既有的 no_available_group（503）。
const RequestAttemptBudgetReason GatewayFailureReason = "request_attempt_budget"

// requestAttemptBudgetClientMessage 是预算耗尽时返回给客户端的文案。
const requestAttemptBudgetClientMessage = "Service temporarily unavailable: request attempt budget exhausted"

// requestAttemptBudgetFailoverBody 是预算拒绝时的响应体占位：上游从未被调用，
// 这里只保留与传输失败同构的错误体，便于既有透传/日志链路解析。
var requestAttemptBudgetFailoverBody = []byte(`{"error":{"type":"api_error","code":"no_available_group","message":"Service temporarily unavailable: request attempt budget exhausted"}}`)

// NewRequestAttemptBudgetFailoverError 构造预算耗尽的终止型 failover 错误：
// 503 + NextAccountStop（ShouldRetryNextAccount() 返回 false，不重试、不换号）。
// Reason 让 handler 复用既有耗尽出口，错误码用 C-7 既有的 no_available_group。
func NewRequestAttemptBudgetFailoverError() *UpstreamFailoverError {
	return &UpstreamFailoverError{
		StatusCode:        http.StatusServiceUnavailable,
		ResponseBody:      requestAttemptBudgetFailoverBody,
		NextAccountAction: NextAccountStop,
		Reason:            RequestAttemptBudgetReason,
		ClientStatusCode:  http.StatusServiceUnavailable,
		ClientMessage:     requestAttemptBudgetClientMessage,
	}
}
