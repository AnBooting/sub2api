//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func videoPlanPricePtr(v float64) *float64 { return &v }

// seedanceTokenPlan builds a per_token plan priced only for 720p.
func seedanceTokenPlan(without, with float64) map[string]domain.VideoPricePlan {
	return map[string]domain.VideoPricePlan{
		VideoPriceFamilySeedance: {
			Mode: domain.VideoBillingModePerToken,
			PerTokenPrices: map[string]domain.VideoTokenPrice{
				VideoBillingResolution720P: {
					WithoutInputVideo: videoPlanPricePtr(without),
					WithInputVideo:    videoPlanPricePtr(with),
				},
			},
		},
	}
}

func videoPlanTestService() *OpenAIGatewayService {
	return &OpenAIGatewayService{billingService: NewBillingService(&config.Config{}, nil)}
}

// PRD-06/K-4: a per_token plan must actually price a video result on the real
// calculateOpenAIVideoCost path, instead of reaching the image-price fallback.
func TestCalculateOpenAIVideoCostUsesPerTokenPlan(t *testing.T) {
	t.Parallel()
	groupID := int64(9001)
	apiKey := &APIKey{ID: 1, GroupID: &groupID, Group: &Group{
		ID: groupID, Platform: PlatformOpenAI,
		VideoPricePlans: seedanceTokenPlan(4.0, 6.0),
	}}
	svc := videoPlanTestService()

	result := &OpenAIForwardResult{
		Model: "doubao-seedance-2-5-260628", BillingModel: "doubao-seedance-2-5-260628",
		VideoResolution: "720p", Usage: OpenAIUsage{OutputTokens: 2_500_000},
	}
	cost, err := svc.calculateOpenAIVideoCost(context.Background(), result.BillingModel, apiKey, result, 0.5)
	require.NoError(t, err)
	require.NotNil(t, cost)
	// 2.5M tokens / 1e6 x 4.0 = 10.0; x0.5 multiplier = 5.0.
	require.InDelta(t, 10.0, cost.TotalCost, 1e-12)
	require.InDelta(t, 5.0, cost.ActualCost, 1e-12)
	require.Equal(t, string(BillingModeVideo), cost.BillingMode)
}

// The create-time video-input fact must select the with_input_video token cell.
func TestCalculateOpenAIVideoCostPerTokenHonoursInputVideoFact(t *testing.T) {
	t.Parallel()
	groupID := int64(9002)
	apiKey := &APIKey{ID: 2, GroupID: &groupID, Group: &Group{
		ID: groupID, Platform: PlatformOpenAI,
		VideoPricePlans: seedanceTokenPlan(4.0, 6.0),
	}}
	svc := videoPlanTestService()
	result := &OpenAIForwardResult{
		Model: "doubao-seedance-2-5-260628", BillingModel: "doubao-seedance-2-5-260628",
		VideoResolution: "720p", Usage: OpenAIUsage{OutputTokens: 1_000_000},
	}

	// Absent fact defaults to false: the higher-priced cell must not be discounted.
	withoutFact, err := svc.calculateOpenAIVideoCost(context.Background(), result.BillingModel, apiKey, result, 1)
	require.NoError(t, err)
	require.InDelta(t, 4.0, withoutFact.TotalCost, 1e-12)

	withFact, err := svc.calculateOpenAIVideoCost(
		WithVideoPlanInputVideoFact(context.Background(), true), result.BillingModel, apiKey, result, 1)
	require.NoError(t, err)
	require.InDelta(t, 6.0, withFact.TotalCost, 1e-12)
}

// A plan that exists but has no price for the requested resolution must fail
// closed: never fall back to the per-second rate or to the image price.
func TestCalculateOpenAIVideoCostPlanMissingTierFailsClosed(t *testing.T) {
	t.Parallel()
	groupID := int64(9003)
	apiKey := &APIKey{ID: 3, GroupID: &groupID, Group: &Group{
		ID: groupID, Platform: PlatformOpenAI,
		VideoPricePlans: seedanceTokenPlan(4.0, 6.0),
		// A per-second slot also exists; it must NOT be used when the plan wins.
		VideoPrice720P: videoPlanPricePtr(99.0),
	}}
	svc := videoPlanTestService()
	result := &OpenAIForwardResult{
		Model: "doubao-seedance-2-5-260628", BillingModel: "doubao-seedance-2-5-260628",
		// 1080p is not priced in the plan.
		VideoResolution: "1080p", Usage: OpenAIUsage{OutputTokens: 1_000_000},
	}
	cost, err := svc.calculateOpenAIVideoCost(context.Background(), result.BillingModel, apiKey, result, 1)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Nil(t, cost)
}

// per_request and per_second plans must also be honoured on this path.
func TestCalculateOpenAIVideoCostPlanPerRequestAndPerSecond(t *testing.T) {
	t.Parallel()
	svc := videoPlanTestService()

	// MiniMax has no canonical price family, so its plan is keyed by the raw model
	// id — the documented fallback key in VideoPricePlanForGroup.
	perRequestGroupID := int64(9004)
	perRequestKey := &APIKey{ID: 4, GroupID: &perRequestGroupID, Group: &Group{
		ID: perRequestGroupID, Platform: PlatformOpenAI,
		VideoPricePlans: map[string]domain.VideoPricePlan{
			"minimax-hailuo-02": {
				Mode:            domain.VideoBillingModePerRequest,
				PerRequestPrice: videoPlanPricePtr(0.25),
			},
		},
	}}
	requestResult := &OpenAIForwardResult{
		Model: "minimax-hailuo-02", BillingModel: "minimax-hailuo-02",
		VideoCount: 4, VideoResolution: "720p",
	}
	cost, err := svc.calculateOpenAIVideoCost(context.Background(), requestResult.BillingModel, perRequestKey, requestResult, 2)
	require.NoError(t, err)
	require.InDelta(t, 1.0, cost.TotalCost, 1e-12)
	require.InDelta(t, 2.0, cost.ActualCost, 1e-12)

	perSecondGroupID := int64(9005)
	perSecondKey := &APIKey{ID: 5, GroupID: &perSecondGroupID, Group: &Group{
		ID: perSecondGroupID, Platform: PlatformOpenAI,
		VideoPricePlans: map[string]domain.VideoPricePlan{
			VideoPriceFamilySeedance: {
				Mode:            domain.VideoBillingModePerSecond,
				PerSecondPrices: map[string]float64{VideoBillingResolution720P: 0.2},
			},
		},
	}}
	secondResult := &OpenAIForwardResult{
		Model: "doubao-seedance-1-0-pro-250528", BillingModel: "doubao-seedance-1-0-pro-250528",
		VideoCount: 2, VideoResolution: "720p", VideoDurationSeconds: 5,
	}
	cost, err = svc.calculateOpenAIVideoCost(context.Background(), secondResult.BillingModel, perSecondKey, secondResult, 1)
	require.NoError(t, err)
	require.InDelta(t, 2.0, cost.TotalCost, 1e-12)
}

// TS-06: a Seedance model with no plan and no operator-set slot must never be
// priced through getDefaultVideoPrice (whose last resort is the image price).
func TestCalculateOpenAIVideoCostSeedanceWithoutPriceFailsClosed(t *testing.T) {
	t.Parallel()
	groupID := int64(9006)
	apiKey := &APIKey{ID: 6, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformOpenAI}}
	svc := videoPlanTestService()
	result := &OpenAIForwardResult{
		Model: "doubao-seedance-2-5-260628", BillingModel: "doubao-seedance-2-5-260628",
		VideoCount: 1, VideoResolution: "720p", Usage: OpenAIUsage{OutputTokens: 1_000_000},
	}
	cost, err := svc.calculateOpenAIVideoCost(context.Background(), result.BillingModel, apiKey, result, 1)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Nil(t, cost)

	// Guard against regression: the bundled image price for this model is a real
	// number, so an unguarded path would silently have produced a cost.
	require.Positive(t, svc.billingService.GetDefaultVideoPriceForModel("doubao-seedance-2-5-260628", "720p"))
}

// Regression: Grok has a bundled per_second tariff and no plan, so its behavior
// must be unchanged (group override, then bundled default).
func TestCalculateOpenAIVideoCostGrokWithoutPlanUnchanged(t *testing.T) {
	t.Parallel()
	svc := videoPlanTestService()

	groupID := int64(9007)
	perSecond := 0.07
	apiKey := &APIKey{ID: 7, GroupID: &groupID, Group: &Group{
		ID: groupID, Platform: PlatformGrok, VideoPrice720P: &perSecond,
	}}
	result := &OpenAIForwardResult{
		Model: "grok-imagine-video", BillingModel: "grok-imagine-video",
		VideoCount: 2, VideoResolution: "720p", VideoDurationSeconds: 5,
	}
	cost, err := svc.calculateOpenAIVideoCost(context.Background(), result.BillingModel, apiKey, result, 1)
	require.NoError(t, err)
	require.InDelta(t, 0.7, cost.TotalCost, 1e-12)

	// No group price: the bundled Grok per-second default still applies.
	bareKey := &APIKey{ID: 8, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformGrok}}
	defaulted, err := svc.calculateOpenAIVideoCost(context.Background(), result.BillingModel, bareKey, result, 1)
	require.NoError(t, err)
	require.InDelta(t, defaultGrokImagineVideoPrice720P*5*2, defaulted.TotalCost, 1e-12)
}

// The Seedance create body is the only source for the video-input dimension.
func TestSeedanceRequestHasVideoInputConservative(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"text only", `{"model":"doubao-seedance","content":[{"type":"text","text":"waves"}]}`, false},
		{"image input is not video", `{"content":[{"type":"image_url","image_url":{"url":"https://x/a.png"}}]}`, false},
		{"video_url is video input", `{"content":[{"type":"video_url","video_url":{"url":"https://x/a.mp4"}}]}`, true},
		{"draft_task is video input", `{"content":[{"type":"draft_task","draft_task":{"id":"t1"}}]}`, true},
		{"empty body", ``, false},
		{"invalid json", `{`, false},
		{"content not array", `{"content":{"type":"video_url"}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, SeedanceRequestHasVideoInput([]byte(tc.body)))
		})
	}
}

// Only a per_token plan requires usage.completion_tokens to settle the charge.
func TestSeedanceCompletionRequiresTokens(t *testing.T) {
	t.Parallel()
	require.True(t, SeedanceCompletionRequiresTokens(nil, "doubao-seedance"), "no plan keeps the legacy token settlement")
	require.True(t, SeedanceCompletionRequiresTokens(&Group{}, "doubao-seedance"))

	tokenGroup := &Group{VideoPricePlans: seedanceTokenPlan(1, 2)}
	require.True(t, SeedanceCompletionRequiresTokens(tokenGroup, "doubao-seedance-2-5-260628"))

	requestGroup := &Group{VideoPricePlans: map[string]domain.VideoPricePlan{
		VideoPriceFamilySeedance: {Mode: domain.VideoBillingModePerRequest, PerRequestPrice: videoPlanPricePtr(1)},
	}}
	require.False(t, SeedanceCompletionRequiresTokens(requestGroup, "doubao-seedance-2-5-260628"))

	secondGroup := &Group{VideoPricePlans: map[string]domain.VideoPricePlan{
		VideoPriceFamilySeedance: {Mode: domain.VideoBillingModePerSecond, PerSecondPrices: map[string]float64{VideoBillingResolution720P: 1}},
	}}
	require.False(t, SeedanceCompletionRequiresTokens(secondGroup, "doubao-seedance-2-5-260628"))
}

// End-to-end through RecordUsage: a Seedance per_token plan must produce a real
// charge and must not borrow the bundled image price.
func TestRecordUsageSeedancePerTokenPlanBillsTokens(t *testing.T) {
	groupID := int64(9008)
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)

	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID:       "seedance-plan-token",
			ResponseID:      "seedance:task-plan-token",
			Model:           "doubao-seedance-2-5-260628",
			BillingModel:    "doubao-seedance-2-5-260628",
			VideoResolution: "720p",
			Usage:           OpenAIUsage{OutputTokens: 2_000_000},
		},
		APIKey: &APIKey{ID: 9008, GroupID: &groupID, Group: &Group{
			ID: groupID, Platform: PlatformOpenAI, RateMultiplier: 1,
			VideoPricePlans: seedanceTokenPlan(3.0, 5.0),
		}},
		User:    &User{ID: 9008},
		Account: &Account{ID: 9008, Platform: PlatformOpenAI},
	})
	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	// 2M / 1e6 x 3.0 = 6.0.
	require.InDelta(t, 6.0, usageRepo.lastLog.TotalCost, 1e-12)
	require.NotNil(t, usageRepo.lastLog.BillingMode)
	require.Equal(t, string(BillingModeVideo), *usageRepo.lastLog.BillingMode)
}
