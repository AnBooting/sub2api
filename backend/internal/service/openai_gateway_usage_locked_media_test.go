//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// 锁价消费者「媒体/非文本（按次计数）」专用分支的验收测试。
//
// 改前（基线）：locked 分支只要带 ImageCount/VideoCount/AudioUsage/WebSearchCalls/
// SearchCount 就整笔返回 ErrModelPricingUnavailable，账单 0 条、扣费 0。
// 改后：按冻结快照分派计价，且冻结后管理员改实时分组价不影响已冻结请求。
const (
	lockedMediaGroupID   = int64(71)
	lockedMediaAccountID = int64(81)
	lockedMediaKeyID     = int64(91)
	lockedMediaUserID    = int64(92)
	// 冻结 bundle 的基础倍率来源：group.RateMultiplier（无用户专属倍率、非订阅分组无高峰因子）。
	lockedMediaRateMultiplier = 1.5
)

func lockedMediaFloatPtr(v float64) *float64 { return &v }

// lockedMediaTokenCard 构造 token 模式的分组手工卡（client-model）：
// input 1e-6 / output 2e-6 / 缓存读写 0，与 handler 侧锁价夹具同口径。
func lockedMediaTokenCard() ChannelModelPricing {
	return ChannelModelPricing{
		Models:          []string{"client-model"},
		InputPrice:      lockedMediaFloatPtr(1e-6),
		OutputPrice:     lockedMediaFloatPtr(2e-6),
		CacheReadPrice:  lockedMediaFloatPtr(0),
		CacheWritePrice: lockedMediaFloatPtr(0),
	}
}

type lockedMediaFixture struct {
	svc         *OpenAIGatewayService
	usageRepo   *openAIRecordUsageLogRepoStub
	billingRepo *openAIRecordUsageBillingRepoStub
	userRepo    *openAIRecordUsageUserRepoStub
	group       *Group
	key         *APIKey
	account     *Account
	ctx         context.Context
}

// newLockedMediaFixture 用真实冻结核（inflightEstimateDeps.prepareRequestBilling +
// WithGatewayRequestBillingAccount）建立请求级冻结 bundle，再把实际账号绑定上去。
// 冻结后对 group 的实时修改不得影响已冻结请求的计费。
func newLockedMediaFixture(t *testing.T, group *Group, model string) *lockedMediaFixture {
	t.Helper()
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	userRepo := &openAIRecordUsageUserRepoStub{}
	subRepo := &openAIRecordUsageSubRepoStub{}
	cache := NewBillingCacheService(nil, userRepo, subRepo, nil, nil, nil, cfg, nil)
	t.Cleanup(cache.Stop)
	billing := NewBillingService(cfg, nil)
	svc := NewOpenAIGatewayService(
		nil,
		usageRepo,
		billingRepo,
		userRepo,
		subRepo,
		nil,
		nil,
		cfg,
		nil,
		nil,
		billing,
		nil,
		cache,
		nil,
		&DeferredService{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	key := &APIKey{
		ID:      lockedMediaKeyID,
		UserID:  lockedMediaUserID,
		GroupID: i64p(lockedMediaGroupID),
		Group:   group,
		User:    &User{ID: lockedMediaUserID, Status: StatusActive},
	}
	account := &Account{ID: lockedMediaAccountID, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive}
	deps := inflightEstimateDeps{
		cfg:     cfg,
		billing: billing,
		accountMappingSnapshot: func(context.Context, *APIKey, string) (map[int64]requestAccountMapping, error) {
			return map[int64]requestAccountMapping{lockedMediaAccountID: {Model: model}}, nil
		},
	}
	ctx, _, err := deps.prepareRequestBilling(context.Background(), key, InflightEstimateRequest{Model: model})
	require.NoError(t, err)
	ctx, err = WithGatewayRequestBillingAccount(ctx, lockedMediaGroupID, lockedMediaAccountID)
	require.NoError(t, err)
	return &lockedMediaFixture{
		svc: svc, usageRepo: usageRepo, billingRepo: billingRepo, userRepo: userRepo,
		group: group, key: key, account: account, ctx: ctx,
	}
}

func (f *lockedMediaFixture) record(t *testing.T, result *OpenAIForwardResult) error {
	t.Helper()
	return f.svc.RecordUsage(f.ctx, &OpenAIRecordUsageInput{
		Result:  result,
		APIKey:  f.key,
		User:    f.key.User,
		Account: f.account,
	})
}

// recordWithoutBundle 走无冻结 bundle 的 legacy 分支（回归对照）。
func (f *lockedMediaFixture) recordWithoutBundle(t *testing.T, result *OpenAIForwardResult) error {
	t.Helper()
	return f.svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result:  result,
		APIKey:  f.key,
		User:    f.key.User,
		Account: f.account,
	})
}

// TestLockedMediaImageCountUsesFrozenCard WS 图生 turn 形态：ImageCount>0 不再整笔失败，
// 按冻结按次卡（次数 + 尺寸层级）只扣一次；冻结后改实时分组价不影响计费。
func TestLockedMediaImageCountUsesFrozenCard(t *testing.T) {
	price := 0.5
	group := &Group{
		ID: lockedMediaGroupID, Platform: PlatformOpenAI, Status: StatusActive,
		RateMultiplier: lockedMediaRateMultiplier,
		ModelPricing: []ChannelModelPricing{{
			Models: []string{"client-model"}, BillingMode: BillingModeImage, PerRequestPrice: &price,
		}},
	}
	f := newLockedMediaFixture(t, group, "client-model")
	// 冻结后管理员把实时分组价 0.5 → 9.0：锁价必须仍用冻结价，证明没有回源取价。
	group.ModelPricing[0].PerRequestPrice = lockedMediaFloatPtr(9.0)

	result := &OpenAIForwardResult{
		RequestID:     "resp_locked_image_turn",
		Model:         "client-model",
		UpstreamModel: "client-model",
		OpenAIWSMode:  true,
		ImageCount:    2,
		ImageSize:     "4K",
		Usage:         OpenAIUsage{InputTokens: 1000, OutputTokens: 500},
	}
	require.NoError(t, f.record(t, result))

	require.Equal(t, 1, f.billingRepo.calls, "图生 turn 只扣一次")
	require.NotNil(t, f.billingRepo.lastCmd)
	// 0.5（冻结价/张）× 2 张 = 1.0；× 1.5 倍率 = 1.5（改前该请求整笔失败，账单 0）。
	require.InDelta(t, 1.5, f.billingRepo.lastCmd.BalanceCost, 1e-12)
	require.NotNil(t, f.usageRepo.lastLog)
	require.InDelta(t, 1.0, f.usageRepo.lastLog.TotalCost, 1e-12)
	require.InDelta(t, 1.5, f.usageRepo.lastLog.ActualCost, 1e-12)
	require.Equal(t, 2, f.usageRepo.lastLog.ImageCount)
	require.NotNil(t, f.usageRepo.lastLog.BillingMode)
	require.Equal(t, string(BillingModeImage), *f.usageRepo.lastLog.BillingMode)
	require.InDelta(t, lockedMediaRateMultiplier, f.usageRepo.lastLog.RateMultiplier, 1e-12)
}

// TestLockedMediaImageSizeTierUsesFrozenCard 图片按次沿用 legacy 的尺寸层级判定口径，
// 命中的层级价同样取自冻结卡。
func TestLockedMediaImageSizeTierUsesFrozenCard(t *testing.T) {
	group := &Group{
		ID: lockedMediaGroupID, Platform: PlatformOpenAI, Status: StatusActive,
		RateMultiplier: lockedMediaRateMultiplier,
		ModelPricing: []ChannelModelPricing{{
			Models: []string{"client-model"}, BillingMode: BillingModeImage,
			Intervals: []PricingInterval{
				{TierLabel: "1K", PerRequestPrice: lockedMediaFloatPtr(0.3)},
				{TierLabel: "2K", PerRequestPrice: lockedMediaFloatPtr(0.5)},
				{TierLabel: "4K", PerRequestPrice: lockedMediaFloatPtr(0.9)},
			},
		}},
	}
	f := newLockedMediaFixture(t, group, "client-model")
	result := &OpenAIForwardResult{
		RequestID: "resp_locked_image_tier", Model: "client-model", UpstreamModel: "client-model",
		ImageCount: 1, ImageSize: "4K",
	}
	require.NoError(t, f.record(t, result))
	// 4K 冻结层级价 0.9 × 1 张 × 1.5 = 1.35。
	require.InDelta(t, 1.35, f.billingRepo.lastCmd.BalanceCost, 1e-12)
	require.Equal(t, string(BillingModeImage), *f.usageRepo.lastLog.BillingMode)
}

// TestLockedMediaVideoCountUsesFrozenCard 视频按 legacy 的 Grok/视频口径映射为
// 次数 × 时长（BillingModeVideo）与分辨率层级。
func TestLockedMediaVideoCountUsesFrozenCard(t *testing.T) {
	group := &Group{
		ID: lockedMediaGroupID, Platform: PlatformOpenAI, Status: StatusActive,
		RateMultiplier: lockedMediaRateMultiplier,
		ModelPricing: []ChannelModelPricing{{
			Models: []string{"grok-imagine-video"}, BillingMode: BillingModeVideo,
			Intervals: []PricingInterval{{TierLabel: "720p", PerRequestPrice: lockedMediaFloatPtr(0.07)}},
		}},
	}
	f := newLockedMediaFixture(t, group, "grok-imagine-video")
	result := &OpenAIForwardResult{
		RequestID: "resp_locked_video", Model: "grok-imagine-video", UpstreamModel: "grok-imagine-video",
		VideoCount: 2, VideoResolution: "720p", VideoDurationSeconds: 5,
	}
	require.NoError(t, f.record(t, result))
	// 0.07/秒 × 5 秒 × 2 条 = 0.7；× 1.5 = 1.05。
	require.InDelta(t, 1.05, f.billingRepo.lastCmd.BalanceCost, 1e-12)
	require.NotNil(t, f.usageRepo.lastLog)
	require.Equal(t, 2, f.usageRepo.lastLog.VideoCount)
	require.NotNil(t, f.usageRepo.lastLog.VideoResolution)
	require.Equal(t, "720p", *f.usageRepo.lastLog.VideoResolution)
	require.Equal(t, string(BillingModeVideo), *f.usageRepo.lastLog.BillingMode)
	require.InDelta(t, lockedMediaRateMultiplier, f.usageRepo.lastLog.RateMultiplier, 1e-12)
}

// TestLockedMediaAudioUsesFrozenGroupScalar 音频：冻结卡为 per_request 时按
// UsageUnits + 模式层级；否则沿用 legacy 的冻结分组标量价（改实时价不影响）。
func TestLockedMediaAudioUsesFrozenGroupScalar(t *testing.T) {
	t.Run("per_request_card", func(t *testing.T) {
		price := 0.05
		group := &Group{
			ID: lockedMediaGroupID, Platform: PlatformOpenAI, Status: StatusActive,
			RateMultiplier: lockedMediaRateMultiplier,
			ModelPricing: []ChannelModelPricing{{
				Models: []string{"client-model"}, BillingMode: BillingModePerRequest, PerRequestPrice: &price,
			}},
		}
		f := newLockedMediaFixture(t, group, "client-model")
		result := &OpenAIForwardResult{
			RequestID: "resp_locked_audio_card", Model: "client-model", UpstreamModel: "client-model",
			AudioUsage: &AudioUsage{Mode: "realtime", DurationOrUnits: 3},
		}
		require.NoError(t, f.record(t, result))
		// 0.05/分钟 × 3 分钟 = 0.15；× 1.5 = 0.225。
		require.InDelta(t, 0.225, f.billingRepo.lastCmd.BalanceCost, 1e-12)
		require.Equal(t, string(BillingModePerRequest), *f.usageRepo.lastLog.BillingMode)
	})

	t.Run("frozen_group_scalar", func(t *testing.T) {
		group := &Group{
			ID: lockedMediaGroupID, Platform: PlatformOpenAI, Status: StatusActive,
			RateMultiplier:           lockedMediaRateMultiplier,
			ModelPricing:             []ChannelModelPricing{lockedMediaTokenCard()},
			AudioRealtimePricePerMin: lockedMediaFloatPtr(0.20),
		}
		f := newLockedMediaFixture(t, group, "client-model")
		// 冻结后改实时价 0.20 → 9.0：锁价仍用冻结的 0.20。
		group.AudioRealtimePricePerMin = lockedMediaFloatPtr(9.0)

		result := &OpenAIForwardResult{
			RequestID: "resp_locked_audio_scalar", Model: "client-model", UpstreamModel: "client-model",
			AudioUsage: &AudioUsage{Mode: "realtime", DurationOrUnits: 3},
		}
		require.NoError(t, f.record(t, result))
		// 0.20/分钟 × 3 分钟 = 0.6；× 1.5 = 0.9（改前整笔失败；若误读实时价则为 40.5）。
		require.InDelta(t, 0.9, f.billingRepo.lastCmd.BalanceCost, 1e-12)
		require.InDelta(t, 0.6, f.usageRepo.lastLog.TotalCost, 1e-12)
		require.Equal(t, string(BillingModePerRequest), *f.usageRepo.lastLog.BillingMode)
	})
}

// TestLockedMediaWebSearchIsExclusiveAndFrozen WebSearchCalls 为 legacy 的独占按次
// 口径（token 费不加），单价取冻结分组覆盖价。
func TestLockedMediaWebSearchIsExclusiveAndFrozen(t *testing.T) {
	group := &Group{
		ID: lockedMediaGroupID, Platform: PlatformOpenAI, Status: StatusActive,
		RateMultiplier:        lockedMediaRateMultiplier,
		ModelPricing:          []ChannelModelPricing{lockedMediaTokenCard()},
		WebSearchPricePerCall: lockedMediaFloatPtr(0.02),
	}
	f := newLockedMediaFixture(t, group, "client-model")
	// 冻结后改实时价 0.02 → 0.30：锁价仍用冻结价。
	group.WebSearchPricePerCall = lockedMediaFloatPtr(0.30)

	result := &OpenAIForwardResult{
		RequestID: "resp_locked_websearch", Model: "client-model", UpstreamModel: "client-model",
		WebSearchCalls: 3,
		Usage:          OpenAIUsage{InputTokens: 1000, OutputTokens: 500},
	}
	require.NoError(t, f.record(t, result))
	// 0.02/次 × 3 次 = 0.06；× 1.5 = 0.09。token（1000/500）不计入，证明独占口径。
	require.InDelta(t, 0.09, f.billingRepo.lastCmd.BalanceCost, 1e-12)
	require.InDelta(t, 0.06, f.usageRepo.lastLog.TotalCost, 1e-12)
	require.Equal(t, string(BillingModePerRequest), *f.usageRepo.lastLog.BillingMode)
}

// TestLockedMediaSearchCountAddsToTokenCost SearchCount 与 token 费相加（不替换），
// 单价取冻结分组 SearchPricePer1k。
func TestLockedMediaSearchCountAddsToTokenCost(t *testing.T) {
	group := &Group{
		ID: lockedMediaGroupID, Platform: PlatformOpenAI, Status: StatusActive,
		RateMultiplier:   lockedMediaRateMultiplier,
		ModelPricing:     []ChannelModelPricing{lockedMediaTokenCard()},
		SearchPricePer1k: lockedMediaFloatPtr(5.0),
	}
	f := newLockedMediaFixture(t, group, "client-model")
	group.SearchPricePer1k = lockedMediaFloatPtr(50.0)

	result := &OpenAIForwardResult{
		RequestID: "resp_locked_search", Model: "client-model", UpstreamModel: "client-model",
		SearchCount: 2,
		Usage:       OpenAIUsage{InputTokens: 1000, OutputTokens: 500},
	}
	require.NoError(t, f.record(t, result))
	// token：(1000×1e-6 + 500×2e-6)=0.002；search：5/1000 × 2 = 0.01；
	// 合计 0.012；× 1.5 = 0.018（改前整笔失败）。
	require.InDelta(t, 0.018, f.billingRepo.lastCmd.BalanceCost, 1e-12)
	require.InDelta(t, 0.012, f.usageRepo.lastLog.TotalCost, 1e-12)
	// 合计（token + search）而非只记 search：0.018 = 0.003(token) + 0.015(search)。
	require.InDelta(t, 0.018, f.usageRepo.lastLog.ActualCost, 1e-12)
}

// TestLockedMediaTokenModeKeepsImageTokens 冻结卡为 token 模式时保持 token 计费
// （legacy 同口径），图片 token 维度继续可用且与 ImageCount 共存。
func TestLockedMediaTokenModeKeepsImageTokens(t *testing.T) {
	group := &Group{
		ID: lockedMediaGroupID, Platform: PlatformOpenAI, Status: StatusActive,
		RateMultiplier: lockedMediaRateMultiplier,
		ModelPricing:   []ChannelModelPricing{lockedMediaTokenCard()},
	}
	f := newLockedMediaFixture(t, group, "client-model")
	result := &OpenAIForwardResult{
		RequestID: "resp_locked_image_tokens", Model: "client-model", UpstreamModel: "client-model",
		ImageCount: 1, ImageSize: "2K",
		Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 500, ImageInputTokens: 200},
	}
	require.NoError(t, f.record(t, result))
	// 文本输入 800×1e-6 + 图片输入 200×1e-6（未配图片输入价时回退文本价）+ 输出 500×2e-6
	// = 0.002；× 1.5 = 0.003。token 模式不做按次图片计费，与 legacy 一致。
	require.InDelta(t, 0.003, f.billingRepo.lastCmd.BalanceCost, 1e-12)
	require.Equal(t, 200, f.usageRepo.lastLog.ImageInputTokens)
	require.Equal(t, string(BillingModeToken), *f.usageRepo.lastLog.BillingMode)
}

// TestLockedMediaPerRequestCardDoesNotDoubleChargeTokens 按次卡命中时不叠加 token 费，
// 同一用量只计一次。
func TestLockedMediaPerRequestCardDoesNotDoubleChargeTokens(t *testing.T) {
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
		RequestID: "resp_locked_no_double", Model: "client-model", UpstreamModel: "client-model",
		ImageCount: 1, ImageSize: "1K",
		Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 500, ImageInputTokens: 200},
	}
	require.NoError(t, f.record(t, result))
	// 只按冻结按次价计：0.5 × 1 × 1.5 = 0.75（若再加 token 费会变成 0.753）。
	require.InDelta(t, 0.75, f.billingRepo.lastCmd.BalanceCost, 1e-12)
	require.InDelta(t, 0.5, f.usageRepo.lastLog.TotalCost, 1e-12)
	require.Equal(t, string(BillingModeImage), *f.usageRepo.lastLog.BillingMode)
}

// TestLockedMediaMissingFrozenTierFailsClosed 冻结卡没有该尺寸层级的价时 fail-closed：
// 不落零价账（改前是整笔拒绝，改后仍必须拒绝，只是错误来自缺价）。
func TestLockedMediaMissingFrozenTierFailsClosed(t *testing.T) {
	group := &Group{
		ID: lockedMediaGroupID, Platform: PlatformOpenAI, Status: StatusActive,
		RateMultiplier: lockedMediaRateMultiplier,
		ModelPricing: []ChannelModelPricing{{
			Models: []string{"client-model"}, BillingMode: BillingModeImage,
			Intervals: []PricingInterval{{TierLabel: "2K", PerRequestPrice: lockedMediaFloatPtr(0.5)}},
		}},
	}
	f := newLockedMediaFixture(t, group, "client-model")
	result := &OpenAIForwardResult{
		RequestID: "resp_locked_missing_tier", Model: "client-model", UpstreamModel: "client-model",
		ImageCount: 1, ImageSize: "1K",
	}
	err := f.record(t, result)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Equal(t, 0, f.billingRepo.calls, "缺价不得放行")
	require.Equal(t, 0, f.usageRepo.calls, "不得写零价账")
}

// TestLockedMediaUnknownAudioModeFailsClosed 未知音频模式无价可循：fail-closed 而不是零价。
func TestLockedMediaUnknownAudioModeFailsClosed(t *testing.T) {
	group := &Group{
		ID: lockedMediaGroupID, Platform: PlatformOpenAI, Status: StatusActive,
		RateMultiplier: lockedMediaRateMultiplier,
		ModelPricing:   []ChannelModelPricing{lockedMediaTokenCard()},
	}
	f := newLockedMediaFixture(t, group, "client-model")
	result := &OpenAIForwardResult{
		RequestID: "resp_locked_unknown_audio", Model: "client-model", UpstreamModel: "client-model",
		AudioUsage: &AudioUsage{Mode: "not_a_real_mode", DurationOrUnits: 2},
	}
	err := f.record(t, result)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Equal(t, 0, f.billingRepo.calls)
	require.Equal(t, 0, f.usageRepo.calls)
}

// TestLockedMediaLegacyPathWithoutBundleUnchanged 无冻结 bundle 的 legacy 路径行为不变：
// 仍按记账时刻的实时分组价计费（与锁价路径的冻结口径形成对照）。
func TestLockedMediaLegacyPathWithoutBundleUnchanged(t *testing.T) {
	group := &Group{
		ID: lockedMediaGroupID, Platform: PlatformOpenAI, Status: StatusActive,
		RateMultiplier:        lockedMediaRateMultiplier,
		ModelPricing:          []ChannelModelPricing{lockedMediaTokenCard()},
		WebSearchPricePerCall: lockedMediaFloatPtr(0.02),
	}
	f := newLockedMediaFixture(t, group, "client-model")

	first := &OpenAIForwardResult{
		RequestID: "resp_legacy_ws_1", Model: "client-model",
		WebSearchCalls: 2,
	}
	require.NoError(t, f.recordWithoutBundle(t, first))
	// legacy：0.02/次 × 2 次 × 1.5 = 0.06。
	require.InDelta(t, 0.06, f.billingRepo.lastCmd.BalanceCost, 1e-12)

	group.WebSearchPricePerCall = lockedMediaFloatPtr(0.30)
	second := &OpenAIForwardResult{
		RequestID: "resp_legacy_ws_2", Model: "client-model",
		WebSearchCalls: 2,
	}
	require.NoError(t, f.recordWithoutBundle(t, second))
	// legacy 仍读实时分组价：0.30 × 2 × 1.5 = 0.9（锁价路径不允许这种回源）。
	require.InDelta(t, 0.9, f.billingRepo.lastCmd.BalanceCost, 1e-12)
	require.Equal(t, 2, f.billingRepo.calls)
}
