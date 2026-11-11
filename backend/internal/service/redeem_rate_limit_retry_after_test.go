//go:build unit

package service

import (
	"context"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

// R-01：service 层 429 必须携带 Retry-After 元数据，response.ErrorFrom 才能落头。
func TestCheckRedeemRateLimitCarriesRetryAfter(t *testing.T) {
	svc := &RedeemService{cache: &redeemRateLimitCacheStub{count: redeemMaxFailedAttempts}}
	err := svc.checkRedeemRateLimit(context.Background(), 42)
	require.ErrorIs(t, err, ErrRedeemRateLimited)
	seconds, ok := infraerrors.RetryAfter(err)
	require.True(t, ok)
	require.Greater(t, seconds, 0)

	svc2 := &RedeemService{cache: &redeemRateLimitCacheStub{count: redeemMaxFailedAttempts - 1}}
	require.NoError(t, svc2.checkRedeemRateLimit(context.Background(), 42))
}
