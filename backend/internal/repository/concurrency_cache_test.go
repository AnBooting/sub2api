package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestAccountLoadPhysicalCapacityWaitingIndependent(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewConcurrencyCache(client, 15, 900)
	ctx := context.Background()
	for i := 0; i < 9; i++ {
		ok, err := cache.AcquireAccountSlot(ctx, 1, 10, fmt.Sprintf("active-%d", i))
		require.NoError(t, err)
		require.True(t, ok)
	}
	for i := 0; i < 20; i++ {
		ok, err := cache.IncrementAccountWaitCount(ctx, 1, 30)
		require.NoError(t, err)
		require.True(t, ok)
	}
	for _, capacity := range []int{10, 0, -1} {
		loads, err := cache.GetAccountsLoadBatch(ctx, []service.AccountWithConcurrency{{ID: 1, MaxConcurrency: capacity}})
		require.NoError(t, err)
		require.Equal(t, 9, loads[1].CurrentConcurrency)
		require.Equal(t, 20, loads[1].WaitingCount)
		expected := 0
		if capacity > 0 {
			expected = 90
		}
		require.Equal(t, expected, loads[1].LoadRate)
	}
	acquired, err := cache.AcquireAccountSlot(ctx, 1, 10, "last-slot")
	require.NoError(t, err)
	require.True(t, acquired, "queue pressure must not prevent last physical slot")
	acquired, err = cache.AcquireAccountSlot(ctx, 1, 10, "overflow")
	require.NoError(t, err)
	require.False(t, acquired)
	acquired, err = cache.AcquireAccountSlot(ctx, 1, 10, "last-slot")
	require.NoError(t, err)
	require.True(t, acquired, "same request retry refreshes existing slot even when full")
	loads, err := cache.GetAccountsLoadBatch(ctx, []service.AccountWithConcurrency{{ID: 1, MaxConcurrency: 10}})
	require.NoError(t, err)
	require.Equal(t, 100, loads[1].LoadRate)
}

func TestAccountSlotAtomicPhysicalCapacity(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewConcurrencyCache(client, 15, 900)
	type result struct {
		acquired bool
		err      error
	}
	results := make(chan result, 50)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ok, err := cache.AcquireAccountSlot(context.Background(), 1, 10, fmt.Sprintf("race-%d", i))
			results <- result{ok, err}
		}(i)
	}
	wg.Wait()
	close(results)
	acquired := 0
	for r := range results {
		require.NoError(t, r.err)
		if r.acquired {
			acquired++
		}
	}
	require.Equal(t, 10, acquired, "Lua is the final arbiter for competing snapshots")
	count, err := cache.GetAccountConcurrency(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 10, count)
	// Release every competing ID: ZREM is harmless for rejected requests.
	for pass := 0; pass < 2; pass++ {
		for i := 0; i < 50; i++ {
			require.NoError(t, cache.ReleaseAccountSlot(context.Background(), 1, fmt.Sprintf("race-%d", i)))
		}
		count, err = cache.GetAccountConcurrency(context.Background(), 1)
		require.NoError(t, err)
		require.Zero(t, count, "all slots must return to zero, including repeated releases")
	}
}
