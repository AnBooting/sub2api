//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoutingUsageMetadataValidateAndApply(t *testing.T) {
	for _, tc := range []struct {
		name    string
		m       *RoutingUsageMetadata
		wantErr bool
	}{
		{name: "nil_keeps_history", m: nil},
		{name: "none", m: &RoutingUsageMetadata{RequestedGroupID: 10, ServedGroupID: 10, Failover: RoutingFailoverNone, AttemptCount: 1}},
		{name: "account", m: &RoutingUsageMetadata{RequestedGroupID: 10, ServedGroupID: 10, Failover: RoutingFailoverAccount, AttemptCount: 2}},
		{name: "group", m: &RoutingUsageMetadata{RequestedGroupID: 10, ServedGroupID: 20, Failover: RoutingFailoverGroup, AttemptCount: 3}},
		{name: "ha", m: &RoutingUsageMetadata{RequestedGroupID: 10, ServedGroupID: 30, Failover: RoutingFailoverHA, AttemptCount: 4}},
		{name: "zero_requested", m: &RoutingUsageMetadata{ServedGroupID: 10, Failover: RoutingFailoverNone, AttemptCount: 0}, wantErr: true},
		{name: "zero_served", m: &RoutingUsageMetadata{RequestedGroupID: 10, Failover: RoutingFailoverNone, AttemptCount: 0}, wantErr: true},
		{name: "negative_attempts", m: &RoutingUsageMetadata{RequestedGroupID: 10, ServedGroupID: 10, Failover: RoutingFailoverNone, AttemptCount: -1}, wantErr: true},
		{name: "invalid_failover", m: &RoutingUsageMetadata{RequestedGroupID: 10, ServedGroupID: 10, Failover: "sideways", AttemptCount: 0}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.m.validate()
			if tc.wantErr {
				require.Error(t, err)
				require.True(t, errors.Is(err, ErrModelPricingUnavailable))
				return
			}
			require.NoError(t, err)
			log := &UsageLog{}
			tc.m.apply(log)
			if tc.m == nil {
				require.Nil(t, log.RequestedGroupID)
				require.Nil(t, log.ServedGroupID)
				require.Nil(t, log.Failover)
				require.Nil(t, log.AttemptCount)
				return
			}
			require.NotNil(t, log.RequestedGroupID)
			require.Equal(t, tc.m.RequestedGroupID, *log.RequestedGroupID)
			require.NotNil(t, log.ServedGroupID)
			require.Equal(t, tc.m.ServedGroupID, *log.ServedGroupID)
			require.NotNil(t, log.Failover)
			require.Equal(t, tc.m.Failover, *log.Failover)
			require.NotNil(t, log.AttemptCount)
			require.Equal(t, tc.m.AttemptCount, *log.AttemptCount)
		})
	}
}

func TestValidateRoutingUsageFacts(t *testing.T) {
	t.Run("nil_skips", func(t *testing.T) {
		require.NoError(t, validateRoutingUsageFacts(context.Background(), &APIKey{}, nil))
	})

	t.Run("locked_requires_facts_and_requested", func(t *testing.T) {
		input, output, zero := 1e-6, 2e-6, 0.0
		group := &Group{ID: 71, Platform: PlatformAnthropic, Status: StatusActive, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{Models: []string{"client-model"}, InputPrice: &input, OutputPrice: &output, CacheReadPrice: &zero, CacheWritePrice: &zero}}}
		key := &APIKey{ID: 91, UserID: 92, GroupID: requestPricingTestInt64(71), Group: group}
		deps := inflightEstimateDeps{
			billing:  newTestBillingService(),
			resolver: NewModelPricingResolver(nil, newTestBillingService()),
			accountMappingSnapshot: func(context.Context, *APIKey, string) (map[int64]requestAccountMapping, error) {
				return map[int64]requestAccountMapping{81: {Model: "client-model"}}, nil
			},
		}
		ctx, _, err := deps.prepareRequestBilling(context.Background(), key, InflightEstimateRequest{Model: "client-model", Kind: InflightEstimateToken})
		require.NoError(t, err)
		bound, err := WithGatewayRequestBillingAccount(ctx, 71, 81)
		require.NoError(t, err)
		ok := &RoutingUsageMetadata{RequestedGroupID: 71, ServedGroupID: 71, Failover: RoutingFailoverNone, AttemptCount: 1}
		require.NoError(t, validateRoutingUsageFacts(bound, key, ok))
		wrong := &RoutingUsageMetadata{RequestedGroupID: 71, ServedGroupID: 72, Failover: RoutingFailoverNone, AttemptCount: 1}
		require.Error(t, validateRoutingUsageFacts(bound, key, wrong))
		wrongRequested := &RoutingUsageMetadata{RequestedGroupID: 72, ServedGroupID: 71, Failover: RoutingFailoverNone, AttemptCount: 1}
		require.Error(t, validateRoutingUsageFacts(bound, key, wrongRequested))
		// 未绑定事实时，即使值正确也必须拒绝（不能把 metadata 当成事实来源）。
		require.Error(t, validateRoutingUsageFacts(ctx, key, ok))
	})

	t.Run("unlocked_served_matches_api_key_group", func(t *testing.T) {
		gid := int64(71)
		key := &APIKey{GroupID: &gid}
		ok := &RoutingUsageMetadata{RequestedGroupID: 71, ServedGroupID: 71, Failover: RoutingFailoverNone, AttemptCount: 1}
		require.NoError(t, validateRoutingUsageFacts(context.Background(), key, ok))
		wrong := &RoutingUsageMetadata{RequestedGroupID: 71, ServedGroupID: 72, Failover: RoutingFailoverNone, AttemptCount: 1}
		require.Error(t, validateRoutingUsageFacts(context.Background(), key, wrong))
		require.Error(t, validateRoutingUsageFacts(context.Background(), &APIKey{}, ok))
	})
}
