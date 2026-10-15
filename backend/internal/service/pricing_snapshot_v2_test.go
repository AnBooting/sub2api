//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

// Regression for the C-1 lock-price snapshot contract (project-docs/prd/11-共享契约.md).
//
// The frontend requires pricing_snapshot.v === 2 and renders locked unit prices,
// multipliers and the final credit amount from it. The backend previously emitted only
// the v1 shape (currency/source/quota_unit/total_cost/actual_cost), so every row fell
// into the "missing snapshot" branch: users could never see what they were charged per
// unit, and the contract's recompute check could not hold. These tests pin the v2
// producer and the recompute invariant.
func TestCostPricingSnapshotEmitsV2WithRecomputableItems(t *testing.T) {
	bs := newNativeBillingTest(t)
	resolver := NewModelPricingResolver(nil, bs)
	price := 2e-6
	group := &Group{ID: 12, ModelPricing: []ChannelModelPricing{{
		Models: []string{"glm-test"}, Currency: "USD",
		InputPrice: &price, OutputPrice: &price,
	}}}
	cost, err := bs.CalculateTokenCostForRequest(TokenCostRequest{
		Ctx: context.Background(), Model: "glm-test", Group: group,
		Tokens:        UsageTokens{InputTokens: 500_000, OutputTokens: 100_000},
		RateMultiplier: 1.2, Resolver: resolver,
	})
	require.NoError(t, err)

	var snapshot map[string]any
	require.NoError(t, json.Unmarshal(costPricingSnapshot(cost), &snapshot))

	// Version and unit are what the frontend gates on.
	require.EqualValues(t, 2, snapshot["v"], "the frontend only renders v===2 snapshots")
	require.Equal(t, "credits", snapshot["quota_unit"])
	require.Equal(t, PricingSnapshotUnitPer1MTokens, snapshot["unit"])

	// Items carry the locked unit price, the usage and the subtotal.
	items, ok := snapshot["items"].([]any)
	require.True(t, ok, "v2 must carry an items array")
	require.NotEmpty(t, items)
	byKind := map[string]map[string]any{}
	for _, raw := range items {
		item := raw.(map[string]any)
		byKind[item["kind"].(string)] = item
	}
	require.Contains(t, byKind, "input")
	require.Contains(t, byKind, "output")
	// base_credit_price is per 1M tokens: 2e-6 * 1e6 = 2.
	require.InDelta(t, 2.0, byKind["input"]["base_credit_price"].(float64), 1e-9)
	require.InDelta(t, 500_000.0, byKind["input"]["usage"].(float64), 1e-9)
	require.InDelta(t, 0.5, byKind["input"]["normalized_usage"].(float64), 1e-12)
	require.InDelta(t, 1.0, byKind["input"]["subtotal"].(float64), 1e-12)

	// C-1 recompute check: total_credit == group_multiplier * sum(subtotal).
	multiplier := snapshot["group_multiplier"].(float64)
	totalCredit := snapshot["total_credit"].(float64)
	sum := 0.0
	for _, raw := range items {
		sum += raw.(map[string]any)["subtotal"].(float64)
	}
	require.InDelta(t, multiplier*sum, totalCredit, 1e-9,
		"the recompute check in the contract must hold for every v2 snapshot")
	require.InDelta(t, cost.ActualCost, totalCredit, 1e-12)
}

// A zero-usage or unpriced breakdown must not claim a multiplier: dividing by a zero
// total would otherwise report NaN/Inf into the snapshot.
func TestCostPricingSnapshotZeroCostDoesNotInventMultiplier(t *testing.T) {
	cost := &CostBreakdown{PricingCurrency: "USD", PricingSource: "manual"}
	require.Zero(t, cost.EffectiveMultiplier())
	// No items and no media: keep the historical v1 shape so old rows stay readable.
	var snapshot map[string]any
	require.NoError(t, json.Unmarshal(costPricingSnapshot(cost), &snapshot))
	_, hasVersion := snapshot["v"]
	require.False(t, hasVersion, "a breakdown without v2 facts must stay v1")
	require.Equal(t, "credits", snapshot["quota_unit"])
}

// v1 must remain byte-compatible for historical readers: the fields the existing test
// asserts (currency/source/quota_unit) are unchanged.
func TestCostPricingSnapshotV1FieldsPreserved(t *testing.T) {
	cost := &CostBreakdown{
		PricingCurrency: "CNY", PricingSource: "manual", TotalCost: 3, ActualCost: 4.5,
		PricingSnapshot: &ModelPricing{InputPricePerToken: 1e-6},
	}
	var snapshot map[string]any
	require.NoError(t, json.Unmarshal(costPricingSnapshot(cost), &snapshot))
	require.Equal(t, "CNY", snapshot["currency"])
	require.Equal(t, "manual", snapshot["source"])
	require.Equal(t, "credits", snapshot["quota_unit"])
	require.Equal(t, 1e-6, snapshot["input_per_token"])
}

// The media facts must reflect the metering mode and the duration source honestly.
func TestPricingSnapshotMediaFacts(t *testing.T) {
	cost := &CostBreakdown{BillingMode: string(BillingModeVideo)}
	require.Equal(t, "per_second", mediaBillingModeForSnapshot(cost))
	cost = &CostBreakdown{BillingMode: string(BillingModePerRequest)}
	require.Equal(t, "per_request", mediaBillingModeForSnapshot(cost))
	cost = &CostBreakdown{BillingMode: string(BillingModeToken)}
	require.Equal(t, "", mediaBillingModeForSnapshot(cost))

	require.Equal(t, "", videoSecondsSource(nil))
	require.Equal(t, "", videoSecondsSource(&OpenAIForwardResult{VideoDurationSeconds: 0}))
	require.Equal(t, "request", videoSecondsSource(&OpenAIForwardResult{VideoDurationSeconds: 10}))
	require.Equal(t, "upstream", videoSecondsSource(&OpenAIForwardResult{VideoDurationSeconds: 10, VideoDurationFromStatus: true}))
}

// price_version must be stable for the same card and change when the rates change,
// so a stored bill can be traced back to the price table it used.
func TestNativeCNYPriceVersionStableAndRateSensitive(t *testing.T) {
	at := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	base := &ModelPricing{PricingSource: "native_cny", NativePricingAt: at, InputPricePerToken: 2e-6, OutputPricePerToken: 4e-6}
	require.Equal(t, nativeCNYPriceVersion(base), nativeCNYPriceVersion(base), "must be stable")

	changed := *base
	changed.InputPricePerToken = 3e-6
	require.NotEqual(t, nativeCNYPriceVersion(base), nativeCNYPriceVersion(&changed),
		"a rate change must produce a new price_version")

	require.Equal(t, "", nativeCNYPriceVersion(&ModelPricing{}), "no fetch time means no version")
}

// The three video metering modes must each produce a recomputable v2 snapshot.
// per_request bills count, per_second bills count x duration, per_token bills
// completion tokens per million. The contract's recompute check must hold for all.
func TestVideoPlanSnapshotsAreRecomputable(t *testing.T) {
	bs := &BillingService{}
	perRequest, perSecond, perToken := 3.0, 0.5, 20.0

	for _, tc := range []struct {
		name     string
		plan     domain.VideoPricePlan
		input    VideoCostInput
		wantKind string
		wantUnit string
		wantCost float64
	}{
		{
			name:     "per_request",
			plan:     domain.VideoPricePlan{Mode: domain.VideoBillingModePerRequest, PerRequestPrice: &perRequest},
			input:    VideoCostInput{Model: "m", VideoCount: 2, RateMultiplier: 1.5},
			wantKind: "video_request", wantUnit: "per_request", wantCost: 6,
		},
		{
			name:     "per_second",
			plan:     domain.VideoPricePlan{Mode: domain.VideoBillingModePerSecond, PerSecondPrices: map[string]float64{VideoBillingResolution720P: perSecond}},
			input:    VideoCostInput{Model: "m", Resolution: VideoBillingResolution720P, VideoCount: 2, DurationSeconds: 10, RateMultiplier: 1.5},
			wantKind: "video_second", wantUnit: "per_second", wantCost: 10,
		},
		{
			name:     "per_token",
			plan:     domain.VideoPricePlan{Mode: domain.VideoBillingModePerToken, PerTokenPrices: map[string]domain.VideoTokenPrice{VideoBillingResolution720P: {WithoutInputVideo: &perToken}}},
			input:    VideoCostInput{Model: "m", Resolution: VideoBillingResolution720P, VideoCount: 1, CompletionTokens: 500_000, RateMultiplier: 1.5},
			wantKind: "video_token", wantUnit: PricingSnapshotUnitPer1MTokens, wantCost: 10,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.input.Plan = &tc.plan
			cost, err := bs.CalculateVideoCostFromPlan(tc.input)
			require.NoError(t, err)
			require.InDelta(t, tc.wantCost, cost.TotalCost, 1e-9)
			require.Equal(t, tc.wantUnit, cost.Unit)
			require.NotNil(t, cost.Media)

			var snapshot map[string]any
			require.NoError(t, json.Unmarshal(costPricingSnapshot(cost), &snapshot))
			require.EqualValues(t, 2, snapshot["v"])
			require.Equal(t, tc.wantUnit, snapshot["unit"])

			items := snapshot["items"].([]any)
			require.Len(t, items, 1)
			item := items[0].(map[string]any)
			require.Equal(t, tc.wantKind, item["kind"])

			// The contract's recompute check must hold here too.
			multiplier := snapshot["group_multiplier"].(float64)
			totalCredit := snapshot["total_credit"].(float64)
			require.InDelta(t, multiplier*item["subtotal"].(float64), totalCredit, 1e-9)
			require.InDelta(t, cost.ActualCost, totalCredit, 1e-12)

			media := snapshot["media"].(map[string]any)
			require.NotEmpty(t, media["mode"])
		})
	}
}


// The image and flat per-second video paths originally emitted no items, so their
// snapshots stayed v1 and the billing UI showed "missing snapshot" for them even
// after the token path was fixed. Both now write an item, which is what makes the
// lock price visible for media too.
func TestMediaCostPathsEmitRecomputableV2(t *testing.T) {
	bs := &BillingService{}

	t.Run("image per image", func(t *testing.T) {
		groupConfig := &ImagePriceConfig{Price1K: floatPtr(2.0)}
		cost := bs.CalculateImageCost("gpt-image-1", "1K", 3, groupConfig, 1.5)
		require.Equal(t, "per_image", cost.Unit)
		require.Len(t, cost.PricingItems, 1)
		require.Equal(t, "image", cost.PricingItems[0].Kind)
		require.InDelta(t, 6.0, cost.TotalCost, 1e-9)

		var snapshot map[string]any
		require.NoError(t, json.Unmarshal(costPricingSnapshot(cost), &snapshot))
		require.EqualValues(t, 2, snapshot["v"], "an image bill must carry a v2 snapshot")
		items := snapshot["items"].([]any)
		require.Len(t, items, 1)
		item := items[0].(map[string]any)
		require.InDelta(t, 2.0, item["base_credit_price"].(float64), 1e-9)
		require.InDelta(t, 3.0, item["usage"].(float64), 1e-9)
		multiplier := snapshot["group_multiplier"].(float64)
		require.InDelta(t, multiplier*item["subtotal"].(float64), snapshot["total_credit"].(float64), 1e-9)
	})

	t.Run("video per second", func(t *testing.T) {
		groupConfig := &VideoPriceConfig{Price720P: floatPtr(0.5)}
		cost := bs.CalculateVideoCost("grok-imagine-video", VideoBillingResolution720P, 2, 10, groupConfig, 1.5)
		require.Equal(t, "per_second", cost.Unit)
		require.NotNil(t, cost.Media)
		require.InDelta(t, 20.0, cost.Media.BillableSeconds, 1e-9)
		require.Len(t, cost.PricingItems, 1)
		require.Equal(t, "video_second", cost.PricingItems[0].Kind)

		var snapshot map[string]any
		require.NoError(t, json.Unmarshal(costPricingSnapshot(cost), &snapshot))
		require.EqualValues(t, 2, snapshot["v"], "a video bill must carry a v2 snapshot")
		require.Equal(t, "per_second", snapshot["unit"])
		item := snapshot["items"].([]any)[0].(map[string]any)
		multiplier := snapshot["group_multiplier"].(float64)
		require.InDelta(t, multiplier*item["subtotal"].(float64), snapshot["total_credit"].(float64), 1e-9)
	})

	// A zero-count request keeps the historical v1 shape (no items, no media).
	require.Empty(t, bs.CalculateImageCost("m", "1K", 0, nil, 1).PricingItems)
}

