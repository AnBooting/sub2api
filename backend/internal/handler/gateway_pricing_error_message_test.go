//go:build unit

package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// N-01：缺价错误必须同时满足三条
//  1. 仍是 503 + type=pricing_unavailable（红线：缺价不得放行，也不能改成 404）；
//  2. message 必须可读，不能是 pkgerrors 兜底的 "internal error"。
//
// 根因：billingErrorDetails 用 pkgerrors.Message(err) 取文案，而
// service.ErrModelPricingUnavailable 是裸 errors.New，FromError 会落到
// UnknownMessage="internal error" 兜底分支，于是线上返回
// {"type":"pricing_unavailable","message":"internal error"}——错误类型和文案自相矛盾，
// 终端 SDK 只能显示 "internal error"。
func TestBillingErrorDetailsPricingUnavailableHasReadableMessage(t *testing.T) {
	// 真实生产错误形态：service 层用 %w 包了 sentinel。
	err := fmt.Errorf("%w for model: %s", service.ErrModelPricingUnavailable, "deepseek-v9-ultra")

	status, code, message, retryAfter := billingErrorDetails(err)

	require.Equal(t, http.StatusServiceUnavailable, status, "缺价必须保持 503")
	require.Equal(t, "pricing_unavailable", code, "错误类型是客户端契约，不得改动")
	require.Zero(t, retryAfter)

	require.NotEqual(t, "internal error", message, "不得再用 pkgerrors 兜底文案")
	require.NotEmpty(t, message)
	// 可读性判据：不是内部占位句，且明确说明是"价格/计费未配置"这一可行动问题。
	require.NotContains(t, strings.ToLower(message), "internal error")
	require.Contains(t, strings.ToLower(message), "pric",
		"文案必须让调用方知道是价格未配置，而不是泛化的服务错误")
}

// 裸 sentinel（没有 %w 包装时 service 直接返回）同样必须可读。
func TestBillingErrorDetailsBareSentinelHasReadableMessage(t *testing.T) {
	status, code, message, _ := billingErrorDetails(service.ErrModelPricingUnavailable)
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Equal(t, "pricing_unavailable", code)
	require.NotEqual(t, "internal error", message)
	require.Contains(t, strings.ToLower(message), "pric")
}

// 其它 billing 分支的文案不得被这次改动影响（回归护栏）。
func TestBillingErrorDetailsOtherBranchesUnchanged(t *testing.T) {
	status, code, _, _ := billingErrorDetails(service.ErrBillingServiceUnavailable)
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Equal(t, "billing_service_error", code)

	status, code, _, _ = billingErrorDetails(errors.New("some unrelated billing failure"))
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "billing_error", code)
}

// 红线端到端：Chat Completions 对"已公布但未定价"的模型必须 503 pricing_unavailable
// 且 message 可读（不是 internal error），并且绝不触达上游、不产生账目。
func TestChatCompletionsUnpricedModelReturnsReadable503(t *testing.T) {
	f := newSharedLockedFixture(t, false, "")
	rec := f.serveChat(t, sharedLockedChatBody(false))

	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "pricing_unavailable")
	require.NotContains(t, rec.Body.String(), "internal error",
		"缺价响应的 message 不得是 internal error：%s", rec.Body.String())
	require.Empty(t, f.upstream.callList(), "缺价不得转发上游")
	require.Empty(t, f.ledger.commands, "缺价不得计费")
}
