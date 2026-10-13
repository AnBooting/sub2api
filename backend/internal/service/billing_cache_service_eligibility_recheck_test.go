//go:build unit

package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestFinancialEligibilityRecheckDoesNotCountRPM(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 100}
	rpm := &userRPMCacheStub{}
	svc := NewBillingCacheService(cache, nil, nil, nil, rpm, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	user := &User{ID: 1, RPMLimit: 10}
	group := &Group{ID: 2, RPMLimit: 10}
	ctx := context.Background()
	require.NoError(t, svc.CheckBillingEligibility(ctx, user, nil, group, nil, ""))
	for i := 0; i < 3; i++ {
		require.NoError(t, svc.CheckFinancialEligibility(ctx, user, nil, group, nil, ""))
	}
	require.EqualValues(t, 1, atomic.LoadInt32(&rpm.userCalls))
	require.EqualValues(t, 1, atomic.LoadInt32(&rpm.userGroupCalls))
	cache.balance = 0
	require.ErrorIs(t, svc.CheckFinancialEligibility(ctx, user, nil, group, nil, ""), ErrInsufficientBalance)
	require.ErrorIs(t, svc.CheckBillingEligibility(ctx, user, nil, group, nil, ""), ErrInsufficientBalance)
	require.EqualValues(t, 1, atomic.LoadInt32(&rpm.userCalls))
	require.EqualValues(t, 1, atomic.LoadInt32(&rpm.userGroupCalls))
}

func TestFinancialEligibilityRecheckPreservesSimpleModeWindows(t *testing.T) {
	now := time.Now()
	rpm := &userRPMCacheStub{}
	svc := &BillingCacheService{
		cfg:                   &config.Config{RunMode: config.RunModeSimple, SimpleModeKeyRateLimitEnabled: true},
		userRPMCache:          rpm,
		apiKeyRateLimitLoader: &simpleModeRateLimitLoaderStub{data: &APIKeyRateLimitData{Usage5h: 10, Window5hStart: &now}},
	}
	user := &User{ID: 1, RPMLimit: 1}
	key := &APIKey{ID: 2, RateLimit5h: 10}
	group := &Group{ID: 3, RPMLimit: 1}
	require.ErrorIs(t, svc.CheckFinancialEligibility(context.Background(), user, key, group, nil, ""), ErrAPIKeyRateLimit5hExceeded)
	require.ErrorIs(t, svc.CheckBillingEligibility(context.Background(), user, key, group, nil, ""), ErrAPIKeyRateLimit5hExceeded)
	key.RateLimit5h = 20
	require.NoError(t, svc.CheckFinancialEligibility(context.Background(), user, key, group, nil, ""))
	require.NoError(t, svc.CheckBillingEligibility(context.Background(), user, key, group, nil, ""))
	require.Zero(t, atomic.LoadInt32(&rpm.userCalls))
	require.Zero(t, atomic.LoadInt32(&rpm.userGroupCalls))
}
