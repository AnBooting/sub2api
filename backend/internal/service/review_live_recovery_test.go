//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Model only the SQL repository's documented unique-key storage contract:
// INSERT ... ON CONFLICT (request_id, api_key_id) DO NOTHING. Billing and
// finalization are the real production functions, not duplicated here.
type reviewFirstInsertUsageRepo struct {
	UsageLogRepository
	rows   map[string]UsageLog
	writes int
}

func (r *reviewFirstInsertUsageRepo) Create(_ context.Context, row *UsageLog) (bool, error) {
	r.writes++
	if _, found := r.rows[row.RequestID]; found {
		return false, nil
	}
	r.rows[row.RequestID] = *row
	return true, nil
}

func TestReviewLiveRetryUpdatesDurableUsageAmount(t *testing.T) {
	price := 0.1
	svc, store, _, billing := newLiveSettlementTestService()
	usage := &reviewFirstInsertUsageRepo{rows: map[string]UsageLog{}}
	svc.usageLogRepo = usage
	record := newLiveSettlementRecord(t, store, 0, &price)
	billing.err = errors.New("temporary database outage")
	svc.finalizeLiveCall(record)
	billing.err = nil
	svc.finalizeLiveCall(record)
	current, err := store.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, LiveSettlementSettled, current.Settlement.Status)
	require.Equal(t, 2, billing.calls)
	require.Positive(t, billing.lastCmd.BalanceCost)
	require.Equal(t, billing.lastCmd.BalanceCost, usage.rows[record.CallHash].ActualCost,
		"successful retry must reconcile the first zero-cost usage row; duplicate INSERT cannot update it")
}

type reviewUnavailableSettlementStore struct {
	*liveTestStore
	failSave bool
}

func (s *reviewUnavailableSettlementStore) SaveLiveCall(ctx context.Context, record *LiveCallRecord, ttl time.Duration) error {
	if s.failSave {
		return errors.New("Redis temporarily unavailable after close marker")
	}
	return s.liveTestStore.SaveLiveCall(ctx, record, ttl)
}

func TestReviewLivePendingSurvivesCheckpointWriteFailure(t *testing.T) {
	price := 0.1
	svc, store, _, billing := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, store, 0, &price)
	failing := &reviewUnavailableSettlementStore{liveTestStore: store, failSave: true}
	svc.cache = failing
	billing.err = errors.New("temporary billing outage")
	svc.finalizeLiveCall(record)
	require.Equal(t, 1, billing.calls)
	failing.failSave = false
	billing.err = nil
	// Deliberately retry from persisted storage, as observer/restart does.
	persisted, err := store.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	svc.finalizeLiveCall(persisted)
	require.Equal(t, 2, billing.calls,
		"a successful close marker must not permanently hide settlement whose checkpoint failed")
}

func TestReviewLiveObserverExpiryAutomaticallyRetriesPendingSettlement(t *testing.T) {
	previous := liveSettlementRetryInterval
	liveSettlementRetryInterval = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { liveSettlementRetryInterval = previous })
	price := 0.1
	svc, store, _, billing := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, store, 0, &price)
	record.ExpiresAt = time.Now().Add(-time.Second)
	require.NoError(t, store.SaveLiveCall(context.Background(), record, time.Hour))
	billing.err = errors.New("temporary billing outage")
	svc.observeLiveCall(record)
	persisted, err := store.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, LiveSettlementPending, persisted.Settlement.Status)
	require.GreaterOrEqual(t, billing.calls, 2,
		"observer must enter automatic retry when its own finalization leaves pending settlement")
}
