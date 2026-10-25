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
	svc, store, _, billing := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, store, 0, &price)
	record.ExpiresAt = time.Now().Add(-time.Second)
	require.NoError(t, store.SaveLiveCall(context.Background(), record, time.Hour))
	svc.cache = &liveCheckpointFailStore{liveTestStore: store, failSave: true}
	billing.err = errors.New("temporary ledger outage")
	svc.observeLiveCall(record)
	require.GreaterOrEqual(t, billing.calls, 2,
		"the observer must retry with its in-memory frozen settlement when Redis checkpoint writes failed")
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
