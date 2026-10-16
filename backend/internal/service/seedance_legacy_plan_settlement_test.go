//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

// Regression for the B-4 money defect: the create-time gate
// (GroupHasSeedanceVideoPrice) accepts the legacy per-model video price slots, but
// settlement only recognised a metering plan. A group configured with legacy slots
// alone therefore passed the gate, generated upstream, and then never entered the
// video cost path — it fell through to LLM token billing and was recorded at zero
// cost. SeedanceLegacyPerSecondPlan was written to bridge exactly this gap but had no
// caller.
//
// These tests pin the invariant the fix relies on: the routing predicate and the
// legacy plan synthesis must cover the same configurations the create gate accepts.
func TestSeedanceLegacyPerSecondPlanSynthesizesFromPerModelSlots(t *testing.T) {
	price720 := 0.5
	group := &Group{
		VideoModelPrices: map[string]map[string]float64{
			VideoPriceFamilySeedance: {VideoBillingResolution720P: price720},
		},
	}
	plan := SeedanceLegacyPerSecondPlan(group, "doubao-seedance")
	require.NotNil(t, plan, "a legacy per-model slot must synthesize a per_second plan")
	require.Equal(t, domain.VideoBillingModePerSecond, plan.Mode)
	require.Equal(t, price720, plan.PerSecondPrices[VideoBillingResolution720P])
}

func TestSeedanceLegacyPerSecondPlanFailsClosedWithoutAnySlot(t *testing.T) {
	require.Nil(t, SeedanceLegacyPerSecondPlan(nil, "doubao-seedance"))
	require.Nil(t, SeedanceLegacyPerSecondPlan(&Group{}, "doubao-seedance"))
	// A Grok tariff is not a Seedance tariff.
	require.Nil(t, SeedanceLegacyPerSecondPlan(&Group{
		VideoModelPrices: map[string]map[string]float64{
			VideoPriceFamilyGrokImagineVideo15: {VideoBillingResolution720P: 0.1},
		},
	}, "doubao-seedance"))
}

// The core invariant: whatever the create gate accepts must be routable to the video
// cost path. Before the fix, a legacy-only group passed GroupHasSeedanceVideoPrice but
// seedanceVideoPricingAvailable was false, so the result never reached settlement.
func TestSeedanceRoutingGateMatchesCreateGate(t *testing.T) {
	perModel := &Group{
		VideoModelPrices: map[string]map[string]float64{
			VideoPriceFamilySeedance: {VideoBillingResolution720P: 0.5},
		},
	}
	perRequest := 3.0
	planOnly := &Group{
		VideoPricePlans: map[string]domain.VideoPricePlan{
			VideoPriceFamilySeedance: {Mode: domain.VideoBillingModePerRequest, PerRequestPrice: &perRequest},
		},
	}
	unpriced := &Group{}

	for _, tc := range []struct {
		name  string
		group *Group
	}{
		{"legacy per-model only", perModel},
		{"metering plan only", planOnly},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := &APIKey{Group: tc.group}
			require.True(t, GroupHasSeedanceVideoPrice(tc.group),
				"the create gate accepts this configuration")
			require.True(t, seedanceVideoPricingAvailable(key, "doubao-seedance"),
				"settlement must be able to price exactly what the create gate accepts")
		})
	}

	// And the fail-closed direction: a group with no Seedance price is rejected by both.
	require.False(t, GroupHasSeedanceVideoPrice(unpriced))
	require.False(t, seedanceVideoPricingAvailable(&APIKey{Group: unpriced}, "doubao-seedance"))
	// A non-Seedance model is never routed here.
	require.False(t, seedanceVideoPricingAvailable(&APIKey{Group: perModel}, "grok-imagine-video-1.5"))
}

// End-to-end guard for the B-4 money defect. The previous unit tests only exercised
// the routing predicate, so reverting the call site to hasSeedanceVideoPlan still left
// them green. This test drives the real cost entry point: a group configured with ONLY
// the legacy per-model slots must produce a non-zero video cost for a Seedance result
// instead of falling through to LLM token billing.
func TestSeedanceLegacyOnlyGroupSettlesNonZeroThroughCostEntry(t *testing.T) {
	t.Parallel()
	svc := &OpenAIGatewayService{billingService: &BillingService{}}
	groupID := int64(42)

	// Legacy-only configuration: no video_price_plans at all.
	group := &Group{
		ID:       groupID,
		Platform: PlatformOpenAI,
		VideoModelPrices: map[string]map[string]float64{
			VideoPriceFamilySeedance: {VideoBillingResolution720P: 0.5},
		},
	}
	apiKey := &APIKey{ID: 1, GroupID: &groupID, Group: group}
	require.True(t, GroupHasSeedanceVideoPrice(group), "the create gate accepts this group")

	// Ark Seedance results carry token usage but no VideoCount.
	result := &OpenAIForwardResult{
		Model:                "doubao-seedance",
		BillingModel:         "doubao-seedance",
		VideoResolution:      VideoBillingResolution720P,
		VideoDurationSeconds: 10,
	}
	cost, err := svc.calculateOpenAIRecordUsageCost(
		context.Background(), result, apiKey, []string{"doubao-seedance"},
		1.0, 1.0, 1.0, 1.0, UsageTokens{InputTokens: 10, OutputTokens: 20},
		"", boolPtr(false), time.Time{},
	)
	require.NoError(t, err, "a group the create gate accepts must be priceable")
	require.NotNil(t, cost)
	require.InDelta(t, 0.5*10, cost.TotalCost, 1e-9,
		"legacy per-second slot must bill duration x unit price, not zero")
	require.Positive(t, cost.ActualCost, "the customer must be charged, not zero-cost")
}

// The fail-closed direction through the same entry point: a group with no Seedance
// price at all must not silently settle at zero.
func TestSeedanceUnpricedGroupFailsClosedThroughCostEntry(t *testing.T) {
	t.Parallel()
	svc := &OpenAIGatewayService{billingService: &BillingService{}}
	groupID := int64(43)
	apiKey := &APIKey{ID: 1, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformOpenAI}}
	require.False(t, GroupHasSeedanceVideoPrice(apiKey.Group))

	result := &OpenAIForwardResult{
		Model:                "doubao-seedance",
		BillingModel:         "doubao-seedance",
		VideoResolution:      VideoBillingResolution720P,
		VideoDurationSeconds: 10,
	}
	_, err := svc.calculateOpenAIRecordUsageCost(
		context.Background(), result, apiKey, []string{"doubao-seedance"},
		1.0, 1.0, 1.0, 1.0, UsageTokens{InputTokens: 10, OutputTokens: 20},
		"", boolPtr(false), time.Time{},
	)
	require.Error(t, err, "an unpriced Seedance result must fail closed rather than settle at zero")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

