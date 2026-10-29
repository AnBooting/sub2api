package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestReviewLiveAtomicSettlementAcrossCaches(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	a := NewGatewayCache(client).(service.LiveCallStore)
	b := NewGatewayCache(client).(service.LiveCallStore)
	ctx := context.Background()
	record := &service.LiveCallRecord{CallID: "atomic", CallHash: HashLiveCallID("atomic"), Controller: service.LiveControllerPending, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, a.SaveLiveCall(ctx, record, time.Hour))
	states := []*service.LiveSettlement{
		{Status: service.LiveSettlementPending, ActualCost: 0.12345678901234567, TotalCost: 0.12345678901234567, DurationMs: 1, FrozenAt: time.Now().UTC()},
		{Status: service.LiveSettlementPending, ActualCost: 9, TotalCost: 9, DurationMs: 2, FrozenAt: time.Now().UTC().Add(time.Second)},
	}
	type result struct {
		state *service.LiveSettlement
		token string
		first bool
		err   error
	}
	results := make([]result, 2)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, store := range []service.LiveCallStore{a, b} {
		wg.Add(1)
		go func(i int, store service.LiveCallStore) {
			defer wg.Done()
			<-start
			results[i].state, results[i].token, results[i].first, results[i].err = store.BeginLiveSettlement(ctx, record.CallHash, states[i], time.Hour)
		}(i, store)
	}
	close(start)
	wg.Wait()
	require.NoError(t, results[0].err)
	require.NoError(t, results[1].err)
	require.NotEqual(t, results[0].first, results[1].first)
	require.Equal(t, results[0].token, results[1].token)
	require.Equal(t, results[0].state, results[1].state)
	winner := *results[0].state
	require.True(t, winner.ActualCost == states[0].ActualCost || winner.ActualCost == states[1].ActualCost)

	settled := winner
	settled.Status = service.LiveSettlementSettled
	ok, err := a.CompareAndSwapLiveSettlement(ctx, record.CallHash, results[0].token, &settled, time.Hour)
	require.NoError(t, err)
	require.True(t, ok)
	stale := winner
	stale.Attempts++
	ok, err = b.CompareAndSwapLiveSettlement(ctx, record.CallHash, results[1].token, &stale, time.Hour)
	require.NoError(t, err)
	require.False(t, ok, "a stale pending update must not overwrite settled")
	current, token, first, err := b.BeginLiveSettlement(ctx, record.CallHash, &stale, time.Hour)
	require.NoError(t, err)
	require.False(t, first)
	require.Equal(t, service.LiveSettlementSettled, current.Status)
	require.Equal(t, winner.ActualCost, current.ActualCost)
	invalid := *current
	invalid.Status = service.LiveSettlementPending
	_, err = b.CompareAndSwapLiveSettlement(ctx, record.CallHash, token, &invalid, time.Hour)
	require.Error(t, err)
	invalid = *current
	invalid.ActualCost++
	_, err = b.CompareAndSwapLiveSettlement(ctx, record.CallHash, token, &invalid, time.Hour)
	require.Error(t, err)
	logged := *current
	logged.UsageLogWritten = true
	ok, err = b.CompareAndSwapLiveSettlement(ctx, record.CallHash, token, &logged, time.Hour)
	require.NoError(t, err)
	require.True(t, ok)
	current, token, _, err = a.BeginLiveSettlement(ctx, record.CallHash, &winner, time.Hour)
	require.NoError(t, err)
	invalid = *current
	invalid.UsageLogWritten = false
	_, err = a.CompareAndSwapLiveSettlement(ctx, record.CallHash, token, &invalid, time.Hour)
	require.Error(t, err)
}

func TestReviewLiveAtomicSettlementLegacyAndExpiry(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewGatewayCache(client).(service.LiveCallStore)
	ctx := context.Background()
	state := &service.LiveSettlement{Status: service.LiveSettlementPending, ActualCost: 1, TotalCost: 1, FrozenAt: time.Now().UTC()}
	record := &service.LiveCallRecord{CallID: "legacy", CallHash: HashLiveCallID("legacy"), Controller: service.LiveControllerClosed, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), Settlement: state}
	require.NoError(t, store.SaveLiveCall(ctx, record, time.Hour))
	alternate := *state
	alternate.ActualCost = 10
	current, token, first, err := store.BeginLiveSettlement(ctx, record.CallHash, &alternate, time.Second)
	require.NoError(t, err)
	require.False(t, first)
	require.Equal(t, float64(1), current.ActualCost)
	require.Empty(t, current.SettlementMode)
	current.SettlementMode = "standard"
	ok, err := store.CompareAndSwapLiveSettlement(ctx, record.CallHash, token, current, time.Second)
	require.NoError(t, err)
	require.True(t, ok)
	server.FastForward(2 * time.Second)
	_, _, _, err = store.BeginLiveSettlement(ctx, record.CallHash, state, time.Hour)
	require.True(t, errors.Is(err, service.ErrLiveCallNotFound))
	_, err = store.CompareAndSwapLiveSettlement(ctx, record.CallHash, token, current, time.Hour)
	require.True(t, errors.Is(err, service.ErrLiveCallNotFound))
	require.False(t, server.Exists(liveCallKey(record.CallHash)), "expired calls must not be recreated")
}

func TestReviewLiveLeaseDuplicateReleasePreservesOtherLease(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	regular := NewConcurrencyCache(client, 15, 900)
	live := regular.(service.LiveConcurrencyCache)
	ctx := context.Background()
	for _, id := range []string{"lease-a", "lease-b"} {
		ok, err := live.AcquireLiveLease(ctx, 10, 2, 20, 2, 30, id, false)
		require.NoError(t, err)
		require.True(t, ok)
	}
	require.NoError(t, live.ReleaseLiveLease(ctx, 10, 20, 30, "lease-a"))
	require.NoError(t, live.ReleaseLiveLease(ctx, 10, 20, 30, "lease-a"))
	accountCount, err := regular.GetAccountConcurrency(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, accountCount)
	userCount, err := regular.GetUserConcurrency(ctx, 20)
	require.NoError(t, err)
	require.Equal(t, 1, userCount)
	active, err := live.RefreshLiveLease(ctx, 10, 20, 30, "lease-b")
	require.NoError(t, err)
	require.True(t, active)
	removed, err := live.RefreshLiveLease(ctx, 10, 20, 30, "lease-a")
	require.NoError(t, err)
	require.False(t, removed)
}

func TestReviewLiveVerifiedBeginRejectsChangedOrCorruptMetadata(t *testing.T) {
	cases := []struct {
		name    string
		change  func(*service.LiveCallRecord)
		corrupt func(context.Context, *redis.Client, string)
	}{
		{name: "user", change: func(r *service.LiveCallRecord) { r.UserID++ }},
		{name: "account", change: func(r *service.LiveCallRecord) { r.AccountID++ }},
		{name: "api key", change: func(r *service.LiveCallRecord) { r.APIKeyID++ }},
		{name: "group", change: func(r *service.LiveCallRecord) { r.GroupID++ }},
		{name: "subscription", change: func(r *service.LiveCallRecord) { r.SubscriptionID++ }},
		{name: "lease", change: func(r *service.LiveCallRecord) { r.LeaseID += "x" }},
		{name: "model", change: func(r *service.LiveCallRecord) { r.Model += "x" }},
		{name: "created", change: func(r *service.LiveCallRecord) { r.CreatedAt = r.CreatedAt.Add(time.Millisecond) }},
		{name: "expiry", change: func(r *service.LiveCallRecord) { r.ExpiresAt = r.ExpiresAt.Add(time.Millisecond) }},
		{name: "price", change: func(r *service.LiveCallRecord) { value := 0.2; r.RealtimePricePerMin = &value }},
		{name: "policy", change: func(r *service.LiveCallRecord) { r.BillingPolicy = &service.LiveBillingPolicy{APIKeyQuota: 2} }},
		{name: "hash mismatch", change: func(r *service.LiveCallRecord) { r.CallID += "x" }},
		{name: "missing group", corrupt: func(ctx context.Context, c *redis.Client, key string) { c.HDel(ctx, key, "group_id") }},
		{name: "bad timestamp", corrupt: func(ctx context.Context, c *redis.Client, key string) { c.HSet(ctx, key, "created_at", "bad") }},
		{name: "bad price", corrupt: func(ctx context.Context, c *redis.Client, key string) {
			c.HSet(ctx, key, "realtime_price_per_min", "bad")
		}},
		{name: "empty policy is not missing", corrupt: func(ctx context.Context, c *redis.Client, key string) { c.HSet(ctx, key, "billing_policy", "") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			store := NewGatewayCache(client).(service.LiveCallStore)
			ctx := context.Background()
			price := 0.1
			record := &service.LiveCallRecord{CallID: "verified", CallHash: HashLiveCallID("verified"), UserID: 1, AccountID: 2, APIKeyID: 3, LeaseID: "lease", Model: "live", CreatedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), RealtimePricePerMin: &price, Controller: service.LiveControllerPending}
			require.NoError(t, store.SaveLiveCall(ctx, record, time.Hour))
			expected := *record
			if tc.change != nil {
				tc.change(&expected)
			}
			key := liveCallKey(record.CallHash)
			if tc.corrupt != nil {
				tc.corrupt(ctx, client, key)
			}
			ttl := server.TTL(key)
			candidate := &service.LiveSettlement{Status: service.LiveSettlementPending, ActualCost: 0.1, TotalCost: 0.1, FrozenAt: time.Now()}
			_, _, _, err := store.BeginLiveSettlementVerified(ctx, &expected, candidate, 24*time.Hour)
			require.ErrorIs(t, err, service.ErrLiveIdentityMismatch)
			require.Equal(t, ttl, server.TTL(key), "rejected identity must not extend TTL")
			require.Equal(t, service.LiveControllerPending, client.HGet(ctx, key, "controller").Val())
			require.False(t, client.HExists(ctx, key, "settlement").Val())
		})
	}
}

func TestReviewLiveVerifiedBeginLegacyNilAndExpiry(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewGatewayCache(client).(service.LiveCallStore)
	ctx := context.Background()
	record := &service.LiveCallRecord{CallID: "legacy-verified", CallHash: HashLiveCallID("legacy-verified"), UserID: 1, AccountID: 2, APIKeyID: 3, LeaseID: "lease", Model: "live", CreatedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), Controller: service.LiveControllerPending}
	require.NoError(t, store.SaveLiveCall(ctx, record, time.Second))
	candidate := &service.LiveSettlement{Status: service.LiveSettlementSettled, FrozenAt: time.Now()}
	current, _, first, err := store.BeginLiveSettlementVerified(ctx, record, candidate, time.Second)
	require.NoError(t, err)
	require.True(t, first)
	require.Equal(t, float64(0), current.ActualCost)
	server.FastForward(2 * time.Second)
	_, _, _, err = store.BeginLiveSettlementVerified(ctx, record, candidate, time.Hour)
	require.ErrorIs(t, err, service.ErrLiveCallNotFound)
	require.False(t, server.Exists(liveCallKey(record.CallHash)))
}

func TestReviewLiveVerifiedBeginCanonicalFields(t *testing.T) {
	cases := []struct {
		name   string
		field  string
		remove bool
	}{
		{name: "valid"},
		{name: "price missing", field: "realtime_price_per_min", remove: true},
		{name: "price empty", field: "realtime_price_per_min"},
		{name: "policy missing", field: "billing_policy", remove: true},
		{name: "policy empty", field: "billing_policy"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			store := NewGatewayCache(client).(service.LiveCallStore)
			ctx := context.Background()
			price := 0.123456789012345
			created := time.Unix(1800000000, 123456789)
			record := &service.LiveCallRecord{CallID: "canonical", CallHash: HashLiveCallID("canonical"), UserID: 1, AccountID: 2, APIKeyID: 3, LeaseID: "lease", Model: "live", CreatedAt: created, ExpiresAt: created.Add(time.Hour + 765*time.Millisecond), RealtimePricePerMin: &price, BillingPolicy: &service.LiveBillingPolicy{APIKeyQuota: 1.25, RateLimit5h: 0.125, GroupPlatform: "openai"}, Controller: service.LiveControllerPending}
			require.NoError(t, store.SaveLiveCall(ctx, record, time.Hour))
			key := liveCallKey(record.CallHash)
			if tc.field != "" {
				if tc.remove {
					require.NoError(t, client.HDel(ctx, key, tc.field).Err())
				} else {
					require.NoError(t, client.HSet(ctx, key, tc.field, "").Err())
				}
			}
			ttl := server.TTL(key)
			candidate := &service.LiveSettlement{Status: service.LiveSettlementPending, ActualCost: 0.1, TotalCost: 0.1, FrozenAt: time.Now()}
			state, _, first, err := store.BeginLiveSettlementVerified(ctx, record, candidate, 24*time.Hour)
			if tc.field == "" {
				require.NoError(t, err)
				require.True(t, first)
				require.Equal(t, candidate.ActualCost, state.ActualCost)
			} else {
				require.ErrorIs(t, err, service.ErrLiveIdentityMismatch)
				require.Equal(t, ttl, server.TTL(key))
				require.False(t, client.HExists(ctx, key, "settlement").Val())
				require.Equal(t, service.LiveControllerPending, client.HGet(ctx, key, "controller").Val())
			}
		})
	}
}

func TestReviewLiveVerifiedBeginRejectsNonpositiveAndMissingPricesForCharge(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewGatewayCache(client).(service.LiveCallStore)
	ctx := context.Background()
	record := &service.LiveCallRecord{CallID: "no-price", CallHash: HashLiveCallID("no-price"), UserID: 1, AccountID: 2, APIKeyID: 3, LeaseID: "lease", Model: "live", CreatedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), Controller: service.LiveControllerPending}
	require.NoError(t, store.SaveLiveCall(ctx, record, time.Hour))
	candidate := &service.LiveSettlement{Status: service.LiveSettlementPending, ActualCost: 1, TotalCost: 1, FrozenAt: time.Now()}
	_, _, _, err := store.BeginLiveSettlementVerified(ctx, record, candidate, time.Hour)
	require.ErrorIs(t, err, service.ErrLiveIdentityMismatch)
	zero := 0.0
	record.RealtimePricePerMin = &zero
	require.NoError(t, store.SaveLiveCall(ctx, record, time.Hour))
	_, _, _, err = store.BeginLiveSettlementVerified(ctx, record, candidate, time.Hour)
	require.ErrorIs(t, err, service.ErrLiveIdentityMismatch)
	key := liveCallKey(record.CallHash)
	require.Equal(t, service.LiveControllerPending, client.HGet(ctx, key, "controller").Val())
	require.False(t, client.HExists(ctx, key, "settlement").Val())
}

func TestReviewLiveSettlementAuditClaimsUseRedisTimeAndFenceOwners(t *testing.T) {
	server := miniredis.RunT(t)
	now := time.Unix(1800000000, 0)
	server.SetTime(now)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	a := NewGatewayCache(client).(service.LiveCallStore)
	b := NewGatewayCache(client).(service.LiveCallStore)
	ctx := context.Background()
	record := &service.LiveCallRecord{CallID: "audit-claim", CallHash: HashLiveCallID("audit-claim"), Controller: service.LiveControllerPending}
	require.NoError(t, a.SaveLiveCall(ctx, record, time.Hour))
	pending := &service.LiveSettlement{Status: service.LiveSettlementPending, FrozenAt: time.Now()}
	_, token, _, err := a.BeginLiveSettlement(ctx, record.CallHash, pending, time.Hour)
	require.NoError(t, err)
	key := liveCallKey(record.CallHash)
	ttl := server.TTL(key)
	type result struct {
		owner   string
		claimed bool
		err     error
	}
	results := make(chan result, 2)
	for _, item := range []struct {
		owner string
		store service.LiveCallStore
	}{{"owner-a", a}, {"owner-b", b}} {
		go func(owner string, store service.LiveCallStore) {
			won, err := store.ClaimLiveSettlementAudit(ctx, record.CallHash, owner, 10*time.Second)
			results <- result{owner, won, err}
		}(item.owner, item.store)
	}
	one, two := <-results, <-results
	require.NoError(t, one.err)
	require.NoError(t, two.err)
	require.NotEqual(t, one.claimed, two.claimed)
	winner, other := one.owner, two.owner
	if two.claimed {
		winner, other = two.owner, one.owner
	}
	again, err := a.ClaimLiveSettlementAudit(ctx, record.CallHash, winner, 10*time.Second)
	require.NoError(t, err)
	require.True(t, again, "lost reply must be idempotent for the same owner")
	server.SetTime(now.Add(11 * time.Second))
	claimed, err := b.ClaimLiveSettlementAudit(ctx, record.CallHash, other, 10*time.Second)
	require.NoError(t, err)
	require.True(t, claimed, "another owner can take over after Redis time expiry")
	stale, err := a.FinishLiveSettlementAudit(ctx, record.CallHash, winner, true)
	require.NoError(t, err)
	require.False(t, stale, "an expired owner must not complete the new owner's claim")
	released, err := b.FinishLiveSettlementAudit(ctx, record.CallHash, other, false)
	require.NoError(t, err)
	require.True(t, released)
	claimed, err = a.ClaimLiveSettlementAudit(ctx, record.CallHash, "owner-c", 10*time.Second)
	require.NoError(t, err)
	require.True(t, claimed)
	marked, err := a.FinishLiveSettlementAudit(ctx, record.CallHash, "owner-c", true)
	require.NoError(t, err)
	require.True(t, marked)
	claimed, err = b.ClaimLiveSettlementAudit(ctx, record.CallHash, "owner-d", 10*time.Second)
	require.NoError(t, err)
	require.False(t, claimed)
	require.Equal(t, token, client.HGet(ctx, key, "settlement").Val())
	require.Equal(t, ttl, server.TTL(key), "audit claim operations must not extend the call TTL")
	next := *pending
	next.Status = service.LiveSettlementSettled
	swapped, err := a.CompareAndSwapLiveSettlement(ctx, record.CallHash, token, &next, time.Hour)
	require.NoError(t, err)
	require.True(t, swapped)
	claimed, err = a.ClaimLiveSettlementAudit(ctx, record.CallHash, "owner-e", 10*time.Second)
	require.NoError(t, err)
	require.False(t, claimed)
	server.FastForward(2 * time.Hour)
	_, err = a.ClaimLiveSettlementAudit(ctx, record.CallHash, "owner-f", 10*time.Second)
	require.ErrorIs(t, err, service.ErrLiveCallNotFound)
	require.False(t, server.Exists(key))
}

func TestReviewLiveSettlementAuditClaimRejectsMalformedState(t *testing.T) {
	cases := []struct {
		name string
		raw  *string
	}{}
	malformed, missingStatus, badStatus := "{", "{}", `{"status":"unknown"}`
	cases = append(cases, struct {
		name string
		raw  *string
	}{"missing field", nil}, struct {
		name string
		raw  *string
	}{"malformed json", &malformed}, struct {
		name string
		raw  *string
	}{"missing status", &missingStatus}, struct {
		name string
		raw  *string
	}{"invalid status", &badStatus})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			store := NewGatewayCache(client).(service.LiveCallStore)
			ctx := context.Background()
			record := &service.LiveCallRecord{CallID: "audit-invalid", CallHash: HashLiveCallID("audit-invalid"), Controller: service.LiveControllerPending}
			require.NoError(t, store.SaveLiveCall(ctx, record, time.Hour))
			key := liveCallKey(record.CallHash)
			if tc.raw != nil {
				require.NoError(t, client.HSet(ctx, key, "settlement", *tc.raw).Err())
			}
			ttl := server.TTL(key)
			_, err := store.ClaimLiveSettlementAudit(ctx, record.CallHash, "owner", 10*time.Second)
			require.ErrorIs(t, err, service.ErrLiveSettlementInvalid)
			require.False(t, client.HExists(ctx, key, "audit_owner").Val())
			require.False(t, client.HExists(ctx, key, "audit_written").Val())
			require.Equal(t, ttl, server.TTL(key))
			require.Equal(t, service.LiveControllerPending, client.HGet(ctx, key, "controller").Val())
		})
	}
}

func TestReviewLiveSettlementAuditLateOwnerDoesNotModifySettled(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewGatewayCache(client).(service.LiveCallStore)
	ctx := context.Background()
	record := &service.LiveCallRecord{CallID: "late-audit", CallHash: HashLiveCallID("late-audit"), Controller: service.LiveControllerPending}
	require.NoError(t, store.SaveLiveCall(ctx, record, time.Hour))
	pending := &service.LiveSettlement{Status: service.LiveSettlementPending, FrozenAt: time.Now()}
	_, token, _, err := store.BeginLiveSettlement(ctx, record.CallHash, pending, time.Hour)
	require.NoError(t, err)
	claimed, err := store.ClaimLiveSettlementAudit(ctx, record.CallHash, "owner", 10*time.Second)
	require.NoError(t, err)
	require.True(t, claimed)
	settled := *pending
	settled.Status = service.LiveSettlementSettled
	swapped, err := store.CompareAndSwapLiveSettlement(ctx, record.CallHash, token, &settled, time.Hour)
	require.NoError(t, err)
	require.True(t, swapped)
	key := liveCallKey(record.CallHash)
	before, ttl := client.HGet(ctx, key, "settlement").Val(), server.TTL(key)
	finished, err := store.FinishLiveSettlementAudit(ctx, record.CallHash, "owner", true)
	require.NoError(t, err)
	require.True(t, finished)
	require.Equal(t, before, client.HGet(ctx, key, "settlement").Val())
	require.Equal(t, ttl, server.TTL(key))
	claimed, err = store.ClaimLiveSettlementAudit(ctx, record.CallHash, "other", 10*time.Second)
	require.NoError(t, err)
	require.False(t, claimed)
}

func TestReviewLiveSettlementAuditClaimHonoursSubsecondRedisTime(t *testing.T) {
	server := miniredis.RunT(t)
	start := time.Unix(1800000000, 999*int64(time.Millisecond))
	server.SetTime(start)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewGatewayCache(client).(service.LiveCallStore)
	ctx := context.Background()
	record := &service.LiveCallRecord{CallID: "audit-subsecond", CallHash: HashLiveCallID("audit-subsecond"), Controller: service.LiveControllerPending}
	require.NoError(t, store.SaveLiveCall(ctx, record, time.Hour))
	_, _, _, err := store.BeginLiveSettlement(ctx, record.CallHash, &service.LiveSettlement{Status: service.LiveSettlementPending, FrozenAt: time.Now()}, time.Hour)
	require.NoError(t, err)
	won, err := store.ClaimLiveSettlementAudit(ctx, record.CallHash, "owner-a", time.Second)
	require.NoError(t, err)
	require.True(t, won)
	server.SetTime(start.Add(time.Millisecond))
	won, err = store.ClaimLiveSettlementAudit(ctx, record.CallHash, "owner-b", time.Second)
	require.NoError(t, err)
	require.False(t, won, "a one-millisecond boundary crossing cannot expire a one-second claim")
	server.SetTime(start.Add(time.Second))
	won, err = store.ClaimLiveSettlementAudit(ctx, record.CallHash, "owner-b", time.Second)
	require.NoError(t, err)
	require.True(t, won, "the claim can be taken after its full TTL")
}
