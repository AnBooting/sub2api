//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Exercise the actual observer, not a manually invoked second finalization.
func TestReviewLiveObserverRetriesWhenCheckpointAndLedgerFailTogether(t *testing.T) {
	previous := liveSettlementRetryInterval
	liveSettlementRetryInterval = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { liveSettlementRetryInterval = previous })
	price := 0.1
	svc, store, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, store, 0, &price)
	record.ExpiresAt = time.Now().Add(-time.Second)
	require.NoError(t, store.SaveLiveCall(context.Background(), record, time.Hour))
	checkpoint := &reviewLiveCheckpointSequenceStore{liveTestStore: store, failuresRemaining: 2}
	ledger := &reviewLiveBillingLedger{failRemaining: 1}
	usage := &liveFirstInsertUsageRepo{}
	svc.cache = checkpoint
	svc.usageBillingRepo = ledger
	svc.usageLogRepo = usage
	svc.observeLiveCall(record)
	require.GreaterOrEqual(t, checkpoint.beginCalls, 4, "observer must retry after Redis recovery")
	require.Equal(t, 2, ledger.attempts, "observer must retry after the ledger recovers")
	require.Equal(t, 1, ledger.applied)
	require.Zero(t, ledger.conflicts)
	current, err := store.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, LiveSettlementSettled, current.Settlement.Status)
	require.Equal(t, checkpoint.firstCandidate.DurationMs, current.Settlement.DurationMs)
	require.True(t, current.Settlement.UsageLogWritten)
	require.Len(t, usage.rows, 1)
	require.InDelta(t, ledger.amount, usage.rows[record.CallHash].ActualCost, 0.000001)
}

// usage_logs are the source for request counts, list entries and total_cost sums.
func TestReviewLiveFailedAttemptDoesNotDoubleRecordedUsage(t *testing.T) {
	price := 0.1
	svc, store, _, billing := newLiveSettlementTestService()
	usage := &liveFirstInsertUsageRepo{}
	svc.usageLogRepo = usage
	record := newLiveSettlementRecord(t, store, 0, &price)
	billing.err = errors.New("temporary ledger outage")
	svc.finalizeLiveCall(record)
	billing.err = nil
	svc.finalizeLiveCall(record)
	var total float64
	for _, row := range usage.rows {
		total += row.TotalCost
	}
	require.InDelta(t, billing.lastCmd.BalanceCost, total, 1e-12,
		"one successful live session must not contribute its full total_cost twice")
}
