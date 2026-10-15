//go:build unit

package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

// Regression for the missing 4k tier. The official Ark Seedance price table lists a
// 4k rate (PRD 22:54), but the resolution enumerations were three independent literal
// lists, so 4k was absent from all of them: an operator could not enter a 4k unit
// price, and the serializer silently dropped it.
//
// VideoBillingResolutionTiers is now the single list every enumeration uses. This
// test pins the tier set and the aliases, so adding or removing a tier fails loudly
// instead of silently skewing which tiers can be priced.
func TestVideoBillingResolutionTiersInclude4K(t *testing.T) {
	require.Contains(t, VideoBillingResolutionTiers, VideoBillingResolution4K,
		"4k is a real Seedance tier and must be configurable")
	require.Len(t, VideoBillingResolutionTiers, 4)
	// Order is low to high; callers rely on it for "cheapest tier" fallbacks.
	require.Equal(t, []string{"480p", "720p", "1080p", "4k"}, VideoBillingResolutionTiers)

	for _, alias := range []string{"4k", "4K", "2160", "2160p", "uhd", "ultra_hd", " 4K "} {
		normalized, ok := LookupVideoBillingResolution(alias)
		require.True(t, ok, "alias %q must be recognised", alias)
		require.Equal(t, VideoBillingResolution4K, normalized)
	}

	// Every tier in the shared list must round-trip through the strict lookup, so a
	// configured price can never fail to match at settlement time.
	for _, tier := range VideoBillingResolutionTiers {
		normalized, ok := LookupVideoBillingResolution(tier)
		require.True(t, ok, "tier %q must be recognised", tier)
		require.Equal(t, tier, normalized)
	}

	// An unknown tier still fails closed rather than silently becoming 480p.
	_, ok := LookupVideoBillingResolution("1080i")
	require.False(t, ok, "an unrecognised tier must not be silently accepted")
}

// A 4k per-token price must be accepted by the plan lookup and billed at that tier,
// which is the operator-visible half of the missing-4k defect.
func TestSeedance4KPriceIsBillable(t *testing.T) {
	price4k, price1080 := 26.0, 77.0
	plan := &domain.VideoPricePlan{
		Mode: domain.VideoBillingModePerToken,
		PerTokenPrices: map[string]domain.VideoTokenPrice{
			VideoBillingResolution4K:   {WithoutInputVideo: &price4k},
			VideoBillingResolution1080P: {WithoutInputVideo: &price1080},
		},
	}
	require.Equal(t, &price4k, VideoPlanTokenUnitPrice(plan, VideoBillingResolution4K, false),
		"a configured 4k rate must be used for a 4k result")
	require.Equal(t, &price1080, VideoPlanTokenUnitPrice(plan, VideoBillingResolution1080P, false),
		"and must not leak into the 1080p tier")

	// A group carrying only a 4k slot is a configured Seedance price.
	group := &Group{VideoPricePlans: map[string]domain.VideoPricePlan{VideoPriceFamilySeedance: *plan}}
	require.True(t, GroupHasSeedanceVideoPrice(group),
		"a 4k-only configuration must pass the create gate")
	require.NotNil(t, SeedanceLegacyPerSecondPlan(&Group{
		VideoModelPrices: map[string]map[string]float64{
			VideoPriceFamilySeedance: {VideoBillingResolution4K: 0.5},
		},
	}, "doubao-seedance"), "a 4k-only legacy slot must be usable at settlement")
}
