package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type physicalCapacityLoadCache struct {
	schedulerTestConcurrencyCache
	requests []AccountWithConcurrency
}

func (c *physicalCapacityLoadCache) GetAccountsLoadBatch(ctx context.Context, accounts []AccountWithConcurrency) (map[int64]*AccountLoadInfo, error) {
	c.requests = append([]AccountWithConcurrency(nil), accounts...)
	return c.schedulerTestConcurrencyCache.GetAccountsLoadBatch(ctx, accounts)
}

func TestOpenAISchedulerPhysicalCapacity(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		for _, tc := range []struct {
			name                           string
			capacity, active, factor, rate int
			acquired                       bool
		}{
			{"last_slot_waiting_low_factor", 10, 9, 1, 900, true},
			{"full_high_factor", 10, 10, 100, 10, false},
			{"zero_unlimited", 0, 1000, 1, 1000, true},
			{"negative_unlimited", -1, 1000, 1, 1000, true},
		} {
			t.Run(fmt.Sprintf("advanced_%t/%s", advanced, tc.name), func(t *testing.T) {
				resetOpenAIAdvancedSchedulerSettingCacheForTest()
				defer resetOpenAIAdvancedSchedulerSettingCacheForTest()
				account := Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: tc.capacity, LoadFactor: &tc.factor}
				acquireIDs := []int64{}
				cache := &physicalCapacityLoadCache{schedulerTestConcurrencyCache: schedulerTestConcurrencyCache{loadMap: map[int64]*AccountLoadInfo{1: {AccountID: 1, CurrentConcurrency: tc.active, WaitingCount: 20, LoadRate: tc.rate}}, acquiredIDs: &acquireIDs}}
				cfg := &config.Config{}
				cfg.Gateway.Scheduling.LoadBatchEnabled = true
				enabled := "false"
				if advanced {
					enabled = "true"
				}
				svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{account}}, cfg: cfg, concurrencyService: NewConcurrencyService(cache), rateLimitService: newOpenAIAdvancedSchedulerRateLimitService(enabled)}
				result, _, err := svc.SelectAccountWithScheduler(context.Background(), nil, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
				require.NoError(t, err)
				require.Equal(t, tc.acquired, result.Acquired)
				require.NotEmpty(t, cache.requests)
				require.Equal(t, tc.capacity, cache.requests[0].MaxConcurrency, "load factor is not physical capacity")
				if result.Acquired {
					result.ReleaseFunc()
				} else {
					require.NotNil(t, result.WaitPlan)
					require.Empty(t, acquireIDs, "snapshot known full should not be probed")
				}
			})
		}
	}
}

type capacityDiagnosticAccountRepo struct {
	AccountRepository
	accounts []Account
}

func (r capacityDiagnosticAccountRepo) ListSchedulable(context.Context) ([]Account, error) {
	return r.accounts, nil
}

func TestCapacityDiagnosticsPhysicalDenominator(t *testing.T) {
	factor := 1
	accounts := []Account{{ID: 1, Concurrency: 10, LoadFactor: &factor}, {ID: 2, Concurrency: 0, LoadFactor: &factor}}
	cache := &physicalCapacityLoadCache{}
	ops := &OpsService{concurrencyService: NewConcurrencyService(cache)}
	ops.getAccountsLoadMapBestEffort(context.Background(), accounts)
	byID := map[int64]int{}
	for _, request := range cache.requests {
		byID[request.ID] = request.MaxConcurrency
	}
	require.Equal(t, map[int64]int{1: 10, 2: 0}, byID)
	collector := &OpsMetricsCollector{accountRepo: capacityDiagnosticAccountRepo{accounts: accounts}}
	requests, err := collector.listSchedulableAccountLoads(context.Background())
	require.NoError(t, err)
	require.Equal(t, []AccountWithConcurrency{{ID: 1, MaxConcurrency: 10}, {ID: 2, MaxConcurrency: 0}}, requests)
}

func TestBuildOpenAIAccountLoadRequestPhysicalDenominator(t *testing.T) {
	factor := 1
	require.Equal(t, []AccountWithConcurrency{{ID: 1, MaxConcurrency: 10}, {ID: 2, MaxConcurrency: 0}, {ID: 3, MaxConcurrency: -1}}, buildOpenAIAccountLoadRequest([]*Account{nil, {ID: 1, Concurrency: 10, LoadFactor: &factor}, {ID: 2}, {ID: 3, Concurrency: -1}}))
}
