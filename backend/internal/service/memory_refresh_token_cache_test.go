//go:build unit

package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestMemoryRefreshTokenCacheConsumedTombstone(t *testing.T) {
	cache := service.NewMemoryRefreshTokenCache()
	ctx := context.Background()

	tokenHash := "hash-a"
	familyID := "family-a"

	family, err := cache.GetConsumedRefreshTokenFamilyID(ctx, tokenHash)
	require.ErrorIs(t, err, service.ErrConsumedRefreshTokenNotFound)
	require.Empty(t, family)

	require.NoError(t, cache.MarkRefreshTokenConsumed(ctx, tokenHash, familyID, time.Hour))
	got, err := cache.GetConsumedRefreshTokenFamilyID(ctx, tokenHash)
	require.NoError(t, err)
	require.Equal(t, familyID, got)
}
