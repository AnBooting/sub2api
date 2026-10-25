//go:build unit

package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUserErrorCodeMapsStableCodes 错误码必须是稳定契约：前端按 code 判定，不再解析文案。
func TestUserErrorCodeMapsStableCodes(t *testing.T) {
	for _, tc := range []struct {
		name string
		log  *OpsErrorLog
		want string
	}{
		{"缺价被拒", &OpsErrorLog{StatusCode: http.StatusServiceUnavailable, Phase: "request", Type: "billing_error", Message: "no pricing available for model: gpt-x: pricing not found"}, UserErrorRequestCodeMissingPrice},
		{"缺价（不可用文案）", &OpsErrorLog{StatusCode: http.StatusServiceUnavailable, Phase: "request", Type: "billing_error", Message: "Model pricing is unavailable. Please retry later."}, UserErrorRequestCodeMissingPrice},
		{"普通 503 不算缺价", &OpsErrorLog{StatusCode: http.StatusServiceUnavailable, Phase: "upstream", Type: "upstream", Message: "upstream returned 503"}, "upstream_error"},
		{"鉴权失败", &OpsErrorLog{StatusCode: http.StatusUnauthorized, Phase: "auth", Type: "auth", Message: "invalid api key"}, "auth_failed"},
		{"限流", &OpsErrorLog{StatusCode: http.StatusTooManyRequests, Phase: "request", Type: "rate_limit_error", Message: "rate limited"}, "rate_limit"},
		{"额度不足", &OpsErrorLog{StatusCode: http.StatusPaymentRequired, Phase: "request", Type: "billing_error", Message: "insufficient balance"}, "quota_exhausted"},
		{"参数非法", &OpsErrorLog{StatusCode: http.StatusBadRequest, Phase: "request", Type: "invalid_request_error", Message: "bad request"}, "invalid_request"},
		{"内容策略", &OpsErrorLog{StatusCode: http.StatusBadRequest, Phase: "request", Type: "cyber_policy", Message: "blocked"}, "content_policy"},
		{"无可用渠道", &OpsErrorLog{StatusCode: http.StatusServiceUnavailable, Phase: "routing", Type: "routing", Message: "no available channel"}, "service_unavailable"},
		{"内部错误", &OpsErrorLog{StatusCode: http.StatusInternalServerError, Phase: "internal", Type: "internal", Message: "boom"}, "internal_error"},
		{"其它", &OpsErrorLog{StatusCode: http.StatusBadRequest, Phase: "unknown", Type: "unknown", Message: "?"}, "other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, UserErrorCode(tc.log))
		})
	}
}

// TestToUserErrorRequestCarriesCode 用户可见的错误请求必须带上稳定错误码。
func TestToUserErrorRequestCarriesCode(t *testing.T) {
	view := ToUserErrorRequest(&OpsErrorLog{
		StatusCode: http.StatusServiceUnavailable,
		Phase:      "request",
		Type:       "billing_error",
		Message:    "no pricing available for model: gpt-x: pricing not found",
	})
	require.NotNil(t, view)
	require.Equal(t, UserErrorRequestCodeMissingPrice, view.Code)
}
