//go:build unit

package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountPhysicalCapacityBoundary(t *testing.T) {
	low, high := 1, 100
	for _, tc := range []struct {
		name                            string
		capacity, active, waiting, rate int
		factor                          *int
		available                       bool
	}{
		{"last_slot_low_factor", 10, 9, 20, 900, &low, true},
		{"full_high_factor", 10, 10, 0, 10, &high, false},
		{"over_capacity", 10, 11, 0, 0, nil, false},
		{"zero_unlimited", 0, 1000, 20, 1000, &low, true},
		{"negative_unlimited", -1, 1000, 20, 1000, &low, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{Concurrency: tc.capacity, LoadFactor: tc.factor}
			require.Equal(t, tc.available, account.HasAvailableConcurrency(&AccountLoadInfo{CurrentConcurrency: tc.active, WaitingCount: tc.waiting, LoadRate: tc.rate}))
		})
	}
	require.True(t, (&Account{Concurrency: 1}).HasAvailableConcurrency(nil), "unknown snapshot must defer to atomic acquire")
	var missing *Account
	require.False(t, missing.HasAvailableConcurrency(nil))
}

func TestGatewayPhysicalCapacityAdmission(t *testing.T) {
	for _, routed := range []bool{false, true} {
		for _, capacity := range []int{10, 0, -1} {
			t.Run(fmt.Sprintf("routed_%t/capacity_%d", routed, capacity), func(t *testing.T) {
				factor := 1
				account := Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: capacity, LoadFactor: &factor}
				repo := &mockAccountRepoForPlatform{accounts: []Account{account}, accountsByID: map[int64]*Account{1: &account}}
				cache := &mockConcurrencyCache{loadMap: map[int64]*AccountLoadInfo{1: {AccountID: 1, CurrentConcurrency: 9, WaitingCount: 20, LoadRate: 900}}}
				cfg := testConfig()
				cfg.Gateway.Scheduling.LoadBatchEnabled = true
				svc := &GatewayService{accountRepo: repo, cache: &mockGatewayCacheForPlatform{}, cfg: cfg, concurrencyService: NewConcurrencyService(cache)}
				groupID := int64(90)
				if routed {
					svc.groupRepo = &mockGroupRepoForGateway{groups: map[int64]*Group{groupID: {ID: groupID, Platform: PlatformAnthropic, Status: StatusActive, Hydrated: true, ModelRoutingEnabled: true, ModelRouting: map[string][]int64{"claude-test": {1}}}}}
				}
				var group *int64
				if routed {
					group = &groupID
				}
				result, err := svc.SelectAccountWithLoadAwareness(context.Background(), group, "", "claude-test", nil, "", 0)
				require.NoError(t, err)
				require.True(t, result.Acquired, "waiting and legacy load percent cannot hide a physical slot")
				require.Nil(t, result.WaitPlan)
				result.ReleaseFunc()
			})
		}
	}
}

func TestGatewayWaitingIsSeparateRankingSignal(t *testing.T) {
	a := accountWithLoad{account: &Account{ID: 1}, loadInfo: &AccountLoadInfo{LoadRate: 90, WaitingCount: 20}}
	b := accountWithLoad{account: &Account{ID: 2}, loadInfo: &AccountLoadInfo{LoadRate: 90, WaitingCount: 1}}
	require.Len(t, filterByMinLoadRate([]accountWithLoad{a, b}), 2)
	selected := filterByMinWaitingCount(filterByMinLoadRate([]accountWithLoad{a, b}))
	require.Len(t, selected, 1)
	require.Equal(t, int64(2), selected[0].account.ID)
	require.False(t, sameAccountWithLoadGroup(a, b), "random tie shuffle must preserve queue ordering")
}
