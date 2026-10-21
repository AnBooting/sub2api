package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Exercises the production finalizer, not a copy of its cost formula.
// Requires both a billed usage record and a financial mutation.
func TestReviewLiveReportedChargeHasFinancialSettlement(t *testing.T) {
	for _, subscriptionID := range []int64{0, 9} {
		t.Run(map[bool]string{true: "subscription", false: "balance"}[subscriptionID > 0], func(t *testing.T) {
			usage := &liveTestUsageRepo{}
			store := &liveTestStore{}
			billing := &openAIRecordUsageBillingRepoStub{}
			user := &openAIRecordUsageUserRepoStub{}
			subscription := &openAIRecordUsageSubRepoStub{}
			svc := &OpenAIGatewayService{
				cache: store, concurrencyService: NewConcurrencyService(&liveTestConcurrencyCache{}),
				usageLogRepo: usage, usageBillingRepo: billing, userRepo: user, userSubRepo: subscription,
				billingService: NewBillingService(rawChatCompletionsTestConfig(), nil),
			}
			record := &LiveCallRecord{CallID: "review-settlement", CallHash: hashLiveCallID("review-settlement"),
				AccountID: 1, APIKeyID: 2, UserID: 3, GroupID: 4, SubscriptionID: subscriptionID,
				CreatedAt: time.Now().Add(-2 * time.Minute), ExpiresAt: time.Now().Add(time.Hour), Controller: LiveControllerPending}
			// JSON also permits running this test against the PR base, before the field existed.
			require.NoError(t, json.Unmarshal([]byte(`{"RealtimePricePerMin":0.1}`), record))
			require.NoError(t, store.SaveLiveCall(context.Background(), record, time.Hour))
			svc.finalizeLiveCall(record)
			require.Len(t, usage.logs, 1)
			require.InDelta(t, 0.2, usage.logs[0].ActualCost, 0.02)
			t.Logf("usage actual_cost=%.6f; billing Apply=%d; balance deduction=%d; subscription increment=%d",
				usage.logs[0].ActualCost, billing.calls, user.deductCalls, subscription.incrementCalls)
			require.Positive(t, billing.calls+user.deductCalls+subscription.incrementCalls,
				"nonzero billed usage must have a financial settlement")
		})
	}
}
