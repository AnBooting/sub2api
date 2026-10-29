//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type reviewOneReadGlitchStore struct {
	*liveTestStore
	billing *openAIRecordUsageBillingRepoStub
	reads int
	failAt int
}

func (s *reviewOneReadGlitchStore) GetLiveCall(ctx context.Context, key string) (*LiveCallRecord, error) {
	s.reads++
	if s.reads == s.failAt {
		// Both dependencies recover immediately after this one transient read.
		s.billing.err = nil
		return nil, errors.New("one transient Redis read failure")
	}
	return s.liveTestStore.GetLiveCall(ctx, key)
}

func TestReviewLiveObserverRecoversFromOneRedisReadFailure(t *testing.T) {
	previous := liveSettlementRetryInterval
	liveSettlementRetryInterval = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { liveSettlementRetryInterval = previous })
	for _, tt := range []struct{name string; failAt int}{
		{"retry_entry_read", 3},
		{"retry_loop_read", 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			price := 0.1
			svc, store, _, billing := newLiveSettlementTestService()
			record := newLiveSettlementRecord(t, store, 0, &price)
			record.ExpiresAt = time.Now().Add(-time.Second)
			require.NoError(t, store.SaveLiveCall(context.Background(), record, time.Hour))
			glitch := &reviewOneReadGlitchStore{liveTestStore: store, billing: billing, failAt: tt.failAt}
			svc.cache = glitch
			billing.err = errors.New("temporary ledger outage")
			svc.observeLiveCall(record)
			current, err := store.GetLiveCall(context.Background(), record.CallHash)
			require.NoError(t, err)
			require.NotNil(t, current.Settlement)
			assert.Equal(t, 2, billing.calls, "one transient read must not discard the remaining retry budget")
			assert.Equal(t, LiveSettlementSettled, current.Settlement.Status)
		})
	}
}
