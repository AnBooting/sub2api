//go:build unit

package service

import (
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func videoPlanPtr(v float64) *float64 { return &v }

// PRD-06/K-4: per_request plans bill unit price × count, independent of duration.
func TestVideoCostPlanPerRequest(t *testing.T) {
	t.Parallel()
	plan := &domain.VideoPricePlan{
		Mode:            domain.VideoBillingModePerRequest,
		PerRequestPrice: videoPlanPtr(0.5),
	}
	svc := &BillingService{}
	cost, err := svc.CalculateVideoCostFromPlan(VideoCostInput{
		Model: "minimax-hailuo-02", VideoCount: 3, Plan: plan, RateMultiplier: 2,
	})
	require.NoError(t, err)
	require.NotNil(t, cost)
	require.InDelta(t, 1.5, cost.TotalCost, 1e-12)
	require.InDelta(t, 3.0, cost.ActualCost, 1e-12)
	require.Equal(t, string(BillingModeVideo), cost.BillingMode)
}

// per_second plans bill unit price × duration × count (Grok Imagine口径).
func TestVideoCostPlanPerSecond(t *testing.T) {
	t.Parallel()
	plan := &domain.VideoPricePlan{
		Mode: domain.VideoBillingModePerSecond,
		PerSecondPrices: map[string]float64{
			VideoBillingResolution480P: 0.05,
			VideoBillingResolution720P: 0.14,
		},
	}
	svc := &BillingService{}
	cost, err := svc.CalculateVideoCostFromPlan(VideoCostInput{
		Model: "grok-imagine-video-1.5", Resolution: "720p",
		VideoCount: 1, DurationSeconds: 10, Plan: plan, RateMultiplier: 1,
	})
	require.NoError(t, err)
	require.InDelta(t, 1.4, cost.TotalCost, 1e-12)

	// A tier the operator did not price must not borrow another tier's rate.
	_, err = svc.CalculateVideoCostFromPlan(VideoCostInput{
		Model: "grok-imagine-video-1.5", Resolution: "1080p",
		VideoCount: 1, DurationSeconds: 10, Plan: plan, RateMultiplier: 1,
	})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

// per_token plans bill completion_tokens / 1e6 × unit price, tiered by
// resolution × whether the request carried a video input (Ark official contract).
func TestVideoCostPlanPerTokenTieredByResolutionAndInputVideo(t *testing.T) {
	t.Parallel()
	plan := &domain.VideoPricePlan{
		Mode: domain.VideoBillingModePerToken,
		PerTokenPrices: map[string]domain.VideoTokenPrice{
			VideoBillingResolution720P: {
				WithoutInputVideo: videoPlanPtr(4.0),
				WithInputVideo:    videoPlanPtr(6.0),
			},
			VideoBillingResolution1080P: {
				WithoutInputVideo: videoPlanPtr(10.0),
			},
		},
	}
	svc := &BillingService{}

	// 2.5M completion tokens at ¥4/M with a 0.5 multiplier.
	cost, err := svc.CalculateVideoCostFromPlan(VideoCostInput{
		Model: "doubao-seedance-2-5-260628", Resolution: "720p",
		CompletionTokens: 2_500_000, Plan: plan, RateMultiplier: 0.5,
	})
	require.NoError(t, err)
	require.InDelta(t, 10.0, cost.TotalCost, 1e-12)
	require.InDelta(t, 5.0, cost.ActualCost, 1e-12)

	// The same tokens with a video input use the with_input_video cell.
	withInput, err := svc.CalculateVideoCostFromPlan(VideoCostInput{
		Model: "doubao-seedance-2-5-260628", Resolution: "720p",
		CompletionTokens: 2_500_000, HasInputVideo: true, Plan: plan, RateMultiplier: 1,
	})
	require.NoError(t, err)
	require.InDelta(t, 15.0, withInput.TotalCost, 1e-12)

	// 1080p priced only for the no-input cell: the input-video cell is missing and
	// must fail closed instead of reusing the other cell.
	_, err = svc.CalculateVideoCostFromPlan(VideoCostInput{
		Model: "doubao-seedance-2-5-260628", Resolution: "1080p",
		CompletionTokens: 1_000_000, HasInputVideo: true, Plan: plan, RateMultiplier: 1,
	})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)

	// Missing upstream token usage must not be recorded as a zero-cost success.
	_, err = svc.CalculateVideoCostFromPlan(VideoCostInput{
		Model: "doubao-seedance-2-5-260628", Resolution: "720p",
		CompletionTokens: 0, Plan: plan, RateMultiplier: 1,
	})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

// Fail closed: no plan, invalid mode, or a mode with no price never yields a cost.
func TestVideoCostPlanFailsClosed(t *testing.T) {
	t.Parallel()
	svc := &BillingService{}

	_, err := svc.CalculateVideoCostFromPlan(VideoCostInput{Model: "doubao-seedance", VideoCount: 1})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)

	_, err = svc.CalculateVideoCostFromPlan(VideoCostInput{
		Model: "doubao-seedance", VideoCount: 1,
		Plan: &domain.VideoPricePlan{Mode: "per_minute"},
	})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)

	// A declared mode with every price missing is unconfigured, not free.
	_, err = svc.CalculateVideoCostFromPlan(VideoCostInput{
		Model: "doubao-seedance", VideoCount: 1,
		Plan: &domain.VideoPricePlan{Mode: domain.VideoBillingModePerRequest},
	})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

// The group-bound entry resolves the plan from group.VideoPricePlans by price
// family, and refuses when the family has no plan at all.
func TestCalculateVideoCostForGroupResolvesPlanByFamily(t *testing.T) {
	t.Parallel()
	group := &Group{VideoPricePlans: map[string]domain.VideoPricePlan{
		VideoPriceFamilySeedance: {
			Mode: domain.VideoBillingModePerToken,
			PerTokenPrices: map[string]domain.VideoTokenPrice{
				VideoBillingResolution720P: {WithoutInputVideo: videoPlanPtr(2.0)},
			},
		},
	}}
	svc := &BillingService{}
	cost, err := svc.CalculateVideoCostForGroup(VideoCostInput{
		Model: "doubao-seedance-2-5-260628", Resolution: "720p",
		CompletionTokens: 1_000_000, RateMultiplier: 1,
	}, group)
	require.NoError(t, err)
	require.InDelta(t, 2.0, cost.TotalCost, 1e-12)

	// A model with no family/plan is not priceable even though the group has a
	// Seedance plan: plans are per family, never global.
	_, err = svc.CalculateVideoCostForGroup(VideoCostInput{
		Model: "some-unknown-video", VideoCount: 1, RateMultiplier: 1,
	}, group)
	require.True(t, errors.Is(err, ErrModelPricingUnavailable))

	// Nil group fails closed.
	_, err = svc.CalculateVideoCostForGroup(VideoCostInput{
		Model: "doubao-seedance", VideoCount: 1, RateMultiplier: 1,
	}, nil)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

// The plan gate treats a mode-without-prices as unconfigured (fail closed).
func TestGroupHasVideoPricePlanRequiresPrices(t *testing.T) {
	t.Parallel()
	require.False(t, GroupHasVideoPricePlan(nil, "doubao-seedance"))
	require.False(t, GroupHasVideoPricePlan(&Group{}, "doubao-seedance"))

	emptyMode := &Group{VideoPricePlans: map[string]domain.VideoPricePlan{
		VideoPriceFamilySeedance: {Mode: domain.VideoBillingModePerToken},
	}}
	require.False(t, GroupHasVideoPricePlan(emptyMode, "doubao-seedance-2-5-260628"))

	invalidMode := &Group{VideoPricePlans: map[string]domain.VideoPricePlan{
		VideoPriceFamilySeedance: {Mode: "per_minute", PerRequestPrice: videoPlanPtr(1)},
	}}
	require.False(t, GroupHasVideoPricePlan(invalidMode, "doubao-seedance"))

	priced := &Group{VideoPricePlans: map[string]domain.VideoPricePlan{
		VideoPriceFamilySeedance: {
			Mode: domain.VideoBillingModePerToken,
			PerTokenPrices: map[string]domain.VideoTokenPrice{
				VideoBillingResolution1080P: {WithInputVideo: videoPlanPtr(9.0)},
			},
		},
	}}
	require.True(t, GroupHasVideoPricePlan(priced, "doubao-seedance-2-5-260628"))
}

// Grok per_second regression: the legacy CalculateVideoCost path is unchanged and
// still bills per second with the flat/per-model tariff.
func TestCalculateVideoCostLegacyPerSecondUnchanged(t *testing.T) {
	t.Parallel()
	svc := &BillingService{}
	perSecond := 0.14
	cost := svc.CalculateVideoCost("grok-imagine-video-1.5", "720p", 2, 10, &VideoPriceConfig{
		Price720P: &perSecond,
	}, 1)
	require.InDelta(t, 2.8, cost.TotalCost, 1e-12)
	require.Equal(t, string(BillingModeVideo), cost.BillingMode)

	// Defaults still apply when the group configured nothing.
	defaulted := svc.CalculateVideoCost("grok-imagine-video", "480p", 1, 0, nil, 1)
	require.InDelta(t, defaultGrokImagineVideoPrice480P*float64(VideoBillingDefaultDurationSeconds), defaulted.TotalCost, 1e-12)
}
