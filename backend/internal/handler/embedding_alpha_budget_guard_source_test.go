package handler

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEmbeddingsAndAlphaSearchBudgetRejectStopsBeforeSwitch 是 handler 源码级
// 护栏测试：预算拒绝在 embeddings/alpha-search failover 分支必须先于
// RecordOpenAIAccountSwitch / failedAccountIDs，且账号健康/调度上报只能包在
// ShouldRetryNextAccount() 内。完整 handler stub 成本过高；Service 层已钉死
// RequestAttemptBudgetReason 的 ShouldRetryNextAccount=false /
// ShouldReportAccountScheduleFailure=false，这里锁定调用顺序不倒退。
func TestEmbeddingsAndAlphaSearchBudgetRejectStopsBeforeSwitch(t *testing.T) {
	for _, tc := range []struct {
		name string
		file string
	}{
		{name: "embeddings", file: "openai_embeddings.go"},
		{name: "alpha_search", file: "openai_alpha_search.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, err := os.ReadFile(tc.file)
			require.NoErrorf(t, err, "read %s", tc.file)

			body := string(src)
			// 预算耗尽必须在 failover 分支最前面拦截，交给预算错误出口。
			require.Contains(t, body, "failoverErr.Reason == service.RequestAttemptBudgetReason || requestAttemptBudgetExhausted(c.Request.Context()")
			require.Contains(t, body, "h.handleFailoverExhausted(c, service.NewRequestAttemptBudgetFailoverError(), false)")
			require.Contains(t, body, "if failoverErr.ShouldRetryNextAccount() {", "账号调度上报只能对可重试错误进行")

			budgetIdx := strings.Index(body, "预算耗尽立即终止")
			require.GreaterOrEqual(t, budgetIdx, 0, "必须有预算耗尽注释块")

			returnIdx := strings.Index(body, "h.handleFailoverExhausted(c, service.NewRequestAttemptBudgetFailoverError(), false)")
			switchIdx := strings.Index(body, "RecordOpenAIAccountSwitch()")
			failedIdx := strings.Index(body, "failedAccountIDs[account.ID] = struct{}{}")

			require.True(t, returnIdx >= 0, "must contain budget failover return")
			require.True(t, switchIdx >= 0, "must contain account switch")
			require.True(t, failedIdx >= 0, "must contain failedAccountIDs update")
			require.Less(t, returnIdx, switchIdx, "预算拒绝 return 必须先于换号")
			require.Less(t, returnIdx, failedIdx, "预算拒绝 return 必须先于加入 failedAccountIDs")
		})
	}
}

func TestMessagesAndChatCompletionsBudgetRejectSkipsHealthFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		file string
	}{
		{name: "messages", file: "openai_gateway_handler.go"},
		{name: "chat_completions", file: "openai_chat_completions.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, err := os.ReadFile(tc.file)
			require.NoError(t, err)
			body := string(src)
			require.Contains(t, body, `if failoverErr.Reason != service.RequestAttemptBudgetReason {`, "健康失败上报前必须有预算 Reason 守卫")
			require.Contains(t, body, "ObserveOpenAIAccountHealthFailure(c.Request.Context(), account, err)")
		})
	}
}
