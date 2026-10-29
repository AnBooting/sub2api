package repository

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type lostHandoffReply struct {
	delegate service.LiveHandoffStore
	lose     bool
}

func (s *lostHandoffReply) BeginLiveSettlementVerifiedV2(ctx context.Context, record *service.LiveCallRecord, state *service.LiveSettlement, identity, digest string, generation int64) (service.LiveHandoffBeginResult, error) {
	result, err := s.delegate.BeginLiveSettlementVerifiedV2(ctx, record, state, identity, digest, generation)
	if err == nil && s.lose {
		s.lose = false
		return service.LiveHandoffBeginResult{}, errors.New("simulated lost Redis reply")
	}
	return result, err
}

type handoffBoundLookup struct {
	reservation service.LiveHandoffBoundReservation
	err         error
}

func (l handoffBoundLookup) LookupBoundLiveHandoff(_ context.Context, hash string, apiKeyID int64) (service.LiveHandoffBoundReservation, error) {
	if l.err != nil {
		return service.LiveHandoffBoundReservation{}, l.err
	}
	if l.reservation.CallHash != hash || l.reservation.APIKeyID != apiKeyID {
		return service.LiveHandoffBoundReservation{}, errors.New("not found")
	}
	return l.reservation, nil
}

func newHandoffV2Fixture(t *testing.T, id string, generation int64) (*miniredis.Miniredis, *redis.Client, *gatewayCache, *service.LiveCallRecord, handoffBoundLookup) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewGatewayCache(client).(*gatewayCache)
	price := 0.1
	created := time.Date(2026, 1, 2, 3, 4, 5, 123000000, time.UTC)
	record := &service.LiveCallRecord{
		CallID: id, CallHash: HashLiveCallID(id), UserID: 1, APIKeyID: 2,
		AccountID: 3, GroupID: 4, SubscriptionID: 5, LeaseID: "lease-" + id,
		Model: "gpt-live", CreatedAt: created, ExpiresAt: created.Add(time.Hour),
		Controller: service.LiveControllerPending, RealtimePricePerMin: &price,
		BillingPolicy: &service.LiveBillingPolicy{RateLimit5h: 100},
		UserAgent:     "ua", IPAddress: "127.0.0.1", InboundEndpoint: "/live",
		AttestationCiphertext: "encrypted-attestation",
	}
	_, fp, err := service.LiveSettlementIdentityFromRecord(record)
	require.NoError(t, err)
	lookup := handoffBoundLookup{reservation: service.LiveHandoffBoundReservation{
		OperationID: "operation-" + id, CallHash: record.CallHash, APIKeyID: record.APIKeyID,
		IdentityFingerprint: fp, Generation: generation, Status: "bound",
	}}
	return server, client, cache, record, lookup
}

func confirmV2Creation(t *testing.T, cache *gatewayCache, record *service.LiveCallRecord, lookup handoffBoundLookup, generation int64, ttl time.Duration) service.ConfirmedLiveHandoffCreation {
	t.Helper()
	proof, err := service.ConfirmLiveHandoffCreation(context.Background(), lookup, cache, record, generation, ttl)
	require.NoError(t, err)
	return proof
}

func handoffFrozenDigest(t *testing.T, record *service.LiveCallRecord, state *service.LiveSettlement) (string, string) {
	t.Helper()
	identity, fp, err := service.LiveSettlementIdentityFromRecord(record)
	require.NoError(t, err)
	snapshot := service.LiveFrozenSnapshot{Version: 1, Identity: identity, IdentityFingerprint: fp,
		Frozen: service.LiveFrozenFact{TotalCost: state.TotalCost, ActualCost: state.ActualCost,
			BillingMode: state.BillingMode, SettlementMode: state.SettlementMode,
			DurationMs: state.DurationMs, FrozenAt: state.FrozenAt.UTC()}}
	digest, err := snapshot.ImmutableDigest()
	require.NoError(t, err)
	return fp, digest
}

func TestLiveHandoffV2CreationProofAndNoResurrection(t *testing.T) {
	ctx := context.Background()
	const generation int64 = 1<<53 + 1
	server, _, cache, record, lookup := newHandoffV2Fixture(t, "create", generation)
	var empty service.ConfirmedLiveHandoffCreation
	_, err := empty.Validate()
	require.Error(t, err)
	proof := confirmV2Creation(t, cache, record, lookup, generation, time.Hour)
	data, err := proof.Validate()
	require.NoError(t, err)
	require.Equal(t, lookup.reservation.OperationID, data.OperationID)
	require.Equal(t, record.CallHash, data.CallHash)
	require.Equal(t, generation, data.Generation)
	require.Equal(t, 2, data.ProtocolVersion)
	data.CallHash = "changed"
	original, err := proof.Validate()
	require.NoError(t, err)
	require.Equal(t, record.CallHash, original.CallHash, "proof read must not change its stored value")

	server.FastForward(10 * time.Minute)
	ttl := server.TTL(liveCallKey(record.CallHash))
	require.Equal(t, 50*time.Minute, ttl)
	confirmV2Creation(t, cache, record, lookup, generation, time.Hour)
	require.Equal(t, ttl, server.TTL(liveCallKey(record.CallHash)), "confirmed replay must not extend the original TTL")

	bad := lookup
	bad.reservation.Status = "active"
	_, err = service.ConfirmLiveHandoffCreation(ctx, bad, cache, record, generation, time.Hour)
	require.Error(t, err)
	server.FastForward(time.Hour)
	require.False(t, server.Exists(liveCallKey(record.CallHash)))
	require.True(t, server.Exists(liveHandoffGuardKey(record.CallHash)))
	require.Error(t, cache.SaveLiveCall(ctx, record, time.Hour), "v1 late Save must not resurrect v2")
	_, err = service.ConfirmLiveHandoffCreation(ctx, lookup, cache, record, generation, time.Hour)
	require.Error(t, err, "v2 create replay must not resurrect an expired main key")
	require.False(t, server.Exists(liveCallKey(record.CallHash)))
}

func TestLiveHandoffV2ConcurrentWinnerAndGeneration(t *testing.T) {
	ctx := context.Background()
	const generation int64 = 1<<53 + 1
	server, _, cache, record, lookup := newHandoffV2Fixture(t, "winner", generation)
	confirmV2Creation(t, cache, record, lookup, generation, time.Hour)
	frozenAt := time.Date(2026, 1, 2, 3, 5, 0, 123456789, time.UTC)
	candidates := []*service.LiveSettlement{
		{Status: service.LiveSettlementPending, TotalCost: .1, ActualCost: .1, DurationMs: 100, FrozenAt: frozenAt, SettlementMode: "standard"},
		{Status: service.LiveSettlementPending, TotalCost: .2, ActualCost: .2, DurationMs: 200, FrozenAt: frozenAt.Add(time.Second), SettlementMode: "standard"},
	}
	type outcome struct {
		value service.LiveHandoffBeginResult
		err   error
	}
	out := make([]outcome, 2)
	var wg sync.WaitGroup
	gate := make(chan struct{})
	for i := range candidates {
		fp, digest := handoffFrozenDigest(t, record, candidates[i])
		wg.Add(1)
		go func(i int, fp, digest string) {
			defer wg.Done()
			<-gate
			out[i].value, out[i].err = cache.BeginLiveSettlementVerifiedV2(ctx, record, candidates[i], fp, digest, generation)
		}(i, fp, digest)
	}
	close(gate)
	wg.Wait()
	require.NoError(t, out[0].err)
	require.NoError(t, out[1].err)
	require.Equal(t, out[0].value, out[1].value)
	require.Equal(t, generation, out[0].value.Generation)
	var decoded service.LiveSettlement
	require.NoError(t, json.Unmarshal([]byte(out[0].value.Token), &decoded))
	require.Equal(t, out[0].value.Winner, &decoded)
	_, winnerDigest := handoffFrozenDigest(t, record, out[0].value.Winner)
	require.Equal(t, winnerDigest, out[0].value.FrozenDigest)
	require.Zero(t, server.TTL(liveCallKey(record.CallHash)), "pending frozen record must be persistent")
	server.FastForward(48 * time.Hour)
	require.True(t, server.Exists(liveCallKey(record.CallHash)))
	fp, digest := handoffFrozenDigest(t, record, candidates[0])
	_, err := cache.BeginLiveSettlementVerifiedV2(ctx, record, candidates[0], fp, digest, generation+1)
	require.Error(t, err, "adjacent bigints must not compare equal")
}

func TestLiveHandoffV2AckAndLegacyWriters(t *testing.T) {
	ctx := context.Background()
	const generation int64 = 1<<53 + 1
	server, client, cache, record, lookup := newHandoffV2Fixture(t, "ack", generation)
	confirmV2Creation(t, cache, record, lookup, generation, time.Hour)
	state := &service.LiveSettlement{Status: service.LiveSettlementPending, ActualCost: .1, TotalCost: .1, DurationMs: 123, FrozenAt: time.Now().UTC(), SettlementMode: "standard"}
	fp, digest := handoffFrozenDigest(t, record, state)
	result, err := cache.BeginLiveSettlementVerifiedV2(ctx, record, state, fp, digest, generation)
	require.NoError(t, err)
	require.Error(t, cache.SaveLiveCall(ctx, record, time.Hour))
	_, err = cache.MarkLiveCallClosed(ctx, record.CallHash, time.Hour)
	require.Error(t, err)
	_, _, _, err = cache.BeginLiveSettlement(ctx, record.CallHash, state, time.Hour)
	require.Error(t, err)
	_, err = cache.CompareAndSwapLiveSettlement(ctx, record.CallHash, result.Token, state, time.Hour)
	require.Error(t, err)
	require.Zero(t, server.TTL(liveCallKey(record.CallHash)))

	ref := service.LiveHandoffAckReference{CallHash: record.CallHash, IdentityFingerprint: fp, FrozenDigest: digest, Generation: generation}
	wrong := ref
	wrong.Generation++
	require.Error(t, cache.AcknowledgeLiveHandoffV2(ctx, wrong, time.Hour))
	wrong = ref
	wrong.FrozenDigest = "bad"
	require.Error(t, cache.AcknowledgeLiveHandoffV2(ctx, wrong, time.Hour))
	wrong = ref
	wrong.IdentityFingerprint = "bad"
	require.Error(t, cache.AcknowledgeLiveHandoffV2(ctx, wrong, time.Hour))
	require.Zero(t, server.TTL(liveCallKey(record.CallHash)))
	require.NoError(t, cache.AcknowledgeLiveHandoffV2(ctx, ref, time.Hour))
	require.Equal(t, time.Hour, server.TTL(liveCallKey(record.CallHash)))
	server.FastForward(10 * time.Minute)
	require.NoError(t, cache.AcknowledgeLiveHandoffV2(ctx, ref, time.Hour))
	require.Equal(t, 50*time.Minute, server.TTL(liveCallKey(record.CallHash)), "ACK replay must not extend TTL")
	require.NoError(t, client.Persist(ctx, liveCallKey(record.CallHash)).Err())
	require.NoError(t, cache.AcknowledgeLiveHandoffV2(ctx, ref, time.Hour))
	require.Equal(t, time.Hour, server.TTL(liveCallKey(record.CallHash)), "acked record without TTL must recover it")
	server.FastForward(2 * time.Hour)
	require.Error(t, cache.AcknowledgeLiveHandoffV2(ctx, ref, time.Hour))
	require.False(t, server.Exists(liveCallKey(record.CallHash)))
}

func TestLiveHandoffV2RejectsForgedBoundAndCandidate(t *testing.T) {
	ctx := context.Background()
	server, _, cache, record, lookup := newHandoffV2Fixture(t, "forged", 8)
	cases := []func(*service.LiveHandoffBoundReservation){
		func(r *service.LiveHandoffBoundReservation) { r.Generation++ },
		func(r *service.LiveHandoffBoundReservation) { r.IdentityFingerprint = "different" },
		func(r *service.LiveHandoffBoundReservation) { r.CallHash = HashLiveCallID("other") },
		func(r *service.LiveHandoffBoundReservation) { r.APIKeyID++ },
		func(r *service.LiveHandoffBoundReservation) { r.Status = "quarantine" },
	}
	for _, mutate := range cases {
		bad := lookup
		mutate(&bad.reservation)
		_, err := service.ConfirmLiveHandoffCreation(ctx, bad, cache, record, 8, time.Hour)
		require.Error(t, err)
		require.False(t, server.Exists(liveCallKey(record.CallHash)))
		require.False(t, server.Exists(liveHandoffGuardKey(record.CallHash)))
	}
	confirmV2Creation(t, cache, record, lookup, 8, time.Hour)
	state := &service.LiveSettlement{Status: service.LiveSettlementPending, TotalCost: .1, ActualCost: .1, DurationMs: 100, FrozenAt: time.Now().UTC(), SettlementMode: "standard"}
	fp, digest := handoffFrozenDigest(t, record, state)
	_, err := cache.BeginLiveSettlementVerifiedV2(ctx, record, state, fp, "bad-"+digest, 8)
	require.Error(t, err, "unverified candidate digest must not poison the first freeze")
	require.Greater(t, server.TTL(liveCallKey(record.CallHash)), time.Duration(0))
	changed := *record
	changed.UserID++
	_, err = cache.BeginLiveSettlementVerifiedV2(ctx, &changed, state, fp, digest, 8)
	require.Error(t, err, "the candidate identity must still match the bound record")
	result, err := cache.BeginLiveSettlementVerifiedV2(ctx, record, state, fp, digest, 8)
	require.NoError(t, err)
	require.Equal(t, digest, result.FrozenDigest)
}

func TestLiveHandoffV2ControllerFencesGeneration(t *testing.T) {
	ctx := context.Background()
	const generation int64 = 1<<53 + 1
	_, _, cache, record, lookup := newHandoffV2Fixture(t, "controller", generation)
	confirmV2Creation(t, cache, record, lookup, generation, time.Hour)
	_, err := cache.ClaimLiveController(ctx, record.CallHash, service.LiveControllerProxy, "owner")
	require.Error(t, err, "legacy controller operation has no generation")
	_, err = cache.ClaimLiveControllerV2(ctx, record.CallHash, service.LiveControllerProxy, "owner", generation+1)
	require.Error(t, err, "adjacent generations above 2^53 must differ")
	claimed, err := cache.ClaimLiveControllerV2(ctx, record.CallHash, service.LiveControllerProxy, "owner", generation)
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = cache.ReleaseLiveControllerV2(ctx, record.CallHash, "owner", generation+1)
	require.Error(t, err)
	released, err := cache.ReleaseLiveControllerV2(ctx, record.CallHash, "owner", generation)
	require.NoError(t, err)
	require.True(t, released)
}

func TestLiveHandoffV2RejectsUnexpectedOptionalField(t *testing.T) {
	ctx := context.Background()
	server, client, cache, record, lookup := newHandoffV2Fixture(t, "unexpected-policy", 29)
	record.BillingPolicy = nil
	_, fp, err := service.LiveSettlementIdentityFromRecord(record)
	require.NoError(t, err)
	lookup.reservation.IdentityFingerprint = fp
	confirmV2Creation(t, cache, record, lookup, 29, time.Hour)
	require.NoError(t, client.HSet(ctx, liveCallKey(record.CallHash), "billing_policy", "{}").Err())
	_, err = service.ConfirmLiveHandoffCreation(ctx, lookup, cache, record, 29, time.Hour)
	require.Error(t, err, "creation proof must reject an unexpected present policy")
	state := &service.LiveSettlement{Status: service.LiveSettlementPending, TotalCost: .1, ActualCost: .1, DurationMs: 100, FrozenAt: time.Now().UTC(), SettlementMode: "standard"}
	_, digest := handoffFrozenDigest(t, record, state)
	_, err = cache.BeginLiveSettlementVerifiedV2(ctx, record, state, fp, digest, 29)
	require.Error(t, err, "nil policy is distinct from a present policy")
	require.Greater(t, server.TTL(liveCallKey(record.CallHash)), time.Duration(0))
}

func TestLiveHandoffV2RejectsExpiringGuard(t *testing.T) {
	ctx := context.Background()
	server, client, cache, record, lookup := newHandoffV2Fixture(t, "expiring-guard", 31)
	confirmV2Creation(t, cache, record, lookup, 31, time.Hour)
	require.NoError(t, client.PExpire(ctx, liveHandoffGuardKey(record.CallHash), time.Minute).Err())
	_, err := service.ConfirmLiveHandoffCreation(ctx, lookup, cache, record, 31, time.Hour)
	require.Error(t, err, "creation proof requires a persistent guard")
	state := &service.LiveSettlement{Status: service.LiveSettlementPending, TotalCost: .1, ActualCost: .1, DurationMs: 100, FrozenAt: time.Now().UTC(), SettlementMode: "standard"}
	fp, digest := handoffFrozenDigest(t, record, state)
	_, err = cache.BeginLiveSettlementVerifiedV2(ctx, record, state, fp, digest, 31)
	require.Error(t, err, "freezing must not proceed with an expiring guard")
	require.Greater(t, server.TTL(liveCallKey(record.CallHash)), time.Duration(0))
}

func TestLiveHandoffV2ServiceRetryUsesFrozenWinner(t *testing.T) {
	ctx := context.Background()
	server, _, cache, record, lookup := newHandoffV2Fixture(t, "lost-reply", 23)
	record.BillingPolicy.RateLimit5h = math.Copysign(0, -1)
	_, fp, err := service.LiveSettlementIdentityFromRecord(record)
	require.NoError(t, err)
	lookup.reservation.IdentityFingerprint = fp
	confirmV2Creation(t, cache, record, lookup, 23, time.Hour)
	firstCost := *record.RealtimePricePerMin * (float64(100) / 60000)
	secondCost := *record.RealtimePricePerMin * (float64(900) / 60000)
	first := &service.LiveSettlement{Status: service.LiveSettlementPending, TotalCost: firstCost, ActualCost: firstCost, DurationMs: 100, FrozenAt: time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC), BillingMode: string(service.BillingModePerRequest), SettlementMode: "simple_skip"}
	second := *first
	second.TotalCost, second.ActualCost, second.DurationMs = secondCost, secondCost, 900
	second.FrozenAt = first.FrozenAt.Add(time.Minute)
	store := &lostHandoffReply{delegate: cache, lose: true}
	gateway := &service.OpenAIGatewayService{}
	_, err = gateway.ConfirmLiveFrozen(ctx, store, record, first, 23)
	require.Error(t, err, "the Redis result was lost after committing the first freeze")
	confirmed, err := gateway.ConfirmLiveFrozen(ctx, store, record, &second, 23)
	require.NoError(t, err)
	snapshot, err := confirmed.ValidatedSnapshot()
	require.NoError(t, err)
	require.Equal(t, first.TotalCost, snapshot.Frozen.TotalCost)
	require.Equal(t, first.ActualCost, snapshot.Frozen.ActualCost)
	require.Equal(t, first.DurationMs, snapshot.Frozen.DurationMs)
	require.True(t, first.FrozenAt.Equal(snapshot.Frozen.FrozenAt))
	require.Equal(t, float64(0), snapshot.Usage.ActualCost)
	require.Nil(t, snapshot.Billing)
	server.FastForward(48 * time.Hour)
	require.True(t, server.Exists(liveCallKey(record.CallHash)))
}

func TestLiveHandoffV2GuardOnlyIsInspectable(t *testing.T) {
	ctx := context.Background()
	server, client, cache, record, lookup := newHandoffV2Fixture(t, "guard-only", 19)
	confirmV2Creation(t, cache, record, lookup, 19, time.Hour)
	require.NoError(t, client.Del(ctx, liveCallKey(record.CallHash)).Err())
	require.True(t, server.Exists(liveHandoffGuardKey(record.CallHash)))
	status, err := cache.InspectLiveHandoffV2(ctx, record.CallHash)
	require.NoError(t, err)
	require.Equal(t, "guard_only", status.Phase)
	require.Equal(t, lookup.reservation.IdentityFingerprint, status.IdentityFingerprint)
	require.Equal(t, int64(19), status.Generation)
	require.Error(t, cache.SaveLiveCall(ctx, record, time.Hour))
	_, err = service.ConfirmLiveHandoffCreation(ctx, lookup, cache, record, 19, time.Hour)
	require.Error(t, err)
}

func TestLiveHandoffV2RejectsLegacyAuditWrites(t *testing.T) {
	ctx := context.Background()
	_, client, cache, record, lookup := newHandoffV2Fixture(t, "audit", 11)
	confirmV2Creation(t, cache, record, lookup, 11, time.Hour)
	state := &service.LiveSettlement{Status: service.LiveSettlementPending, TotalCost: .1, ActualCost: .1, DurationMs: 100, FrozenAt: time.Now().UTC(), SettlementMode: "standard"}
	fp, digest := handoffFrozenDigest(t, record, state)
	_, err := cache.BeginLiveSettlementVerifiedV2(ctx, record, state, fp, digest, 11)
	require.NoError(t, err)
	_, err = cache.ClaimLiveSettlementAudit(ctx, record.CallHash, "legacy", time.Minute)
	require.Error(t, err, "v1 audit claims must not write v2 state")
	require.False(t, client.HExists(ctx, liveCallKey(record.CallHash), "audit_owner").Val())
	require.NoError(t, client.HSet(ctx, liveCallKey(record.CallHash), "audit_owner", "legacy").Err())
	_, err = cache.FinishLiveSettlementAudit(ctx, record.CallHash, "legacy", true)
	require.Error(t, err, "v1 audit completion must not write v2 state")
	require.False(t, client.HExists(ctx, liveCallKey(record.CallHash), "audit_written").Val())
}

func TestLiveHandoffV2IncompleteBlocksController(t *testing.T) {
	ctx := context.Background()
	for _, phase := range []string{"creating", "active"} {
		t.Run(phase, func(t *testing.T) {
			_, client, cache, record, lookup := newHandoffV2Fixture(t, "incomplete-"+phase, 7)
			confirmV2Creation(t, cache, record, lookup, 7, time.Hour)
			key := liveCallKey(record.CallHash)
			require.NoError(t, client.HSet(ctx, key, "handoff_v2_phase", phase, "controller", service.LiveControllerProxy, "controller_owner", "owner").Err())
			require.NoError(t, client.Persist(ctx, key).Err())
			_, err := cache.ClaimLiveController(ctx, record.CallHash, service.LiveControllerProxy, "owner")
			require.Error(t, err)
			_, err = cache.ReleaseLiveController(ctx, record.CallHash, "owner")
			require.Error(t, err)
			state := &service.LiveSettlement{Status: service.LiveSettlementPending, TotalCost: .1, ActualCost: .1, DurationMs: 100, FrozenAt: time.Now().UTC(), SettlementMode: "standard"}
			fp, digest := handoffFrozenDigest(t, record, state)
			_, err = cache.BeginLiveSettlementVerifiedV2(ctx, record, state, fp, digest, 7)
			require.Error(t, err, "incomplete creation or freeze must never invent a winner")
			require.False(t, client.HExists(ctx, key, "settlement").Val())
			status, err := cache.InspectLiveHandoffV2(ctx, record.CallHash)
			require.NoError(t, err)
			if phase == "creating" {
				require.Equal(t, service.LiveHandoffCreationIncomplete, status.Phase)
			} else {
				require.Equal(t, service.LiveHandoffFreezeIncomplete, status.Phase)
			}
		})
	}
}

func TestLiveHandoffV2RejectsPricePresenceMismatch(t *testing.T) {
	ctx := context.Background()
	const generation int64 = 41

	t.Run("missing price while record has price", func(t *testing.T) {
		server, client, cache, record, lookup := newHandoffV2Fixture(t, "price-missing", generation)
		confirmV2Creation(t, cache, record, lookup, generation, time.Hour)
		require.NoError(t, client.HDel(ctx, liveCallKey(record.CallHash), "realtime_price_per_min").Err())
		_, err := service.ConfirmLiveHandoffCreation(ctx, lookup, cache, record, generation, time.Hour)
		require.Error(t, err, "replay must reject a missing price when the record carries one")
		require.Greater(t, server.TTL(liveCallKey(record.CallHash)), time.Duration(0))
	})

	t.Run("empty price present while record has none", func(t *testing.T) {
		server, client, cache, record, lookup := newHandoffV2Fixture(t, "price-empty", generation)
		confirmV2Creation(t, cache, record, lookup, generation, time.Hour)
		require.NoError(t, client.HSet(ctx, liveCallKey(record.CallHash), "realtime_price_per_min", "").Err())
		_, err := service.ConfirmLiveHandoffCreation(ctx, lookup, cache, record, generation, time.Hour)
		require.Error(t, err, "an empty price is still a present field and must be rejected when the record has none")
		require.Greater(t, server.TTL(liveCallKey(record.CallHash)), time.Duration(0))
	})

	t.Run("empty policy present while record has none", func(t *testing.T) {
		server, client, cache, record, lookup := newHandoffV2Fixture(t, "policy-empty", generation)
		record.BillingPolicy = nil
		_, fp, err := service.LiveSettlementIdentityFromRecord(record)
		require.NoError(t, err)
		lookup.reservation.IdentityFingerprint = fp
		confirmV2Creation(t, cache, record, lookup, generation, time.Hour)
		require.NoError(t, client.HSet(ctx, liveCallKey(record.CallHash), "billing_policy", "").Err())
		_, err = service.ConfirmLiveHandoffCreation(ctx, lookup, cache, record, generation, time.Hour)
		require.Error(t, err, "an empty policy is still a present field and must be rejected when the record has none")
		require.Greater(t, server.TTL(liveCallKey(record.CallHash)), time.Duration(0))
	})

	t.Run("begin rejects missing price", func(t *testing.T) {
		server, client, cache, record, lookup := newHandoffV2Fixture(t, "price-begin", generation)
		confirmV2Creation(t, cache, record, lookup, generation, time.Hour)
		require.NoError(t, client.HDel(ctx, liveCallKey(record.CallHash), "realtime_price_per_min").Err())
		state := &service.LiveSettlement{Status: service.LiveSettlementPending, TotalCost: .1, ActualCost: .1, DurationMs: 100, FrozenAt: time.Now().UTC(), SettlementMode: "standard"}
		fp, digest := handoffFrozenDigest(t, record, state)
		_, err := cache.BeginLiveSettlementVerifiedV2(ctx, record, state, fp, digest, generation)
		require.Error(t, err, "begin must validate presence of the price the record carries")
		require.False(t, client.HExists(ctx, liveCallKey(record.CallHash), "settlement").Val())
		require.Greater(t, server.TTL(liveCallKey(record.CallHash)), time.Duration(0))
	})
}

func TestLiveHandoffV2GuardTTLBlocksRemainingEntrypoints(t *testing.T) {
	ctx := context.Background()
	const generation int64 = 43

	t.Run("controller claim and release require persistent guard", func(t *testing.T) {
		_, client, cache, record, lookup := newHandoffV2Fixture(t, "guard-ttl-ctrl", generation)
		confirmV2Creation(t, cache, record, lookup, generation, time.Hour)
		require.NoError(t, client.PExpire(ctx, liveHandoffGuardKey(record.CallHash), time.Minute).Err())
		_, err := cache.ClaimLiveControllerV2(ctx, record.CallHash, service.LiveControllerProxy, "owner", generation)
		require.Error(t, err, "controller claim must require a persistent guard")
		_, err = cache.ReleaseLiveControllerV2(ctx, record.CallHash, "owner", generation)
		require.Error(t, err, "controller release must require a persistent guard")
	})

	t.Run("ack requires persistent guard", func(t *testing.T) {
		_, client, cache, record, lookup := newHandoffV2Fixture(t, "guard-ttl-ack", generation)
		confirmV2Creation(t, cache, record, lookup, generation, time.Hour)
		state := &service.LiveSettlement{Status: service.LiveSettlementPending, TotalCost: .1, ActualCost: .1, DurationMs: 100, FrozenAt: time.Now().UTC(), SettlementMode: "standard"}
		fp, digest := handoffFrozenDigest(t, record, state)
		_, err := cache.BeginLiveSettlementVerifiedV2(ctx, record, state, fp, digest, generation)
		require.NoError(t, err)
		require.NoError(t, client.PExpire(ctx, liveHandoffGuardKey(record.CallHash), time.Minute).Err())
		ref := service.LiveHandoffAckReference{CallHash: record.CallHash, IdentityFingerprint: fp, FrozenDigest: digest, Generation: generation}
		require.Error(t, cache.AcknowledgeLiveHandoffV2(ctx, ref, time.Hour), "ACK must require a persistent guard")
	})

	t.Run("inspect requires persistent guard", func(t *testing.T) {
		_, client, cache, record, lookup := newHandoffV2Fixture(t, "guard-ttl-inspect", generation)
		confirmV2Creation(t, cache, record, lookup, generation, time.Hour)
		require.NoError(t, client.PExpire(ctx, liveHandoffGuardKey(record.CallHash), time.Minute).Err())
		_, err := cache.InspectLiveHandoffV2(ctx, record.CallHash)
		require.Error(t, err, "inspect must require a persistent guard")
	})

	t.Run("guard-only inspect requires persistent guard", func(t *testing.T) {
		server, client, cache, record, lookup := newHandoffV2Fixture(t, "guard-ttl-only", generation)
		confirmV2Creation(t, cache, record, lookup, generation, time.Hour)
		require.NoError(t, client.Del(ctx, liveCallKey(record.CallHash)).Err())
		require.True(t, server.Exists(liveHandoffGuardKey(record.CallHash)))
		require.NoError(t, client.PExpire(ctx, liveHandoffGuardKey(record.CallHash), time.Minute).Err())
		_, err := cache.InspectLiveHandoffV2(ctx, record.CallHash)
		require.Error(t, err, "guard-only inspect must require a persistent guard")
	})
}

func TestLiveHandoffV2ConcurrentCreationConfirmation(t *testing.T) {
	ctx := context.Background()
	const generation int64 = 47
	server, _, cache, record, lookup := newHandoffV2Fixture(t, "concurrent-create", generation)
	type outcome struct {
		result service.ConfirmedLiveHandoffCreation
		err    error
	}
	out := make([]outcome, 2)
	var wg sync.WaitGroup
	gate := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-gate
			out[i].result, out[i].err = service.ConfirmLiveHandoffCreation(ctx, lookup, cache, record, generation, time.Hour)
		}(i)
	}
	close(gate)
	wg.Wait()
	require.NoError(t, out[0].err)
	require.NoError(t, out[1].err)
	first, err := out[0].result.Validate()
	require.NoError(t, err)
	second, err := out[1].result.Validate()
	require.NoError(t, err)
	require.Equal(t, first, second, "concurrent confirmations must converge on the same proof")
	require.True(t, server.Exists(liveCallKey(record.CallHash)))
	require.True(t, server.Exists(liveHandoffGuardKey(record.CallHash)))
	ttl := server.TTL(liveCallKey(record.CallHash))
	require.Greater(t, ttl, time.Duration(0))
	require.LessOrEqual(t, ttl, time.Hour, "concurrent replay must not extend the original TTL")
}
