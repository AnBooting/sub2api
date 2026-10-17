package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

type capacityReleaseCache struct {
	ConcurrencyCache
	releases          atomic.Int64
	acquires          atomic.Int64
	releaseContextErr error
}

func (c *capacityReleaseCache) AcquireAccountSlot(context.Context, int64, int, string) (bool, error) {
	c.acquires.Add(1)
	return true, nil
}
func (c *capacityReleaseCache) ReleaseAccountSlot(ctx context.Context, _ int64, _ string) error {
	c.releases.Add(1)
	c.releaseContextErr = ctx.Err()
	return nil
}

func TestAccountSlotReleaseOnceAfterCancellation(t *testing.T) {
	cache := &capacityReleaseCache{}
	svc := NewConcurrencyService(cache)
	ctx, cancel := context.WithCancel(context.Background())
	result, err := svc.AcquireAccountSlot(ctx, 1, 10)
	require.NoError(t, err)
	require.True(t, result.Acquired)
	cancel()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); result.ReleaseFunc() }()
	}
	wg.Wait()
	require.Equal(t, int64(1), cache.releases.Load(), "cancel and normal completion may release only once")
	require.NoError(t, cache.releaseContextErr, "cleanup must use a fresh context, not cancelled request")
}

func TestAccountSlotNonPositiveConcurrencyUnlimited(t *testing.T) {
	for _, capacity := range []int{0, -1} {
		cache := &capacityReleaseCache{}
		result, err := NewConcurrencyService(cache).AcquireAccountSlot(context.Background(), 1, capacity)
		require.NoError(t, err)
		require.True(t, result.Acquired)
		result.ReleaseFunc()
		require.Zero(t, cache.acquires.Load(), "unlimited skips Redis slot limiting")
		require.Zero(t, cache.releases.Load())
	}
}
