//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// lockedCacheStorageCatalogJSON 是带显式上下文缓存存储单价的价卡（上游 0.2.17 维度）。
// 存储单价只存在于价格表里：分组价/渠道价不表达这个维度。
const lockedCacheStorageCatalogJSON = `{
	"client-model": {
		"litellm_provider": "anthropic",
		"input_cost_per_token": 0.000001,
		"output_cost_per_token": 0.000002,
		"cache_storage_cost_per_token_per_hour": 0.0000005
	}
}`

// lockedCacheStoragePrice 是 lockedCacheStorageCatalogJSON 里的存储单价（每 token 每小时）。
const lockedCacheStoragePrice = 0.0000005

// lockedCacheStorageTokenHours 是测试用的显式缓存存储量（token·小时）。
const lockedCacheStorageTokenHours = 20000.0

// newLockedCacheStorageFixture 复用共享锁价 fixture；withStoragePrice=true 时把带
// 显式缓存存储单价的价卡装进计费服务（锁价 bundle 会在请求建立时克隆它）。
func newLockedCacheStorageFixture(t *testing.T, withStoragePrice bool) *sharedLockedFixture {
	t.Helper()
	f := newSharedLockedFixture(t, true, false)
	if withStoragePrice {
		f.billing.pricingService = newStubPricingServiceFromJSON(t, lockedCacheStorageCatalogJSON)
		f.svc.resolver = NewModelPricingResolver(nil, f.billing)
	}
	return f
}

// TestSharedLockedUsageChargesFrozenCacheStorage 锁价请求带显式缓存存储量时，
// 按请求建立时冻结的存储单价计费并向客户扣费（此前该维度不计费）。
func TestSharedLockedUsageChargesFrozenCacheStorage(t *testing.T) {
	f := newLockedCacheStorageFixture(t, true)
	ctx := f.lockedContext(t, "claude-sonnet-4")
	result := sharedLockedResult("claude-sonnet-4")
	result.CacheStorageTokenHours = lockedCacheStorageTokenHours

	require.NoError(t, f.svc.RecordUsage(ctx, f.input(result)))
	require.NotNil(t, f.ledger.lastCmd)

	tokenCost := 1000*f.frozenIn + 500*f.frozenOut
	storageCost := lockedCacheStorageTokenHours * lockedCacheStoragePrice
	require.InDelta(t, tokenCost+storageCost, f.ledger.lastCmd.BalanceCost, 1e-12, "客户应付 = token 费 + 冻结存储费")
	require.NotNil(t, f.logs.lastLog)
	require.InDelta(t, tokenCost+storageCost, f.logs.lastLog.TotalCost, 1e-12, "官方总价含冻结存储费")
}

// TestSharedLockedUsageUsesFrozenCacheStoragePrice 锁价请求的存储单价必须来自冻结
// 价卡：冻结之后实时价卡改价不影响已冻结的请求（冻结语义不变）。
func TestSharedLockedUsageUsesFrozenCacheStoragePrice(t *testing.T) {
	f := newLockedCacheStorageFixture(t, true)
	ctx := f.lockedContext(t, "claude-sonnet-4")
	// 冻结之后实时价卡改价。
	f.billing.pricingService.pricingData["client-model"].CacheStorageCostPerTokenPerHour = 0.0000009

	result := sharedLockedResult("claude-sonnet-4")
	result.CacheStorageTokenHours = lockedCacheStorageTokenHours
	require.NoError(t, f.svc.RecordUsage(ctx, f.input(result)))
	require.NotNil(t, f.ledger.lastCmd)

	tokenCost := 1000*f.frozenIn + 500*f.frozenOut
	require.InDelta(t, tokenCost+lockedCacheStorageTokenHours*lockedCacheStoragePrice, f.ledger.lastCmd.BalanceCost, 1e-12,
		"必须按冻结价计费，不受冻结后的实时价表变更影响")
}

// TestSharedLockedUsageFailsClosedWhenCacheStoragePriceMissing 锁价请求带显式缓存
// 存储量但冻结价卡里没有存储单价时必须 fail-closed，不得静默按 0 计费。
func TestSharedLockedUsageFailsClosedWhenCacheStoragePriceMissing(t *testing.T) {
	f := newLockedCacheStorageFixture(t, false)
	ctx := f.lockedContext(t, "claude-sonnet-4")
	result := sharedLockedResult("claude-sonnet-4")
	result.CacheStorageTokenHours = lockedCacheStorageTokenHours

	err := f.svc.RecordUsage(ctx, f.input(result))
	require.Error(t, err)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

// TestSharedLockedUsageCacheStorageMatchesLiveCost 锁价路径与 live 路径的显式缓存
// 存储费口径一致（同样的用量与价卡，两条路径算出同样的钱）。
func TestSharedLockedUsageCacheStorageMatchesLiveCost(t *testing.T) {
	live := newLockedCacheStorageFixture(t, true)
	liveResult := sharedLockedResult("claude-sonnet-4")
	liveResult.CacheStorageTokenHours = lockedCacheStorageTokenHours
	require.NoError(t, live.svc.RecordUsage(context.Background(), live.input(liveResult)))
	require.NotNil(t, live.ledger.lastCmd)
	require.NotNil(t, live.logs.lastLog)

	locked := newLockedCacheStorageFixture(t, true)
	lockedCtx := locked.lockedContext(t, "claude-sonnet-4")
	lockedResult := sharedLockedResult("claude-sonnet-4")
	lockedResult.CacheStorageTokenHours = lockedCacheStorageTokenHours
	require.NoError(t, locked.svc.RecordUsage(lockedCtx, locked.input(lockedResult)))
	require.NotNil(t, locked.ledger.lastCmd)
	require.NotNil(t, locked.logs.lastLog)

	require.InDelta(t, live.ledger.lastCmd.BalanceCost, locked.ledger.lastCmd.BalanceCost, 1e-12, "客户应付口径一致")
	require.InDelta(t, live.logs.lastLog.TotalCost, locked.logs.lastLog.TotalCost, 1e-12, "官方总价口径一致")
}

// TestSharedLockedUsageCacheStorageAccountStatsMatchesLive 账号统计成本口径一致：
// 没有账号统计定价规则时，账号统计成本回落到总价，两条路径应完全相同。
func TestSharedLockedUsageCacheStorageAccountStatsMatchesLive(t *testing.T) {
	live := newLockedCacheStorageFixture(t, true)
	liveResult := sharedLockedResult("claude-sonnet-4")
	liveResult.CacheStorageTokenHours = lockedCacheStorageTokenHours
	require.NoError(t, live.svc.RecordUsage(context.Background(), live.input(liveResult)))
	require.NotNil(t, live.logs.lastLog)

	locked := newLockedCacheStorageFixture(t, true)
	lockedCtx := locked.lockedContext(t, "claude-sonnet-4")
	lockedResult := sharedLockedResult("claude-sonnet-4")
	lockedResult.CacheStorageTokenHours = lockedCacheStorageTokenHours
	require.NoError(t, locked.svc.RecordUsage(lockedCtx, locked.input(lockedResult)))
	require.NotNil(t, locked.logs.lastLog)

	liveStats := live.logs.lastLog.TotalCost
	if live.logs.lastLog.AccountStatsCost != nil {
		liveStats = *live.logs.lastLog.AccountStatsCost
	}
	lockedStats := locked.logs.lastLog.TotalCost
	if locked.logs.lastLog.AccountStatsCost != nil {
		lockedStats = *locked.logs.lastLog.AccountStatsCost
	}
	require.InDelta(t, liveStats, lockedStats, 1e-12, "账号统计成本口径一致")
}

// TestSharedLockedUsageStorageOnlyChargesStorage 纯存储量请求（无 token 用量）
// 在锁价路径下只收存储费，不额外计 token/按次费用（与 live 路径同口径）。
func TestSharedLockedUsageStorageOnlyChargesStorage(t *testing.T) {
	f := newLockedCacheStorageFixture(t, true)
	ctx := f.lockedContext(t, "claude-sonnet-4")
	result := &ForwardResult{
		RequestID:              "upstream-storage-only-1",
		Model:                  "client-model",
		UpstreamModel:          "claude-sonnet-4",
		CacheStorageTokenHours: lockedCacheStorageTokenHours,
	}

	require.NoError(t, f.svc.RecordUsage(ctx, f.input(result)))
	require.NotNil(t, f.ledger.lastCmd)
	require.InDelta(t, lockedCacheStorageTokenHours*lockedCacheStoragePrice, f.ledger.lastCmd.BalanceCost, 1e-12,
		"纯存储量请求只收冻结存储费")
}

// TestSharedLockedUsageCacheStorageAccountStatsRuleMatchesLive 渠道配了账号统计定价
// 规则时，锁价路径的账号统计成本必须与 live 路径一致：存储费同样计入账号统计成本
// （live：规则 token 费 + 官方存储费；锁价：同一笔冻结存储费）。
func TestSharedLockedUsageCacheStorageAccountStatsRuleMatchesLive(t *testing.T) {
	channel := &Channel{ID: 1, Status: StatusActive, AccountStatsPricingRules: []AccountStatsPricingRule{{
		GroupIDs: []int64{71},
		Pricing:  []ChannelModelPricing{{Models: []string{"claude-sonnet-4"}, InputPrice: testPtrFloat64(0.0000002)}},
	}}}

	live := newLockedCacheStorageFixture(t, true)
	live.svc.channelService = newTestChannelServiceForStats(t, channel, 71, PlatformAnthropic)
	liveResult := sharedLockedResult("claude-sonnet-4")
	liveResult.CacheStorageTokenHours = lockedCacheStorageTokenHours
	require.NoError(t, live.svc.RecordUsage(context.Background(), live.input(liveResult)))
	require.NotNil(t, live.logs.lastLog)
	require.NotNil(t, live.logs.lastLog.AccountStatsCost)

	locked := newLockedCacheStorageFixture(t, true)
	locked.svc.channelService = newTestChannelServiceForStats(t, channel, 71, PlatformAnthropic)
	lockedCtx := locked.lockedContext(t, "claude-sonnet-4")
	lockedResult := sharedLockedResult("claude-sonnet-4")
	lockedResult.CacheStorageTokenHours = lockedCacheStorageTokenHours
	require.NoError(t, locked.svc.RecordUsage(lockedCtx, locked.input(lockedResult)))
	require.NotNil(t, locked.logs.lastLog)
	require.NotNil(t, locked.logs.lastLog.AccountStatsCost)

	require.InDelta(t, *live.logs.lastLog.AccountStatsCost, *locked.logs.lastLog.AccountStatsCost, 1e-12,
		"账号统计成本口径一致（含显式缓存存储费）")
	// live 侧的账号统计成本 = 规则 token 费 + 官方存储费，两侧都必须含这笔存储费。
	require.InDelta(t, 1000*0.0000002+lockedCacheStorageTokenHours*lockedCacheStoragePrice, *locked.logs.lastLog.AccountStatsCost, 1e-12)
}

// TestSharedLockedUsageStorageOnlyMatchesLiveCost 纯存储量请求在锁价路径与 live 路径
// 同口径：不是一次模型请求，只收显式缓存存储费（usage_log 快照里也不含本次模型
// 请求的分项）。
func TestSharedLockedUsageStorageOnlyMatchesLiveCost(t *testing.T) {
	storageOnly := func() *ForwardResult {
		return &ForwardResult{
			RequestID:              "upstream-storage-only-1",
			Model:                  "client-model",
			UpstreamModel:          "claude-sonnet-4",
			CacheStorageTokenHours: lockedCacheStorageTokenHours,
		}
	}

	live := newLockedCacheStorageFixture(t, true)
	require.NoError(t, live.svc.RecordUsage(context.Background(), live.input(storageOnly())))
	require.NotNil(t, live.logs.lastLog)
	require.NotNil(t, live.ledger.lastCmd)

	locked := newLockedCacheStorageFixture(t, true)
	lockedCtx := locked.lockedContext(t, "claude-sonnet-4")
	require.NoError(t, locked.svc.RecordUsage(lockedCtx, locked.input(storageOnly())))
	require.NotNil(t, locked.logs.lastLog)
	require.NotNil(t, locked.ledger.lastCmd)

	require.InDelta(t, live.ledger.lastCmd.BalanceCost, locked.ledger.lastCmd.BalanceCost, 1e-12, "客户应付口径一致")
	require.InDelta(t, live.logs.lastLog.TotalCost, locked.logs.lastLog.TotalCost, 1e-12, "官方总价口径一致")
	// 纯存储量请求不是一次模型请求：快照里不应出现本次模型请求的分项（与 live 一致）。
	require.NotContains(t, string(live.logs.lastLog.PricingSnapshot), "\"items\"")
	require.NotContains(t, string(locked.logs.lastLog.PricingSnapshot), "\"items\"")
}

// TestSharedLockedUsageStorageOnlySkipsPerRequestCard 纯存储量请求不是一次模型请求：
// 按次计费模式下也不收按次卡，只收显式缓存存储费（live 与锁价同口径）。
func TestSharedLockedUsageStorageOnlySkipsPerRequestCard(t *testing.T) {
	perRequest := 0.05
	newFixture := func(t *testing.T) *sharedLockedFixture {
		t.Helper()
		f := newSharedLockedFixture(t, false, false)
		f.group.ModelPricing = []ChannelModelPricing{{
			Models:          []string{"client-model"},
			BillingMode:     BillingModePerRequest,
			PerRequestPrice: &perRequest,
		}}
		f.billing.pricingService = newStubPricingServiceFromJSON(t, lockedCacheStorageCatalogJSON)
		f.svc.resolver = NewModelPricingResolver(nil, f.billing)
		return f
	}
	storageOnly := func() *ForwardResult {
		return &ForwardResult{
			RequestID:              "upstream-storage-only-1",
			Model:                  "client-model",
			UpstreamModel:          "claude-sonnet-4",
			CacheStorageTokenHours: lockedCacheStorageTokenHours,
		}
	}

	live := newFixture(t)
	require.NoError(t, live.svc.RecordUsage(context.Background(), live.input(storageOnly())))
	require.NotNil(t, live.ledger.lastCmd)

	locked := newFixture(t)
	lockedCtx := locked.lockedContext(t, "claude-sonnet-4")
	require.NoError(t, locked.svc.RecordUsage(lockedCtx, locked.input(storageOnly())))
	require.NotNil(t, locked.ledger.lastCmd)

	require.InDelta(t, lockedCacheStorageTokenHours*lockedCacheStoragePrice, locked.ledger.lastCmd.BalanceCost, 1e-12,
		"纯存储量请求不收按次卡，只收冻结存储费")
	require.InDelta(t, live.ledger.lastCmd.BalanceCost, locked.ledger.lastCmd.BalanceCost, 1e-12, "与 live 口径一致")
}
