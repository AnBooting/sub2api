//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSharedLockedRoutingUsageMetadataPersists 覆盖共享 locked 普通消费点：
// 传 C6 元数据时四个字段全部落 usage_log；nil 保持历史 NULL。
func TestSharedLockedRoutingUsageMetadataPersists(t *testing.T) {
	f := newSharedLockedFixture(t, true, false)
	ctx := f.lockedContext(t, "claude-sonnet-4")
	err := f.svc.RecordUsage(ctx, &RecordUsageInput{
		Result:       sharedLockedResult("claude-sonnet-4"),
		APIKey:       f.key,
		User:         f.key.User,
		Account:      f.account,
		RoutingUsage: &RoutingUsageMetadata{RequestedGroupID: 71, ServedGroupID: 71, Failover: RoutingFailoverNone, AttemptCount: 2},
	})
	require.NoError(t, err)
	require.NotNil(t, f.logs.lastLog)
	require.NotNil(t, f.logs.lastLog.RequestedGroupID)
	require.Equal(t, int64(71), *f.logs.lastLog.RequestedGroupID)
	require.NotNil(t, f.logs.lastLog.ServedGroupID)
	require.Equal(t, int64(71), *f.logs.lastLog.ServedGroupID)
	require.NotNil(t, f.logs.lastLog.Failover)
	require.Equal(t, RoutingFailoverNone, *f.logs.lastLog.Failover)
	require.NotNil(t, f.logs.lastLog.AttemptCount)
	require.Equal(t, 2, *f.logs.lastLog.AttemptCount)
}

func TestSharedLockedRoutingUsageNilKeepsHistoricalNull(t *testing.T) {
	f := newSharedLockedFixture(t, true, false)
	ctx := f.lockedContext(t, "claude-sonnet-4")
	require.NoError(t, f.svc.RecordUsage(ctx, f.input(sharedLockedResult("claude-sonnet-4"))))
	require.NotNil(t, f.logs.lastLog)
	require.Nil(t, f.logs.lastLog.RequestedGroupID)
	require.Nil(t, f.logs.lastLog.ServedGroupID)
	require.Nil(t, f.logs.lastLog.Failover)
	require.Nil(t, f.logs.lastLog.AttemptCount)
}

func TestSharedLockedRoutingUsageRejectsServedConflictBeforeBilling(t *testing.T) {
	f := newSharedLockedFixture(t, true, false)
	ctx := f.lockedContext(t, "claude-sonnet-4")
	err := f.svc.RecordUsage(ctx, &RecordUsageInput{
		Result:       sharedLockedResult("claude-sonnet-4"),
		APIKey:       f.key,
		User:         f.key.User,
		Account:      f.account,
		RoutingUsage: &RoutingUsageMetadata{RequestedGroupID: 71, ServedGroupID: 72, Failover: RoutingFailoverGroup, AttemptCount: 1},
	})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Nil(t, f.ledger.lastCmd)
	require.Nil(t, f.logs.lastLog)
}

func TestOpenAILockedMediaRoutingUsagePersists(t *testing.T) {
	price := 0.5
	group := &Group{
		ID: lockedMediaGroupID, Platform: PlatformOpenAI, Status: StatusActive,
		RateMultiplier: lockedMediaRateMultiplier,
		ModelPricing: []ChannelModelPricing{{
			Models: []string{"client-model"}, BillingMode: BillingModeImage, PerRequestPrice: &price,
		}},
	}
	f := newLockedMediaFixture(t, group, "client-model")
	result := &OpenAIForwardResult{
		RequestID: "resp_locked_image_routing", Model: "client-model", UpstreamModel: "client-model",
		ImageCount: 1, ImageSize: "1K",
	}
	require.NoError(t, f.recordWithRouting(t, result, &RoutingUsageMetadata{
		RequestedGroupID: lockedMediaGroupID, ServedGroupID: lockedMediaGroupID,
		Failover: RoutingFailoverNone, AttemptCount: 1,
	}))
	require.NotNil(t, f.usageRepo.lastLog)
	require.NotNil(t, f.usageRepo.lastLog.RequestedGroupID)
	require.Equal(t, lockedMediaGroupID, *f.usageRepo.lastLog.RequestedGroupID)
	require.NotNil(t, f.usageRepo.lastLog.ServedGroupID)
	require.Equal(t, lockedMediaGroupID, *f.usageRepo.lastLog.ServedGroupID)
	require.NotNil(t, f.usageRepo.lastLog.Failover)
	require.Equal(t, RoutingFailoverNone, *f.usageRepo.lastLog.Failover)
	require.NotNil(t, f.usageRepo.lastLog.AttemptCount)
	require.Equal(t, 1, *f.usageRepo.lastLog.AttemptCount)
}

func TestRoutingUsageEntrySnapshotIsolatedFromCallerMutation(t *testing.T) {
	f := newSharedLockedFixture(t, true, false)
	ctx := f.lockedContext(t, "claude-sonnet-4")
	routing := &RoutingUsageMetadata{RequestedGroupID: 71, ServedGroupID: 71, Failover: RoutingFailoverNone, AttemptCount: 1}
	err := f.svc.RecordUsage(ctx, &RecordUsageInput{
		Result:       sharedLockedResult("claude-sonnet-4"),
		APIKey:       f.key,
		User:         f.key.User,
		Account:      f.account,
		RoutingUsage: routing,
	})
	require.NoError(t, err)
	// 入口已值复制：调用方随后改原指针不得污染已落 UsageLog 的不可变路由事实。
	routing.RequestedGroupID = 99
	routing.ServedGroupID = 99
	routing.Failover = RoutingFailoverGroup
	routing.AttemptCount = 99
	require.NotNil(t, f.logs.lastLog)
	require.Equal(t, int64(71), *f.logs.lastLog.RequestedGroupID)
	require.Equal(t, int64(71), *f.logs.lastLog.ServedGroupID)
	require.Equal(t, RoutingFailoverNone, *f.logs.lastLog.Failover)
	require.Equal(t, 1, *f.logs.lastLog.AttemptCount)
}

func TestOpenAILockedMediaRoutingNilKeepsHistoricalNull(t *testing.T) {
	group := &Group{
		ID: lockedMediaGroupID, Platform: PlatformOpenAI, Status: StatusActive,
		RateMultiplier: lockedMediaRateMultiplier,
		ModelPricing:   []ChannelModelPricing{lockedMediaTokenCard()},
	}
	f := newLockedMediaFixture(t, group, "client-model")
	result := &OpenAIForwardResult{
		RequestID: "resp_locked_image_nil_routing", Model: "client-model", UpstreamModel: "client-model",
		ImageCount: 1, ImageSize: "1K",
	}
	require.NoError(t, f.recordWithRouting(t, result, nil))
	require.NotNil(t, f.usageRepo.lastLog)
	require.Nil(t, f.usageRepo.lastLog.RequestedGroupID)
	require.Nil(t, f.usageRepo.lastLog.ServedGroupID)
	require.Nil(t, f.usageRepo.lastLog.Failover)
	require.Nil(t, f.usageRepo.lastLog.AttemptCount)
}
