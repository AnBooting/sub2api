//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// geminiModelsBudgetTestAccount 返回一个走 AI Studio 兼容上游（API Key）的账号。
func geminiModelsBudgetTestAccount() *Account {
	return &Account{
		ID:          261,
		Platform:    PlatformGemini,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-pool"},
	}
}

// exhaustedBudgetContext 构造一个已用尽发送次数的请求预算 ctx。
func exhaustedBudgetContext(t *testing.T) (context.Context, *RequestAttemptBudget) {
	t.Helper()
	budget := NewRequestAttemptBudget(time.Now(), 1, time.Minute, time.Time{})
	require.NoError(t, budget.AllowSend(), "第一次发送应当放行")
	require.True(t, budget.Exhausted(), "预算应当已耗尽")
	return WithRequestAttemptBudget(context.Background(), budget), budget
}

// TestGeminiModelsEndpointsRespectRequestAttemptBudget 行为级断言：models 端点
// （GET /v1beta/models、/v1beta/models/{model}）的上游发送同样受单逻辑请求的发送
// 预算约束——预算耗尽时必须被拒，且不得向上游发出请求。
//
// 2026-10-21：上游 0.2.17 新增 gemini_models_endpoints.go 时直接调用
// httpUpstream.Do，绕过了发送预算；这里用行为断言把这条护栏重新立住。
func TestGeminiModelsEndpointsRespectRequestAttemptBudget(t *testing.T) {
	account := geminiModelsBudgetTestAccount()

	t.Run("列表接口：预算耗尽时拒绝且不向上游发请求", func(t *testing.T) {
		upstream := &httpUpstreamRecorder{resp: vertexModelsJSONResponse(http.StatusOK, `{"models":[]}`)}
		svc := newGeminiModelsEndpointsTestService(nil, upstream)
		ctx, budget := exhaustedBudgetContext(t)

		_, err := svc.ForwardListModels(ctx, account)
		require.Error(t, err)
		require.True(t, IsRequestAttemptBudgetError(err), "预算拒绝错误必须可被识别: %v", err)
		require.Empty(t, upstream.requests, "预算拒绝时不得向上游发请求")
		require.Equal(t, 1, budget.Used(), "被预算挡下的尝试不虚增发送计数")
	})

	t.Run("详情接口：预算耗尽时拒绝且不向上游发请求", func(t *testing.T) {
		upstream := &httpUpstreamRecorder{resp: vertexModelsJSONResponse(http.StatusOK, `{"models":[]}`)}
		svc := newGeminiModelsEndpointsTestService(nil, upstream)
		ctx, budget := exhaustedBudgetContext(t)

		_, err := svc.ForwardGetModel(ctx, account, "gemini-native")
		require.Error(t, err)
		require.True(t, IsRequestAttemptBudgetError(err), "预算拒绝错误必须可被识别: %v", err)
		require.Empty(t, upstream.requests, "预算拒绝时不得向上游发请求")
		require.Equal(t, 1, budget.Used(), "被预算挡下的尝试不虚增发送计数")
	})

	t.Run("预算可用时发送放行并计入预算", func(t *testing.T) {
		upstream := &httpUpstreamRecorder{resp: vertexModelsJSONResponse(http.StatusOK, `{"models":[{"name":"models/gemini-native"}]}`)}
		svc := newGeminiModelsEndpointsTestService(nil, upstream)
		budget := NewRequestAttemptBudget(time.Now(), 1, time.Minute, time.Time{})
		ctx := WithRequestAttemptBudget(context.Background(), budget)

		res, err := svc.ForwardListModels(ctx, account)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.StatusCode)
		require.Len(t, upstream.requests, 1)
		require.Equal(t, 1, budget.Used(), "真实上游发送必须计入请求发送预算")
	})

	t.Run("没有预算时不限制（只读列举行为不变）", func(t *testing.T) {
		upstream := &httpUpstreamRecorder{resp: vertexModelsJSONResponse(http.StatusOK, `{"models":[{"name":"models/gemini-native"}]}`)}
		svc := newGeminiModelsEndpointsTestService(nil, upstream)

		res, err := svc.ForwardListModels(context.Background(), account)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.StatusCode)
		require.Len(t, upstream.requests, 1)
	})
}
