//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestRefreshTokenCacheConsumedTombstoneRoundTrip(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = client.Close() }()
	cache := NewRefreshTokenCache(client)
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
	require.Equal(t, time.Hour, server.TTL("consumed:"+tokenHash))

	server.FastForward(2 * time.Hour)
	got, err = cache.GetConsumedRefreshTokenFamilyID(ctx, tokenHash)
	require.ErrorIs(t, err, service.ErrConsumedRefreshTokenNotFound)
	require.Empty(t, got)
}
