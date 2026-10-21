//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// CalculateCostUnified
// ---------------------------------------------------------------------------

func TestCalculateCostUnified_NilResolver_FallsBackToOldPath(t *testing.T) {
	svc := newTestBillingService()

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500}
	input := CostInput{
		Model:          "claude-sonnet-4",
		Tokens:         tokens,
		RateMultiplier: 1.0,
		Resolver:       nil, // no resolver
	}
	cost, err := svc.CalculateCostUnified(input)
	require.NoError(t, err)

	// Should match the old-path result exactly
	expected, err := svc.calculateCostInternal("claude-sonnet-4", tokens, 1.0, "", nil)
	require.NoError(t, err)
	require.InDelta(t, expected.TotalCost, cost.TotalCost, 1e-10)
	require.InDelta(t, expected.ActualCost, cost.ActualCost, 1e-10)
	// BillingMode is NOT set by old path through CalculateCostUnified (resolver == nil)
	require.Empty(t, cost.BillingMode)
}

func TestCalculateCostUnified_TokenMode(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500}
	input := CostInput{
		Ctx:            context.Background(),
		Model:          "claude-sonnet-4",
		Tokens:         tokens,
		RateMultiplier: 1.5,
		Resolver:       resolver,
	}
	cost, err := bs.CalculateCostUnified(input)
	require.NoError(t, err)
	require.NotNil(t, cost)

	// Verify token billing: Input: 1000*3e-6=0.003, Output: 500*15e-6=0.0075
	expectedTotal := 1000*3e-6 + 500*15e-6
	require.InDelta(t, expectedTotal, cost.TotalCost, 1e-10)
	require.InDelta(t, expectedTotal*1.5, cost.ActualCost, 1e-10)
	require.Equal(t, string(BillingModeToken), cost.BillingMode)
}

func TestCalculateCostUnified_TokenModeAppliesRateMultiplierToImageTokens(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 600, ImageOutputTokens: 100}
	cost, err := bs.CalculateCostUnified(CostInput{
		Ctx:            context.Background(),
		Model:          "claude-sonnet-4",
		Tokens:         tokens,
		RateMultiplier: 3.0,
		Resolver:       resolver,
	})
	require.NoError(t, err)

	textInput := 1000 * 3e-6
	textOutput := 500 * 15e-6
	imageOutput := 100 * 15e-6
	require.InDelta(t, textInput+textOutput+imageOutput, cost.TotalCost, 1e-10)
	require.InDelta(t, (textInput+textOutput+imageOutput)*3.0, cost.ActualCost, 1e-10)
	require.InDelta(t, imageOutput, cost.ImageOutputCost, 1e-10)
}

func TestCalculateCostUnified_PerRequestMode(t *testing.T) {
	// Set up a ChannelService with a per-request pricing channel
	cs := newTestChannelServiceWithCache(t, &channelCache{
		pricingByGroupModel: map[channelModelKey]*ChannelModelPricing{
			{groupID: 1, model: "claude-sonnet-4"}: {
				BillingMode:     BillingModePerRequest,
				PerRequestPrice: testPtrFloat64(0.05),
			},
		},
		channelByGroupID: map[int64]*Channel{
			1: {ID: 1, Status: StatusActive},
		},
		groupPlatform:           map[int64]string{1: ""},
		wildcardByGroupPlatform: map[channelGroupPlatformKey][]*wildcardPricingEntry{},
		mappingByGroupModel:     map[channelModelKey]string{},
		wildcardMappingByGP:     map[channelGroupPlatformKey][]*wildcardMappingEntry{},
		byID:                    map[int64]*Channel{},
	})

	bs := newTestBillingService()
	resolver := NewModelPricingResolver(cs, bs)
	groupID := int64(1)

	input := CostInput{
		Ctx:            context.Background(),
		Model:          "claude-sonnet-4",
		GroupID:        &groupID,
		Tokens:         UsageTokens{InputTokens: 100, OutputTokens: 50},
		RequestCount:   3,
		RateMultiplier: 2.0,
		Resolver:       resolver,
	}
	cost, err := bs.CalculateCostUnified(input)
	require.NoError(t, err)
	require.NotNil(t, cost)

	// 3 requests * $0.05 = $0.15
	require.InDelta(t, 0.15, cost.TotalCost, 1e-10)
	// ActualCost = 0.15 * 2.0 = 0.30
	require.InDelta(t, 0.30, cost.ActualCost, 1e-10)
	require.Equal(t, string(BillingModePerRequest), cost.BillingMode)
}

func TestCalculateCostUnified_ImageMode(t *testing.T) {
	cs := newTestChannelServiceWithCache(t, &channelCache{
		pricingByGroupModel: map[channelModelKey]*ChannelModelPricing{
			{groupID: 2, model: "gemini-image"}: {
				BillingMode:     BillingModeImage,
				PerRequestPrice: testPtrFloat64(0.10),
			},
		},
		channelByGroupID: map[int64]*Channel{
			2: {ID: 2, Status: StatusActive},
		},
		groupPlatform:           map[int64]string{2: ""},
		wildcardByGroupPlatform: map[channelGroupPlatformKey][]*wildcardPricingEntry{},
		mappingByGroupModel:     map[channelModelKey]string{},
		wildcardMappingByGP:     map[channelGroupPlatformKey][]*wildcardMappingEntry{},
		byID:                    map[int64]*Channel{},
	})

	bs := &BillingService{
		cfg:            &config.Config{},
		fallbackPrices: map[string]*ModelPricing{},
	}
	resolver := NewModelPricingResolver(cs, bs)
	groupID := int64(2)

	input := CostInput{
		Ctx:            context.Background(),
		Model:          "gemini-image",
		GroupID:        &groupID,
		Tokens:         UsageTokens{},
		RequestCount:   2,
		RateMultiplier: 1.0,
		Resolver:       resolver,
	}
	cost, err := bs.CalculateCostUnified(input)
	require.NoError(t, err)
	require.NotNil(t, cost)

	// 2 * $0.10 = $0.20
	require.InDelta(t, 0.20, cost.TotalCost, 1e-10)
	require.InDelta(t, 0.20, cost.ActualCost, 1e-10)
	require.Equal(t, string(BillingModeImage), cost.BillingMode)
}

func channelTimeResolvedForTest(base *ModelPricing, intervals []PricingInterval) *ResolvedPricing {
	return &ResolvedPricing{
		Mode:        BillingModeToken,
		BasePricing: base,
		Intervals:   intervals,
		Source:      PricingSourceChannel,
		channelPricing: &ChannelModelPricing{
			BillingMode: BillingModeToken,
			TimePricing: &ChannelTimePricing{
				Timezone: "Asia/Shanghai",
				Periods: []ChannelTimePricingPeriod{{
					StartTime:  "09:00",
					EndTime:    "12:00",
					Multiplier: 2,
				}},
			},
		},
		longContextPricingEnabled: true,
	}
}

func TestCalculateCostUnified_ChannelTimePricingScalesBaseAndActualCost(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	resolved := channelTimeResolvedForTest(&ModelPricing{InputPricePerToken: 0.001}, nil)

	cost, err := billing.CalculateCostUnified(CostInput{
		Ctx:            context.Background(),
		Model:          "model",
		Tokens:         UsageTokens{InputTokens: 1000},
		RateMultiplier: 0.8,
		Resolver:       &ModelPricingResolver{},
		Resolved:       resolved,
		PricingAt:      time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.InDelta(t, 2.0, cost.InputCost, 1e-12)
	require.InDelta(t, 2.0, cost.TotalCost, 1e-12)
	require.InDelta(t, 1.6, cost.ActualCost, 1e-12)
}

func TestCalculateCostUnified_ChannelTimePricingScalesMatchingInterval(t *testing.T) {
	intervalInputPrice := 0.003
	resolved := channelTimeResolvedForTest(
		&ModelPricing{InputPricePerToken: 0.001},
		[]PricingInterval{{MinTokens: 0, InputPrice: &intervalInputPrice}},
	)
	billing := NewBillingService(&config.Config{}, nil)

	cost, err := billing.CalculateCostUnified(CostInput{
		Ctx:       context.Background(),
		Model:     "model",
		Tokens:    UsageTokens{InputTokens: 1000},
		Resolver:  &ModelPricingResolver{},
		Resolved:  resolved,
		PricingAt: time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.InDelta(t, 6.0, cost.InputCost, 1e-12)
	require.InDelta(t, 6.0, cost.TotalCost, 1e-12)
}

func TestCalculateCostUnified_ChannelTimePricingScalesBaseOnUnmatchedInterval(t *testing.T) {
	intervalInputPrice := 0.003
	resolved := channelTimeResolvedForTest(
		&ModelPricing{InputPricePerToken: 0.001},
		[]PricingInterval{{MinTokens: 2000, InputPrice: &intervalInputPrice}},
	)
	billing := NewBillingService(&config.Config{}, nil)

	cost, err := billing.CalculateCostUnified(CostInput{
		Ctx:       context.Background(),
		Model:     "model",
		Tokens:    UsageTokens{InputTokens: 1000},
		Resolver:  &ModelPricingResolver{},
		Resolved:  resolved,
		PricingAt: time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.InDelta(t, 2.0, cost.InputCost, 1e-12)
	require.InDelta(t, 2.0, cost.TotalCost, 1e-12)
}

func TestCalculateCostUnified_ChannelTimePricingDoesNotApplyToGroupPricing(t *testing.T) {
	resolved := channelTimeResolvedForTest(&ModelPricing{InputPricePerToken: 0.001}, nil)
	resolved.Source = PricingSourceGroup
	billing := NewBillingService(&config.Config{}, nil)

	cost, err := billing.CalculateCostUnified(CostInput{
		Ctx:       context.Background(),
		Model:     "model",
		Tokens:    UsageTokens{InputTokens: 1000},
		Resolver:  &ModelPricingResolver{},
		Resolved:  resolved,
		PricingAt: time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.InDelta(t, 1.0, cost.TotalCost, 1e-12)
}

// 「计费时刻落在渠道分时窗口之外 → 不套用分时倍率」。
//
// 这里必须用**显式的窗口外时刻**。零值 PricingAt 的生产语义是
// 「没有指定计费时刻 → 回退当前时刻」（billing_service.go 的计费时点契约：
// 请求级 PricingAt → resolved.pricingAt → timezone.Now()），
// 拿它表达「窗口之外」等于把断言绑到墙上时钟：渠道分时窗口是
// 09:00-12:00（Asia/Shanghai），于是这条用例每天只有 09:00-12:00 之外才绿，
// 窗口内必然红。零值语义见 billing_channel_time_pricing_test.go。
func TestCalculateCostUnified_ChannelTimePricingDoesNotApplyOutsideMatchingTime(t *testing.T) {
	resolved := channelTimeResolvedForTest(&ModelPricing{InputPricePerToken: 0.001}, nil)
	billing := NewBillingService(&config.Config{}, nil)

	// 渠道分时窗口是 09:00-12:00（Asia/Shanghai）。下面两个时刻都明确落在窗口外，
	// 且与「现在几点」无关：
	//   2026-08-17 05:00 UTC = 2026-08-17 13:00（上海）
	//   2026-08-16 16:00 UTC = 2026-08-17 00:00（上海，跨日边界）
	for _, pricingAt := range []time.Time{
		time.Date(2026, 8, 17, 5, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 16, 16, 0, 0, 0, time.UTC),
	} {
		cost, err := billing.CalculateCostUnified(CostInput{
			Ctx:       context.Background(),
			Model:     "model",
			Tokens:    UsageTokens{InputTokens: 1000},
			Resolver:  &ModelPricingResolver{},
			Resolved:  resolved,
			PricingAt: pricingAt,
		})
		require.NoError(t, err)
		require.InDelta(t, 1.0, cost.TotalCost, 1e-12,
			"pricingAt=%v 不在 09:00-12:00 窗口内，不应套用分时倍率", pricingAt)
	}
}

func TestApplyCostBreakdownMultiplierScalesAllMonetaryFields(t *testing.T) {
	cost := &CostBreakdown{
		InputCost:                 1,
		ImageInputCost:            2,
		OutputCost:                3,
		ImageOutputCost:           4,
		CacheCreationCost:         5,
		CacheReadCost:             6,
		TotalCost:                 21,
		ActualCost:                42,
		BillingMode:               string(BillingModeToken),
		LongContextBillingApplied: true,
	}

	applyCostBreakdownMultiplier(cost, 1.5)

	require.InDelta(t, 1.5, cost.InputCost, 1e-12)
	require.InDelta(t, 3.0, cost.ImageInputCost, 1e-12)
	require.InDelta(t, 4.5, cost.OutputCost, 1e-12)
	require.InDelta(t, 6.0, cost.ImageOutputCost, 1e-12)
	require.InDelta(t, 7.5, cost.CacheCreationCost, 1e-12)
	require.InDelta(t, 9.0, cost.CacheReadCost, 1e-12)
	require.InDelta(t, 31.5, cost.TotalCost, 1e-12)
	require.InDelta(t, 63.0, cost.ActualCost, 1e-12)
	require.Equal(t, string(BillingModeToken), cost.BillingMode)
	require.True(t, cost.LongContextBillingApplied)
}

// TestCalculateCostUnified_RateMultiplierZeroProducesZero 锁定新行为：
// 保存时强制 > 0；若 0 仍泄漏到计费层，按 0 计费（而非历史上的 1.0）。
func TestCalculateCostUnified_RateMultiplierZeroProducesZero(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500}

	cost, err := bs.CalculateCostUnified(CostInput{
		Ctx:            context.Background(),
		Model:          "claude-sonnet-4",
		Tokens:         tokens,
		RateMultiplier: 0,
		Resolver:       resolver,
	})
	require.NoError(t, err)
	require.Greater(t, cost.TotalCost, 0.0)
	require.InDelta(t, 0.0, cost.ActualCost, 1e-10)
}

// TestCalculateCostUnified_NegativeRateMultiplierClampedToZero 锁定新行为：
// 负数倍率按 0 计费，避免历史的 <=0 → 1.0 把配置异常静默按标准价扣费。
func TestCalculateCostUnified_NegativeRateMultiplierClampedToZero(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	tokens := UsageTokens{InputTokens: 1000}

	cost, err := bs.CalculateCostUnified(CostInput{
		Ctx:            context.Background(),
		Model:          "claude-sonnet-4",
		Tokens:         tokens,
		RateMultiplier: -5.0,
		Resolver:       resolver,
	})
	require.NoError(t, err)
	require.Greater(t, cost.TotalCost, 0.0)
	require.InDelta(t, 0.0, cost.ActualCost, 1e-10)
}

func TestCalculateCostUnified_BillingModeFieldFilled(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	cost, err := bs.CalculateCostUnified(CostInput{
		Ctx:            context.Background(),
		Model:          "claude-sonnet-4",
		Tokens:         UsageTokens{InputTokens: 100},
		RateMultiplier: 1.0,
		Resolver:       resolver,
	})
	require.NoError(t, err)
	require.Equal(t, "token", cost.BillingMode)
}

func TestCalculateCostUnified_UsesPreResolvedPricing(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	// Pre-resolve with per_request mode to verify it's used instead of re-resolving
	preResolved := &ResolvedPricing{
		Mode:                   BillingModePerRequest,
		DefaultPerRequestPrice: 0.07,
	}

	cost, err := bs.CalculateCostUnified(CostInput{
		Ctx:            context.Background(),
		Model:          "claude-sonnet-4",
		Tokens:         UsageTokens{InputTokens: 100},
		RequestCount:   2,
		RateMultiplier: 1.0,
		Resolver:       resolver,
		Resolved:       preResolved,
	})
	require.NoError(t, err)
	require.NotNil(t, cost)

	// 2 * $0.07 = $0.14
	require.InDelta(t, 0.14, cost.TotalCost, 1e-10)
	require.Equal(t, string(BillingModePerRequest), cost.BillingMode)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// newTestChannelServiceWithCache creates a ChannelService with a pre-populated
// cache snapshot, bypassing the repository layer entirely.
func newTestChannelServiceWithCache(t *testing.T, cache *channelCache) *ChannelService {
	t.Helper()
	cs := &ChannelService{}
	cache.loadedAt = time.Now()
	cs.cache.Store(cache)
	return cs
}
