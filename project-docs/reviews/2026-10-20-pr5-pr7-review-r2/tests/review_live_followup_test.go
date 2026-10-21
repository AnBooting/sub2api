package service

import (
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// The regular gateway deliberately bypasses financial settlement in simple mode.
// Exercise the real Live finalizer with both balance and subscription records.
func TestReviewLiveSimpleModeDoesNotDebitFinancialAccounts(t *testing.T) {
	for _, subscriptionID := range []int64{0, 9} {
		t.Run(map[bool]string{true: "subscription", false: "balance"}[subscriptionID > 0], func(t *testing.T) {
			price := 0.1
			svc, store, _, billing := newLiveSettlementTestService()
			svc.cfg = &config.Config{RunMode: config.RunModeSimple}
			record := newLiveSettlementRecord(t, store, subscriptionID, &price)
			svc.finalizeLiveCall(record)
			if billing.lastCmd != nil {
				t.Logf("simple mode: balance_cost=%f subscription_cost=%f", billing.lastCmd.BalanceCost, billing.lastCmd.SubscriptionCost)
				require.Zero(t, billing.lastCmd.BalanceCost, "simple mode must not deduct the user balance")
				require.Zero(t, billing.lastCmd.SubscriptionCost, "simple mode must not consume subscriptions")
			}
		})
	}
}

// Simulate a transaction failure followed by recovery, then retry the same
// production finalizer. The failed call must remain financially recoverable.
func TestReviewLiveTransientSettlementFailureCanBeRetried(t *testing.T) {
	price := 0.1
	svc, store, usage, billing := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, store, 0, &price)
	billing.err = errors.New("temporary database outage")
	svc.finalizeLiveCall(record)
	require.Equal(t, 1, billing.calls)
	require.Len(t, usage.logs, 1)
	require.Zero(t, usage.logs[0].ActualCost)
	billing.err = nil
	svc.finalizeLiveCall(record)
	require.Equal(t, 2, billing.calls, "closed marker must not make a failed settlement permanently uncollectible")
}

// Compare the actual Live command with the existing production billing builder
// for the same authenticated key configured with lifetime and window limits.
// This deliberately does not reproduce the builder's conditional logic.
func TestReviewLiveCommandPreservesKeyLimitCosts(t *testing.T) {
	price := 0.1
	svc, store, usage, billing := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, store, 0, &price)
	key := &APIKey{ID: record.APIKeyID, Quota: 1, RateLimit5h: 1, RateLimit1d: 1, RateLimit7d: 1}
	svc.finalizeLiveCall(record)
	require.NotNil(t, billing.lastCmd)
	log := usage.logs[0]
	expected := buildUsageBillingCommand(record.CallHash, log, &postUsageBillingParams{
		Cost: &CostBreakdown{TotalCost: log.TotalCost, ActualCost: log.ActualCost},
		User: &User{ID: record.UserID}, APIKey: key, Account: &Account{ID: record.AccountID},
		APIKeyService: &openAIRecordUsageAPIKeyQuotaStub{},
	})
	require.Positive(t, expected.APIKeyQuotaCost)
	require.Positive(t, expected.APIKeyRateLimitCost)
	t.Logf("live key costs: quota=%f rate_limit=%f; full-key builder: quota=%f rate_limit=%f",
		billing.lastCmd.APIKeyQuotaCost, billing.lastCmd.APIKeyRateLimitCost,
		expected.APIKeyQuotaCost, expected.APIKeyRateLimitCost)
	require.Equal(t, expected.APIKeyQuotaCost, billing.lastCmd.APIKeyQuotaCost, "Live must retain configured key quota accounting")
	require.Equal(t, expected.APIKeyRateLimitCost, billing.lastCmd.APIKeyRateLimitCost, "Live must retain configured key window accounting")
}
