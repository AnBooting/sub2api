//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// These methods model one Redis hash while leaving the shared lifecycle test untouched.
func (s *liveTestStore) BeginLiveSettlement(_ context.Context, hash string, candidate *LiveSettlement, _ time.Duration) (*LiveSettlement, string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.CallHash != hash {
		return nil, "", false, ErrLiveCallNotFound
	}
	if s.record.Settlement == nil {
		state := *candidate
		s.record.Settlement = &state
	}
	first := s.record.Controller != LiveControllerClosed
	s.record.Controller = LiveControllerClosed
	s.record.ControllerOwner = ""
	state := *s.record.Settlement
	encoded, err := json.Marshal(&state)
	return &state, string(encoded), first, err
}

func (s *liveTestStore) BeginLiveSettlementVerified(_ context.Context, record *LiveCallRecord, candidate *LiveSettlement, _ time.Duration) (*LiveSettlement, string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.record
	if stored == nil || stored.CallHash != record.CallHash {
		return nil, "", false, ErrLiveCallNotFound
	}
	if record.CallID == "" || hashLiveCallID(record.CallID) != record.CallHash || record.UserID <= 0 || record.AccountID <= 0 || record.APIKeyID <= 0 || record.LeaseID == "" || record.Model == "" || record.CreatedAt.UnixMilli() <= 0 || record.ExpiresAt.UnixMilli() <= 0 || stored.CallID != record.CallID || stored.UserID != record.UserID || stored.AccountID != record.AccountID || stored.APIKeyID != record.APIKeyID || stored.GroupID != record.GroupID || stored.SubscriptionID != record.SubscriptionID || stored.LeaseID != record.LeaseID || stored.Model != record.Model || stored.CreatedAt.UnixMilli() != record.CreatedAt.UnixMilli() || stored.ExpiresAt.UnixMilli() != record.ExpiresAt.UnixMilli() || !reflect.DeepEqual(stored.RealtimePricePerMin, record.RealtimePricePerMin) || !reflect.DeepEqual(stored.BillingPolicy, record.BillingPolicy) {
		return nil, "", false, ErrLiveIdentityMismatch
	}
	if record.RealtimePricePerMin == nil && (candidate.TotalCost != 0 || candidate.ActualCost != 0) {
		return nil, "", false, ErrLiveIdentityMismatch
	}
	if stored.Settlement == nil {
		state := *candidate
		stored.Settlement = &state
	}
	first := stored.Controller != LiveControllerClosed
	stored.Controller = LiveControllerClosed
	stored.ControllerOwner = ""
	state := *stored.Settlement
	encoded, err := json.Marshal(&state)
	return &state, string(encoded), first, err
}

// A persistent Redis outage affects the atomic command as well as Get.
func (s *liveLoopReadGlitchStore) BeginLiveSettlementVerified(ctx context.Context, record *LiveCallRecord, candidate *LiveSettlement, ttl time.Duration) (*LiveSettlement, string, bool, error) {
	if s.failAlways {
		return nil, "", false, errors.New("transient Redis read failure")
	}
	return s.liveTestStore.BeginLiveSettlementVerified(ctx, record, candidate, ttl)
}

type reviewLiveAuditClaim struct {
	owner   string
	until   time.Time
	written bool
}

var reviewLiveAuditClaims = struct {
	sync.Mutex
	entries map[*liveTestStore]*reviewLiveAuditClaim
}{entries: make(map[*liveTestStore]*reviewLiveAuditClaim)}

func (s *liveTestStore) ClaimLiveSettlementAudit(_ context.Context, hash, owner string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.CallHash != hash {
		return false, ErrLiveCallNotFound
	}
	if s.record.Settlement == nil {
		return false, ErrLiveSettlementInvalid
	}
	if s.record.Settlement.Status == LiveSettlementSettled {
		return false, nil
	}
	if s.record.Settlement.Status != LiveSettlementPending {
		return false, ErrLiveSettlementInvalid
	}
	reviewLiveAuditClaims.Lock()
	defer reviewLiveAuditClaims.Unlock()
	claim := reviewLiveAuditClaims.entries[s]
	if claim == nil {
		claim = &reviewLiveAuditClaim{}
		reviewLiveAuditClaims.entries[s] = claim
	}
	if claim.written {
		return false, nil
	}
	if claim.owner != "" && time.Now().Before(claim.until) {
		return claim.owner == owner, nil
	}
	claim.owner, claim.until = owner, time.Now().Add(ttl)
	return true, nil
}

func (s *liveTestStore) FinishLiveSettlementAudit(_ context.Context, hash, owner string, written bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.CallHash != hash {
		return false, ErrLiveCallNotFound
	}
	reviewLiveAuditClaims.Lock()
	defer reviewLiveAuditClaims.Unlock()
	claim := reviewLiveAuditClaims.entries[s]
	if claim == nil || claim.owner != owner {
		return false, nil
	}
	claim.written = claim.written || written
	claim.owner = ""
	return true, nil
}

func (s *liveTestStore) CompareAndSwapLiveSettlement(_ context.Context, hash, expected string, next *LiveSettlement, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.CallHash != hash {
		return false, ErrLiveCallNotFound
	}
	encoded, err := json.Marshal(s.record.Settlement)
	if err != nil {
		return false, err
	}
	if string(encoded) != expected {
		return false, nil
	}
	state := *next
	s.record.Settlement = &state
	return true, nil
}

func (s *liveCheckpointFailStore) BeginLiveSettlement(ctx context.Context, hash string, candidate *LiveSettlement, ttl time.Duration) (*LiveSettlement, string, bool, error) {
	if s.failSave {
		return nil, "", false, errors.New("redis checkpoint unavailable")
	}
	return s.liveTestStore.BeginLiveSettlement(ctx, hash, candidate, ttl)
}

func (s *liveCheckpointFailStore) CompareAndSwapLiveSettlement(ctx context.Context, hash, expected string, next *LiveSettlement, ttl time.Duration) (bool, error) {
	if s.failSave {
		return false, errors.New("redis checkpoint unavailable")
	}
	return s.liveTestStore.CompareAndSwapLiveSettlement(ctx, hash, expected, next, ttl)
}

func (s *reviewUnavailableSettlementStore) BeginLiveSettlement(ctx context.Context, hash string, candidate *LiveSettlement, ttl time.Duration) (*LiveSettlement, string, bool, error) {
	if s.failSave {
		return nil, "", false, errors.New("redis checkpoint unavailable")
	}
	return s.liveTestStore.BeginLiveSettlement(ctx, hash, candidate, ttl)
}

func (s *reviewUnavailableSettlementStore) CompareAndSwapLiveSettlement(ctx context.Context, hash, expected string, next *LiveSettlement, ttl time.Duration) (bool, error) {
	if s.failSave {
		return false, errors.New("redis checkpoint unavailable")
	}
	return s.liveTestStore.CompareAndSwapLiveSettlement(ctx, hash, expected, next, ttl)
}

// reviewLiveCheckpointSequenceStore injects a bounded or persistent checkpoint outage.
type reviewLiveCheckpointSequenceStore struct {
	*liveTestStore
	failuresRemaining int
	failAlways        bool
	beginCalls        int
	firstCandidate    *LiveSettlement
}

func (s *reviewLiveCheckpointSequenceStore) BeginLiveSettlement(ctx context.Context, hash string, candidate *LiveSettlement, ttl time.Duration) (*LiveSettlement, string, bool, error) {
	s.beginCalls++
	if s.firstCandidate == nil {
		state := *candidate
		s.firstCandidate = &state
	}
	if s.failAlways || s.failuresRemaining > 0 {
		if s.failuresRemaining > 0 {
			s.failuresRemaining--
		}
		return nil, "", false, errors.New("temporary checkpoint outage")
	}
	return s.liveTestStore.BeginLiveSettlement(ctx, hash, candidate, ttl)
}

// reviewLiveStaleReadStore holds the second Redis read until the first close
// completes, while both calls have already read the same pre-settlement state.
type reviewLiveStaleReadStore struct {
	*liveTestStore
	mu            sync.Mutex
	reads         int
	firstRead     chan struct{}
	secondRead    chan struct{}
	releaseSecond chan struct{}
}

func (s *reviewLiveStaleReadStore) GetLiveCall(ctx context.Context, hash string) (*LiveCallRecord, error) {
	record, err := s.liveTestStore.GetLiveCall(ctx, hash)
	if err != nil || record == nil {
		return record, err
	}
	if record.Settlement != nil {
		state := *record.Settlement
		record.Settlement = &state
	}
	s.mu.Lock()
	s.reads++
	read := s.reads
	s.mu.Unlock()
	switch read {
	case 1:
		close(s.firstRead)
		<-s.secondRead
	case 2:
		close(s.secondRead)
		<-s.releaseSecond
	}
	return record, nil
}

// reviewLiveBillingLedger mirrors the production ledger's request fingerprint
// conflict rather than treating every duplicate request as successful.
type reviewLiveBillingLedger struct {
	UsageBillingRepository
	mu            sync.Mutex
	fingerprints  map[string]string
	applied       int
	attempts      int
	failRemaining int
	conflicts     int
	amount        float64
}

func (r *reviewLiveBillingLedger) Apply(_ context.Context, cmd *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	cmd.Normalize()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attempts++
	if r.failRemaining > 0 {
		r.failRemaining--
		return nil, errors.New("temporary ledger outage")
	}
	if r.fingerprints == nil {
		r.fingerprints = make(map[string]string)
	}
	if previous, ok := r.fingerprints[cmd.RequestID]; ok {
		if previous != cmd.RequestFingerprint {
			r.conflicts++
			return nil, ErrUsageBillingRequestConflict
		}
		return &UsageBillingApplyResult{Applied: false}, nil
	}
	r.fingerprints[cmd.RequestID] = cmd.RequestFingerprint
	r.applied++
	r.amount = cmd.BalanceCost + cmd.SubscriptionCost
	return &UsageBillingApplyResult{Applied: true}, nil
}

func reviewWaitLiveSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for settlement")
	}
}

func TestReviewLiveSettlementConcurrentStaleSnapshots(t *testing.T) {
	price := 0.1
	svc, base, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, base, 0, &price)
	store := &reviewLiveStaleReadStore{liveTestStore: base, firstRead: make(chan struct{}), secondRead: make(chan struct{}), releaseSecond: make(chan struct{})}
	svc.cache = store
	usage := &liveFirstInsertUsageRepo{}
	ledger := &reviewLiveBillingLedger{}
	leases := &liveTestConcurrencyCache{}
	svc.usageLogRepo = usage
	svc.usageBillingRepo = ledger
	svc.concurrencyService = NewConcurrencyService(leases)
	svc2 := *svc // Separate service instances model separate processes sharing Redis and the ledger.
	stale := *record
	stale.Settlement = &LiveSettlement{Status: LiveSettlementPending, ActualCost: 9, TotalCost: 9, DurationMs: 5400000, FrozenAt: time.Now()}
	firstDone := make(chan struct{})
	secondDone := make(chan struct{})
	go func() { svc.finalizeLiveCall(record); close(firstDone) }()
	reviewWaitLiveSignal(t, store.firstRead)
	go func() { svc2.finalizeLiveCall(&stale); close(secondDone) }()
	reviewWaitLiveSignal(t, store.secondRead)
	reviewWaitLiveSignal(t, firstDone)
	close(store.releaseSecond)
	reviewWaitLiveSignal(t, secondDone)
	stored, err := base.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, LiveControllerClosed, stored.Controller)
	require.Equal(t, 2, leases.releases, "each confirmed close retries the idempotent release")
	require.Equal(t, 1, ledger.applied)
	require.Zero(t, ledger.conflicts, "a stale contender must use the winning frozen amount")
	require.Equal(t, LiveSettlementSettled, stored.Settlement.Status, "pending must not overwrite settled")
	require.InDelta(t, ledger.amount, stored.Settlement.ActualCost, 0.000001)
	require.InDelta(t, ledger.amount, usage.rows[record.CallHash].ActualCost, 0.000001)
}

func TestReviewLiveSettlementCheckpointOutageDoesNotConflict(t *testing.T) {
	price := 0.1
	svc, base, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, base, 0, &price)
	failing := &liveCheckpointFailStore{liveTestStore: base, failSave: true}
	svc.cache = failing
	usage := &liveUsageLogFailingRepo{failNext: true}
	ledger := &reviewLiveBillingLedger{}
	svc.usageLogRepo = usage
	svc.usageBillingRepo = ledger
	svc.finalizeLiveCall(record)
	failing.failSave = false
	time.Sleep(30 * time.Millisecond)
	stale, err := base.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	svc.finalizeLiveCall(stale)
	// If the first attempt could not write its usage log, retry the recovery path.
	svc.finalizeLiveCall(stale)
	stored, err := base.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Zero(t, ledger.conflicts, "recovery must never recompute an already billed amount")
	require.Equal(t, 1, ledger.applied)
	require.Equal(t, LiveSettlementSettled, stored.Settlement.Status)
	require.True(t, stored.Settlement.UsageLogWritten)
	require.InDelta(t, ledger.amount, usage.rows[record.CallHash].ActualCost, 0.000001)
}

func TestReviewLiveSettlementSimpleModeLogRecovery(t *testing.T) {
	price := 0.1
	svc, store, _, billing := newLiveSettlementTestService()
	svc.cfg = &config.Config{RunMode: config.RunModeSimple}
	usage := &liveUsageLogFailingRepo{failNext: true}
	svc.usageLogRepo = usage
	record := newLiveSettlementRecord(t, store, 0, &price)
	svc.finalizeLiveCall(record)
	stored, err := store.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, LiveSettlementSettled, stored.Settlement.Status)
	require.False(t, stored.Settlement.UsageLogWritten)
	svc.finalizeLiveCall(record)
	require.Zero(t, billing.calls)
	row, ok := usage.rows[record.CallHash]
	require.True(t, ok)
	require.Positive(t, row.TotalCost)
	require.Zero(t, row.ActualCost, "simple mode must remain non-billing after a log retry")
}

// Simulate a Redis operation that committed but whose reply was lost.
type reviewLiveLostReplyStore struct {
	*liveTestStore
	beginLost  bool
	updateLost bool
}

func (s *reviewLiveLostReplyStore) BeginLiveSettlement(ctx context.Context, hash string, candidate *LiveSettlement, ttl time.Duration) (*LiveSettlement, string, bool, error) {
	state, token, first, err := s.liveTestStore.BeginLiveSettlement(ctx, hash, candidate, ttl)
	if err == nil && s.beginLost {
		s.beginLost = false
		return nil, "", false, errors.New("begin reply lost")
	}
	return state, token, first, err
}

func (s *reviewLiveLostReplyStore) CompareAndSwapLiveSettlement(ctx context.Context, hash, expected string, next *LiveSettlement, ttl time.Duration) (bool, error) {
	ok, err := s.liveTestStore.CompareAndSwapLiveSettlement(ctx, hash, expected, next, ttl)
	if err == nil && ok && s.updateLost {
		s.updateLost = false
		return false, errors.New("update reply lost")
	}
	return ok, err
}

func TestReviewLiveSettlementLostRedisReplies(t *testing.T) {
	price := 0.1
	svc, base, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, base, 0, &price)
	store := &reviewLiveLostReplyStore{liveTestStore: base, beginLost: true, updateLost: true}
	leases := &liveTestConcurrencyCache{}
	ledger := &reviewLiveBillingLedger{}
	usage := &liveFirstInsertUsageRepo{}
	svc.cache = store
	svc.concurrencyService = NewConcurrencyService(leases)
	svc.usageBillingRepo = ledger
	svc.usageLogRepo = usage
	svc.finalizeLiveCall(record)
	current, err := base.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, 1, leases.releases)
	require.Equal(t, 1, ledger.applied)
	require.Zero(t, ledger.conflicts)
	require.Equal(t, LiveSettlementSettled, current.Settlement.Status)
	require.True(t, current.Settlement.UsageLogWritten)
	require.InDelta(t, ledger.amount, usage.rows[record.CallHash].ActualCost, 0.000001)
}

func TestReviewLiveSettlementSimpleModeFrozenAcrossConfigChange(t *testing.T) {
	price := 0.1
	svc, store, _, billing := newLiveSettlementTestService()
	svc.cfg = &config.Config{RunMode: config.RunModeSimple}
	usage := &liveUsageLogFailingRepo{failNext: true}
	svc.usageLogRepo = usage
	record := newLiveSettlementRecord(t, store, 0, &price)
	svc.finalizeLiveCall(record)
	svc.cfg = &config.Config{}
	svc.finalizeLiveCall(record)
	require.Zero(t, billing.calls)
	row, ok := usage.rows[record.CallHash]
	require.True(t, ok)
	require.Positive(t, row.TotalCost)
	require.Zero(t, row.ActualCost)
}

type reviewLiveRepeatedLostReplyStore struct {
	*liveTestStore
	lost int
}

func (s *reviewLiveRepeatedLostReplyStore) BeginLiveSettlement(ctx context.Context, hash string, candidate *LiveSettlement, ttl time.Duration) (*LiveSettlement, string, bool, error) {
	state, token, first, err := s.liveTestStore.BeginLiveSettlement(ctx, hash, candidate, ttl)
	if err == nil && s.lost > 0 {
		s.lost--
		return nil, "", false, errors.New("begin reply lost")
	}
	return state, token, first, err
}

func TestReviewLiveSettlementRepeatedLostRepliesReleaseLease(t *testing.T) {
	price := 0.1
	svc, base, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, base, 0, &price)
	store := &reviewLiveRepeatedLostReplyStore{liveTestStore: base, lost: 2}
	leases := &liveTestConcurrencyCache{}
	ledger := &reviewLiveBillingLedger{}
	svc.cache = store
	svc.concurrencyService = NewConcurrencyService(leases)
	svc.usageBillingRepo = ledger
	svc.finalizeLiveCall(record)
	require.Zero(t, ledger.applied, "billing waits until the checkpoint is confirmed")
	require.Zero(t, leases.releases)
	svc.finalizeLiveCall(record)
	require.Equal(t, 1, leases.releases, "a committed close with both replies lost must release its lease on recovery")
	require.Equal(t, 1, ledger.applied)
}

func TestReviewLiveSettlementCheckpointRecoveryPreservesCandidate(t *testing.T) {
	previous := liveSettlementRetryInterval
	liveSettlementRetryInterval = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { liveSettlementRetryInterval = previous })
	price := 0.1
	svc, base, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, base, 0, &price)
	store := &liveCheckpointFailStore{liveTestStore: base, failSave: true}
	ledger := &reviewLiveBillingLedger{}
	usage := &liveFirstInsertUsageRepo{}
	svc.cache = store
	svc.usageBillingRepo = ledger
	svc.usageLogRepo = usage
	svc.finalizeLiveCall(record)
	require.Zero(t, ledger.applied)
	require.NotNil(t, record.Settlement)
	frozen := *record.Settlement
	time.Sleep(20 * time.Millisecond)
	store.failSave = false
	svc.retryPendingLiveSettlement(record)
	current, err := base.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, 1, ledger.applied)
	require.Zero(t, ledger.conflicts)
	require.Equal(t, LiveSettlementSettled, current.Settlement.Status)
	require.True(t, current.Settlement.UsageLogWritten)
	require.Equal(t, frozen.DurationMs, current.Settlement.DurationMs)
	require.Equal(t, frozen.ActualCost, current.Settlement.ActualCost)
	require.Len(t, usage.rows, 1)
	require.InDelta(t, ledger.amount, usage.rows[record.CallHash].ActualCost, 0.000001)
}

type reviewLiveReadFailureStore struct {
	*liveTestStore
	beginUnavailable bool
	readHealthy      bool
	loseReply        bool
	readCalls        int
	beginCalls       int
}

func (s *reviewLiveReadFailureStore) GetLiveCall(ctx context.Context, hash string) (*LiveCallRecord, error) {
	s.readCalls++
	if s.readHealthy {
		return s.liveTestStore.GetLiveCall(ctx, hash)
	}
	return nil, errors.New("redis read unavailable")
}

func (s *reviewLiveReadFailureStore) BeginLiveSettlement(ctx context.Context, hash string, candidate *LiveSettlement, ttl time.Duration) (*LiveSettlement, string, bool, error) {
	if s.beginUnavailable {
		return nil, "", false, errors.New("redis atomic checkpoint unavailable")
	}
	return s.liveTestStore.BeginLiveSettlement(ctx, hash, candidate, ttl)
}

func (s *reviewLiveReadFailureStore) BeginLiveSettlementVerified(ctx context.Context, record *LiveCallRecord, candidate *LiveSettlement, ttl time.Duration) (*LiveSettlement, string, bool, error) {
	s.beginCalls++
	if s.beginUnavailable {
		return nil, "", false, errors.New("redis atomic checkpoint unavailable")
	}
	state, token, first, err := s.liveTestStore.BeginLiveSettlementVerified(ctx, record, candidate, ttl)
	if err == nil && s.loseReply {
		s.loseReply = false
		return nil, "", false, errors.New("begin reply lost")
	}
	return state, token, first, err
}

func TestReviewLiveSettlementReadFailureUsesConfirmedCheckpoint(t *testing.T) {
	price := 0.1
	svc, base, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, base, 0, &price)
	store := &reviewLiveReadFailureStore{liveTestStore: base}
	leases := &liveTestConcurrencyCache{}
	ledger := &reviewLiveBillingLedger{}
	usage := &liveFirstInsertUsageRepo{}
	svc.cache = store
	svc.concurrencyService = NewConcurrencyService(leases)
	svc.usageBillingRepo = ledger
	svc.usageLogRepo = usage
	svc.finalizeLiveCall(record)
	current, err := base.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, LiveControllerClosed, current.Controller)
	require.Equal(t, LiveSettlementSettled, current.Settlement.Status)
	require.True(t, current.Settlement.UsageLogWritten)
	require.Equal(t, 1, leases.releases)
	require.Equal(t, 1, ledger.applied)
	require.Len(t, usage.rows, 1)
	require.InDelta(t, ledger.amount, usage.rows[record.CallHash].ActualCost, 0.000001)
}

func TestReviewLiveSettlementReadAndCheckpointFailureDoesNotCharge(t *testing.T) {
	price := 0.1
	svc, base, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, base, 0, &price)
	store := &reviewLiveReadFailureStore{liveTestStore: base, beginUnavailable: true}
	ledger := &reviewLiveBillingLedger{}
	usage := &liveFirstInsertUsageRepo{}
	leases := &liveTestConcurrencyCache{}
	svc.cache = store
	svc.usageBillingRepo = ledger
	svc.usageLogRepo = usage
	svc.concurrencyService = NewConcurrencyService(leases)
	svc.finalizeLiveCall(record)
	require.Zero(t, ledger.applied)
	require.Zero(t, leases.releases)
	require.Empty(t, usage.rows)
	require.NotNil(t, record.Settlement)
	current, err := base.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Nil(t, current.Settlement)
}

func TestReviewLiveSettlementReadFailureRejectsIncompleteSnapshot(t *testing.T) {
	price := 0.1
	svc, base, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, base, 0, &price)
	ledger := &reviewLiveBillingLedger{}
	svc.cache = &reviewLiveReadFailureStore{liveTestStore: base}
	svc.usageBillingRepo = ledger
	incomplete := &LiveCallRecord{CallHash: record.CallHash, Settlement: &LiveSettlement{Status: LiveSettlementPending, ActualCost: 9, TotalCost: 9, FrozenAt: time.Now()}}
	svc.finalizeLiveCall(incomplete)
	require.Zero(t, ledger.applied)
	current, err := base.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, LiveControllerPending, current.Controller)
	require.Nil(t, current.Settlement)
}

type reviewLiveLegacyModeStore struct {
	*liveTestStore
	beginCalls int
	swapCalls  int
}

func (s *reviewLiveLegacyModeStore) BeginLiveSettlement(ctx context.Context, hash string, candidate *LiveSettlement, ttl time.Duration) (*LiveSettlement, string, bool, error) {
	s.beginCalls++
	if s.beginCalls > 1 {
		return nil, "", false, errors.New("subsequent checkpoint read unavailable")
	}
	return s.liveTestStore.BeginLiveSettlement(ctx, hash, candidate, ttl)
}

func (s *reviewLiveLegacyModeStore) CompareAndSwapLiveSettlement(ctx context.Context, hash, expected string, next *LiveSettlement, ttl time.Duration) (bool, error) {
	s.swapCalls++
	return s.liveTestStore.CompareAndSwapLiveSettlement(ctx, hash, expected, next, ttl)
}

func TestReviewLiveSettlementLegacyModeUsesUpdatedCASToken(t *testing.T) {
	price := 0.1
	svc, base, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, base, 0, &price)
	record.Settlement = &LiveSettlement{Status: LiveSettlementPending, ActualCost: 0.2, TotalCost: 0.2, DurationMs: 120000, FrozenAt: time.Now()}
	require.NoError(t, base.SaveLiveCall(context.Background(), record, time.Hour))
	store := &reviewLiveLegacyModeStore{liveTestStore: base}
	ledger := &reviewLiveBillingLedger{}
	usage := &liveFirstInsertUsageRepo{}
	svc.cache = store
	svc.usageBillingRepo = ledger
	svc.usageLogRepo = usage
	svc.finalizeLiveCall(record)
	current, err := base.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, 1, ledger.applied)
	require.Equal(t, 1, store.beginCalls, "successful CAS must yield the new token without a readback")
	require.Equal(t, 2, store.swapCalls)
	require.Equal(t, LiveSettlementSettled, current.Settlement.Status)
	require.True(t, current.Settlement.UsageLogWritten)
	require.Len(t, usage.rows, 1)
	require.InDelta(t, ledger.amount, usage.rows[record.CallHash].ActualCost, 0.000001)
}

func TestReviewLiveSettlementReadFailureAutomaticallyRecovers(t *testing.T) {
	previous := liveSettlementRetryInterval
	liveSettlementRetryInterval = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { liveSettlementRetryInterval = previous })
	price := 0.1
	svc, base, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, base, 0, &price)
	store := &reviewLiveReadFailureStore{liveTestStore: base}
	ledger := &reviewLiveBillingLedger{failRemaining: 1}
	usage := &liveUsageLogFailingRepo{failNext: true}
	svc.cache = store
	svc.usageBillingRepo = ledger
	svc.usageLogRepo = usage
	svc.finalizeLiveCall(record)
	require.NotNil(t, record.Settlement)
	frozen := *record.Settlement
	svc.retryPendingLiveSettlement(record)
	current, err := base.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, 2, ledger.attempts)
	require.Equal(t, 1, ledger.applied)
	require.Zero(t, ledger.conflicts)
	require.Equal(t, LiveSettlementSettled, current.Settlement.Status)
	require.True(t, current.Settlement.UsageLogWritten)
	require.Equal(t, frozen.DurationMs, current.Settlement.DurationMs)
	require.Len(t, usage.rows, 1)
	require.InDelta(t, ledger.amount, usage.rows[record.CallHash].ActualCost, 0.000001)
}

func TestReviewLiveSettlementReadAndCheckpointFailureKeepsBudget(t *testing.T) {
	previous := liveSettlementRetryInterval
	backoffs := 0
	liveSettlementRetryInterval = func(int) time.Duration { backoffs++; return time.Millisecond }
	t.Cleanup(func() { liveSettlementRetryInterval = previous })
	price := 0.1
	svc, base, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, base, 0, &price)
	store := &reviewLiveReadFailureStore{liveTestStore: base, beginUnavailable: true}
	ledger := &reviewLiveBillingLedger{}
	svc.cache = store
	svc.usageBillingRepo = ledger
	svc.finalizeLiveCall(record)
	svc.retryPendingLiveSettlement(record)
	require.Equal(t, liveSettlementRetryAttempts, backoffs, "actual retry backoff must remain bounded")
	require.Equal(t, 2+2*liveSettlementRetryAttempts, store.readCalls)
	require.Equal(t, 2*(1+liveSettlementRetryAttempts), store.beginCalls)
	require.Zero(t, ledger.applied)
}

func TestReviewLiveSettlementReadFailureRejectsChangedIdentity(t *testing.T) {
	price := 0.1
	svc, base, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, base, 0, &price)
	record.UserID++
	store := &reviewLiveReadFailureStore{liveTestStore: base}
	ledger := &reviewLiveBillingLedger{}
	svc.cache = store
	svc.usageBillingRepo = ledger
	svc.finalizeLiveCall(record)
	current, err := base.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Zero(t, ledger.applied)
	require.Equal(t, LiveControllerPending, current.Controller)
	require.Nil(t, current.Settlement)
}

func TestReviewLiveSettlementReadFailureLostReplyAndNilPolicy(t *testing.T) {
	price := 0.1
	svc, base, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, base, 0, &price)
	record.BillingPolicy = nil
	require.NoError(t, base.SaveLiveCall(context.Background(), record, time.Hour))
	store := &reviewLiveReadFailureStore{liveTestStore: base, loseReply: true}
	ledger := &reviewLiveBillingLedger{}
	leases := &liveTestConcurrencyCache{}
	svc.cache = store
	svc.usageBillingRepo = ledger
	svc.concurrencyService = NewConcurrencyService(leases)
	svc.finalizeLiveCall(record)
	current, err := base.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, 2, store.beginCalls)
	require.Equal(t, 1, ledger.applied)
	require.Equal(t, 1, leases.releases)
	require.Equal(t, LiveSettlementSettled, current.Settlement.Status)
}

type reviewLiveConcurrentFailureLedger struct {
	UsageBillingRepository
	mu    sync.Mutex
	calls int
	both  chan struct{}
}

func (r *reviewLiveConcurrentFailureLedger) Apply(_ context.Context, _ *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	r.mu.Lock()
	r.calls++
	if r.calls == 2 {
		close(r.both)
	}
	r.mu.Unlock()
	<-r.both
	return nil, errors.New("temporary ledger outage")
}

func TestReviewLiveSettlementConcurrentFailuresCountOnceEach(t *testing.T) {
	price := 0.1
	svc, base, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, base, 0, &price)
	ledger := &reviewLiveConcurrentFailureLedger{both: make(chan struct{})}
	audit := &liveRecordingOpsRepo{}
	svc.usageBillingRepo = ledger
	svc.SetOpsRepository(audit)
	svc2 := *svc
	first := *record
	second := *record
	done1 := make(chan struct{})
	done2 := make(chan struct{})
	go func() { svc.finalizeLiveCall(&first); close(done1) }()
	go func() { svc2.finalizeLiveCall(&second); close(done2) }()
	reviewWaitLiveSignal(t, done1)
	reviewWaitLiveSignal(t, done2)
	current, err := base.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, LiveSettlementPending, current.Settlement.Status)
	entries := audit.snapshot()
	if current.Settlement.Attempts != 2 {
		t.Errorf("two failed billing attempts must both be retained: got %d", current.Settlement.Attempts)
	}
	if len(entries) != 1 {
		t.Errorf("one session must produce one pending audit row: got %d", len(entries))
	}
}

type reviewLiveFailureReplyStore struct {
	*liveTestStore
	lost bool
}

func (s *reviewLiveFailureReplyStore) CompareAndSwapLiveSettlement(ctx context.Context, hash, expected string, next *LiveSettlement, ttl time.Duration) (bool, error) {
	ok, err := s.liveTestStore.CompareAndSwapLiveSettlement(ctx, hash, expected, next, ttl)
	if err == nil && ok && next.Status == LiveSettlementPending && next.Attempts > 0 && s.lost {
		s.lost = false
		return false, errors.New("failure CAS reply lost")
	}
	return ok, err
}

func TestReviewLiveSettlementFailureCASLostReplyDoesNotDuplicate(t *testing.T) {
	price := 0.1
	svc, base, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, base, 0, &price)
	store := &reviewLiveFailureReplyStore{liveTestStore: base, lost: true}
	ledger := &reviewLiveBillingLedger{failRemaining: 1}
	audit := &liveRecordingOpsRepo{}
	svc.cache = store
	svc.usageBillingRepo = ledger
	svc.SetOpsRepository(audit)
	svc.finalizeLiveCall(record)
	current, err := base.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, LiveSettlementPending, current.Settlement.Status)
	require.Equal(t, 1, current.Settlement.Attempts)
	require.Zero(t, ledger.applied)
	require.Len(t, audit.snapshot(), 1, "an uncertain CAS still needs a best-effort pending audit")
}

type reviewLiveMixedLedger struct {
	UsageBillingRepository
	mu           sync.Mutex
	calls        int
	firstEntered chan struct{}
	releaseFirst chan struct{}
}

func (r *reviewLiveMixedLedger) Apply(_ context.Context, _ *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	r.mu.Lock()
	r.calls++
	call := r.calls
	r.mu.Unlock()
	if call == 1 {
		close(r.firstEntered)
		<-r.releaseFirst
		return nil, errors.New("temporary ledger outage")
	}
	return &UsageBillingApplyResult{Applied: true}, nil
}

func TestReviewLiveSettlementConcurrentFailureCannotReopenSettled(t *testing.T) {
	price := 0.1
	svc, base, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, base, 0, &price)
	ledger := &reviewLiveMixedLedger{firstEntered: make(chan struct{}), releaseFirst: make(chan struct{})}
	audit := &liveRecordingOpsRepo{}
	usage := &liveFirstInsertUsageRepo{}
	svc.usageBillingRepo = ledger
	svc.usageLogRepo = usage
	svc.SetOpsRepository(audit)
	svc2 := *svc
	first, second := *record, *record
	done1, done2 := make(chan struct{}), make(chan struct{})
	go func() { svc.finalizeLiveCall(&first); close(done1) }()
	reviewWaitLiveSignal(t, ledger.firstEntered)
	go func() { svc2.finalizeLiveCall(&second); close(done2) }()
	reviewWaitLiveSignal(t, done2)
	close(ledger.releaseFirst)
	reviewWaitLiveSignal(t, done1)
	current, err := base.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, LiveSettlementSettled, current.Settlement.Status)
	require.True(t, current.Settlement.UsageLogWritten)
	require.Len(t, usage.rows, 1)
	require.Empty(t, audit.snapshot(), "a late failure must not publish a pending audit after settlement")
}

func TestReviewLiveSettlementFailureCASLostReplyStillAudits(t *testing.T) {
	previous := liveSettlementRetryInterval
	liveSettlementRetryInterval = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { liveSettlementRetryInterval = previous })
	price := 0.1
	svc, base, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, base, 0, &price)
	store := &reviewLiveFailureReplyStore{liveTestStore: base, lost: true}
	ledger := &reviewLiveBillingLedger{failRemaining: 100}
	audit := &liveRecordingOpsRepo{}
	svc.cache = store
	svc.usageBillingRepo = ledger
	svc.SetOpsRepository(audit)
	svc.finalizeLiveCall(record)
	svc.retryPendingLiveSettlement(record)
	current, err := base.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, LiveSettlementPending, current.Settlement.Status)
	require.Zero(t, ledger.applied)
	require.GreaterOrEqual(t, len(audit.snapshot()), 1, "a lost CAS reply must not silence every pending audit")
}

func TestReviewLiveSettlementRejectedCandidateCannotBypassOnRecovery(t *testing.T) {
	for _, beginUnavailable := range []bool{false, true} {
		t.Run(fmt.Sprintf("begin_unavailable_%t", beginUnavailable), func(t *testing.T) {
			previous := liveSettlementRetryInterval
			liveSettlementRetryInterval = func(int) time.Duration { return time.Millisecond }
			t.Cleanup(func() { liveSettlementRetryInterval = previous })
			oldPrice, newPrice := 0.1, 0.2
			svc, base, _, _ := newLiveSettlementTestService()
			local := newLiveSettlementRecord(t, base, 0, &oldPrice)
			persisted := *local
			persisted.RealtimePricePerMin = &newPrice
			require.NoError(t, base.SaveLiveCall(context.Background(), &persisted, time.Hour))
			store := &reviewLiveReadFailureStore{liveTestStore: base, beginUnavailable: beginUnavailable}
			ledger := &reviewLiveBillingLedger{}
			svc.cache = store
			svc.usageBillingRepo = ledger
			svc.finalizeLiveCall(local)
			require.Zero(t, ledger.applied)
			store.readHealthy = true
			store.beginUnavailable = false
			svc.retryPendingLiveSettlement(local)
			if local.settlementIdentityRejected {
				svc.finalizeLiveCall(local)
			}
			current, err := base.GetLiveCall(context.Background(), local.CallHash)
			require.NoError(t, err)
			require.Zero(t, ledger.applied, "unverified old price must not be charged after read recovery")
			require.Nil(t, current.Settlement, "unverified old candidate must not be frozen")
		})
	}
}

func TestReviewLiveSettlementBareCandidateCannotBypassOnRecovery(t *testing.T) {
	previous := liveSettlementRetryInterval
	liveSettlementRetryInterval = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { liveSettlementRetryInterval = previous })
	price := 0.1
	svc, base, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, base, 0, &price)
	bare := &LiveCallRecord{CallHash: record.CallHash, Settlement: &LiveSettlement{Status: LiveSettlementPending, ActualCost: 9, TotalCost: 9, FrozenAt: time.Now()}}
	store := &reviewLiveReadFailureStore{liveTestStore: base}
	ledger := &reviewLiveBillingLedger{}
	svc.cache = store
	svc.usageBillingRepo = ledger
	svc.finalizeLiveCall(bare)
	store.readHealthy = true
	svc.retryPendingLiveSettlement(bare)
	current, err := base.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Zero(t, ledger.applied)
	require.Nil(t, current.Settlement)
}

type reviewLiveFlakyOpsRepo struct {
	OpsRepository
	mu      sync.Mutex
	calls   int
	entries []*OpsInsertErrorLogInput
}

func (r *reviewLiveFlakyOpsRepo) InsertErrorLog(_ context.Context, input *OpsInsertErrorLogInput) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.calls == 1 {
		return 0, errors.New("ops temporarily unavailable")
	}
	copy := *input
	r.entries = append(r.entries, &copy)
	return int64(len(r.entries)), nil
}
func TestReviewLiveSettlementAuditRetriesAfterOpsFailure(t *testing.T) {
	price := 0.1
	svc, base, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, base, 0, &price)
	ledger := &reviewLiveBillingLedger{failRemaining: 2}
	audit := &reviewLiveFlakyOpsRepo{}
	svc.usageBillingRepo = ledger
	svc.SetOpsRepository(audit)
	svc.finalizeLiveCall(record)
	svc.finalizeLiveCall(record)
	require.Equal(t, 2, audit.calls)
	require.Len(t, audit.entries, 1)
	require.Zero(t, ledger.applied)
	current, err := base.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, LiveSettlementPending, current.Settlement.Status)
	require.Equal(t, 2, current.Settlement.Attempts)
}

func TestReviewLiveSettlementUnconfirmedCandidateRecoversWithMatchingIdentity(t *testing.T) {
	previous := liveSettlementRetryInterval
	liveSettlementRetryInterval = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { liveSettlementRetryInterval = previous })
	price := 0.1
	svc, base, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, base, 0, &price)
	store := &reviewLiveReadFailureStore{liveTestStore: base, beginUnavailable: true}
	ledger := &reviewLiveBillingLedger{}
	svc.cache, svc.usageBillingRepo = store, ledger
	svc.finalizeLiveCall(record)
	require.NotNil(t, record.Settlement)
	frozen := *record.Settlement
	require.Zero(t, ledger.applied)
	store.readHealthy, store.beginUnavailable = true, false
	svc.retryPendingLiveSettlement(record)
	current, err := base.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, 1, ledger.applied)
	require.Equal(t, frozen.DurationMs, current.Settlement.DurationMs)
	require.Equal(t, frozen.ActualCost, current.Settlement.ActualCost)
}

type reviewLiveAuditFinishStore struct {
	*liveTestStore
	failRelease bool
	loseSuccess bool
	loseClaim   bool
}

func (s *reviewLiveAuditFinishStore) ClaimLiveSettlementAudit(ctx context.Context, hash, owner string, ttl time.Duration) (bool, error) {
	claimed, err := s.liveTestStore.ClaimLiveSettlementAudit(ctx, hash, owner, ttl)
	if err == nil && claimed && s.loseClaim {
		s.loseClaim = false
		return false, errors.New("claim response lost")
	}
	return claimed, err
}
func (s *reviewLiveAuditFinishStore) FinishLiveSettlementAudit(ctx context.Context, hash, owner string, written bool) (bool, error) {
	if !written && s.failRelease {
		s.failRelease = false
		return false, errors.New("claim release unavailable")
	}
	done, err := s.liveTestStore.FinishLiveSettlementAudit(ctx, hash, owner, written)
	if err == nil && done && written && s.loseSuccess {
		s.loseSuccess = false
		return false, errors.New("claim completion response lost")
	}
	return done, err
}
func TestReviewLiveSettlementAuditReleaseFailureExpiresAndRecovers(t *testing.T) {
	price := 0.1
	svc, base, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, base, 0, &price)
	store := &reviewLiveAuditFinishStore{liveTestStore: base, failRelease: true}
	ledger := &reviewLiveBillingLedger{failRemaining: 3}
	audit := &reviewLiveFlakyOpsRepo{}
	svc.cache, svc.usageBillingRepo = store, ledger
	svc.SetOpsRepository(audit)
	svc.finalizeLiveCall(record)
	svc.finalizeLiveCall(record)
	require.Equal(t, 1, audit.calls, "active claim prevents a competing write")
	reviewLiveAuditClaims.Lock()
	reviewLiveAuditClaims.entries[base].until = time.Now().Add(-time.Second)
	reviewLiveAuditClaims.Unlock()
	svc.finalizeLiveCall(record)
	require.Equal(t, 2, audit.calls)
	require.Len(t, audit.entries, 1)
	require.Zero(t, ledger.applied)
}
func TestReviewLiveSettlementAuditLostRepliesRemainBounded(t *testing.T) {
	price := 0.1
	svc, base, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, base, 0, &price)
	store := &reviewLiveAuditFinishStore{liveTestStore: base, loseClaim: true, loseSuccess: true}
	ledger := &reviewLiveBillingLedger{failRemaining: 2}
	audit := &liveRecordingOpsRepo{}
	svc.cache, svc.usageBillingRepo = store, ledger
	svc.SetOpsRepository(audit)
	svc.finalizeLiveCall(record)
	svc.finalizeLiveCall(record)
	require.Len(t, audit.snapshot(), 1, "same-owner claim retry and confirmed completion must not duplicate")
	require.Zero(t, ledger.applied)
}

func TestReviewLiveSettlementUnconfirmedCandidateRejectsChangedBillingIdentity(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*LiveCallRecord)
	}{
		{"user", func(r *LiveCallRecord) { r.UserID++ }},
		{"account", func(r *LiveCallRecord) { r.AccountID++ }},
		{"key", func(r *LiveCallRecord) { r.APIKeyID++ }},
		{"group", func(r *LiveCallRecord) { r.GroupID++ }},
		{"subscription", func(r *LiveCallRecord) { r.SubscriptionID++ }},
		{"lease", func(r *LiveCallRecord) { r.LeaseID += "x" }},
		{"model", func(r *LiveCallRecord) { r.Model += "x" }},
		{"creation", func(r *LiveCallRecord) { r.CreatedAt = r.CreatedAt.Add(time.Millisecond) }},
		{"expiry", func(r *LiveCallRecord) { r.ExpiresAt = r.ExpiresAt.Add(time.Millisecond) }},
		{"policy", func(r *LiveCallRecord) { r.BillingPolicy = &LiveBillingPolicy{APIKeyQuota: 3} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			price := 0.1
			svc, base, _, _ := newLiveSettlementTestService()
			local := newLiveSettlementRecord(t, base, 0, &price)
			persisted := *local
			tc.mutate(&persisted)
			require.NoError(t, base.SaveLiveCall(context.Background(), &persisted, time.Hour))
			store := &reviewLiveReadFailureStore{liveTestStore: base, beginUnavailable: true}
			ledger := &reviewLiveBillingLedger{}
			svc.cache, svc.usageBillingRepo = store, ledger
			svc.finalizeLiveCall(local)
			store.readHealthy = true
			store.beginUnavailable = false
			svc.retryPendingLiveSettlement(local)
			current, err := base.GetLiveCall(context.Background(), local.CallHash)
			require.NoError(t, err)
			require.Zero(t, ledger.applied)
			require.Nil(t, current.Settlement)
		})
	}
}
