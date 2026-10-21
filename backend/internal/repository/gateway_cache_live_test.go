package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestGatewayCacheLiveCallIdentityAndController(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	cache, ok := NewGatewayCache(client).(service.LiveCallStore)
	require.True(t, ok)
	otherInstance, ok := NewGatewayCache(client).(service.LiveCallStore)
	require.True(t, ok)
	record := &service.LiveCallRecord{
		CallID:                "call_secret",
		CallHash:              HashLiveCallID("call_secret"),
		AccountID:             11,
		APIKeyID:              22,
		UserID:                33,
		GroupID:               44,
		LeaseID:               "lease",
		Model:                 "gpt-live-test",
		AttestationCiphertext: "encrypted-attestation",
		CreatedAt:             time.Now(),
		ExpiresAt:             time.Now().Add(time.Hour),
		Controller:            service.LiveControllerPending,
	}
	require.NoError(t, cache.SaveLiveCall(context.Background(), record, time.Hour))

	loaded, err := otherInstance.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, record.CallID, loaded.CallID)
	require.Equal(t, record.AccountID, loaded.AccountID)
	require.Equal(t, record.AttestationCiphertext, loaded.AttestationCiphertext)

	claimed, err := cache.ClaimLiveController(context.Background(), record.CallHash, service.LiveControllerObserver, "observer-1")
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = cache.ClaimLiveController(context.Background(), record.CallHash, service.LiveControllerProxy, "proxy-1")
	require.NoError(t, err)
	require.True(t, claimed)
	controller, err := cache.GetLiveController(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, service.LiveControllerProxy, controller)

	released, err := cache.ReleaseLiveController(context.Background(), record.CallHash, "proxy-1")
	require.NoError(t, err)
	require.True(t, released)
	closed, err := cache.MarkLiveCallClosed(context.Background(), record.CallHash, time.Hour)
	require.NoError(t, err)
	require.True(t, closed)
	closed, err = cache.MarkLiveCallClosed(context.Background(), record.CallHash, time.Hour)
	require.NoError(t, err)
	require.False(t, closed)
}

// 冻结单价、计费策略与结算状态必须跨实例可回读：observer 结算与失败重试都依赖它。
func TestGatewayCacheLiveCallPersistsBillingSnapshot(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	writer, ok := NewGatewayCache(client).(service.LiveCallStore)
	require.True(t, ok)
	reader, ok := NewGatewayCache(client).(service.LiveCallStore)
	require.True(t, ok)

	price := 0.125
	frozenAt := time.Now().UTC().Truncate(time.Millisecond)
	record := &service.LiveCallRecord{
		CallID:              "call_billing",
		CallHash:            HashLiveCallID("call_billing"),
		AccountID:           1,
		APIKeyID:            2,
		UserID:              3,
		GroupID:             4,
		CreatedAt:           frozenAt.Add(-2 * time.Minute),
		ExpiresAt:           frozenAt.Add(time.Hour),
		Controller:          service.LiveControllerPending,
		RealtimePricePerMin: &price,
		BillingPolicy: &service.LiveBillingPolicy{
			APIKeyQuota:   5,
			RateLimit5h:   6,
			RateLimit1d:   7,
			RateLimit7d:   8,
			GroupPlatform: "openai",
		},
		Settlement: &service.LiveSettlement{
			Status:      service.LiveSettlementPending,
			TotalCost:   0.25,
			ActualCost:  0.25,
			BillingMode: "per_request",
			DurationMs:  120000,
			Attempts:    1,
			FrozenAt:    frozenAt,
		},
	}
	require.NoError(t, writer.SaveLiveCall(context.Background(), record, time.Hour))

	loaded, err := reader.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.NotNil(t, loaded.RealtimePricePerMin)
	require.InDelta(t, price, *loaded.RealtimePricePerMin, 1e-9)
	require.Equal(t, record.BillingPolicy, loaded.BillingPolicy)
	require.NotNil(t, loaded.Settlement)
	require.Equal(t, service.LiveSettlementPending, loaded.Settlement.Status)
	require.InDelta(t, 0.25, loaded.Settlement.ActualCost, 1e-9)
	require.Equal(t, 1, loaded.Settlement.Attempts)
	require.True(t, frozenAt.Equal(loaded.Settlement.FrozenAt))
}
