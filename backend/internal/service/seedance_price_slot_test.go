//go:build unit

package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

// PRD-06/K-4: Seedance has a reserved price slot and no bundled default.
func TestCanonicalVideoPriceFamilySeedance(t *testing.T) {
	for _, model := range []string{
		"seedance", "Seedance", "seedance-1-0-pro", "doubao-seedance-1-0-pro",
		"volcengine/seedance-1-0-lite", "ark/seedance", "bytedance/seedance",
	} {
		require.Equal(t, VideoPriceFamilySeedance, CanonicalVideoPriceFamily(model), "model=%s", model)
	}
	// Grok families keep their own slots and are never collapsed into Seedance.
	require.Equal(t, VideoPriceFamilyGrokImagineVideo, CanonicalVideoPriceFamily("grok-imagine-video"))
	require.Equal(t, VideoPriceFamilyGrokImagineVideo15, CanonicalVideoPriceFamily("grok-imagine-video-1.5"))
	require.Equal(t, "", CanonicalVideoPriceFamily("some-unknown-video"))
	require.Equal(t, "", CanonicalVideoPriceFamily(""))
}

// The Seedance slot is reachable through the per-model map on every tier.
func TestSeedanceVideoPriceSlotLookup(t *testing.T) {
	prices := NormalizeVideoModelPrices(map[string]map[string]float64{
		"seedance": {VideoBillingResolution720P: 0.11},
	})
	require.NotNil(t, LookupVideoModelPrice(prices, "seedance", VideoBillingResolution720P))
	require.Nil(t, LookupVideoModelPrice(prices, "seedance", VideoBillingResolution480P))
	// A Seedance request must not pick up a Grok family price.
	grok := NormalizeVideoModelPrices(map[string]map[string]float64{
		VideoPriceFamilyGrokImagineVideo: {VideoBillingResolution720P: 0.07},
	})
	require.Nil(t, LookupVideoModelPrice(grok, "seedance", VideoBillingResolution720P))
}

// Fail-closed: with no operator-set Seedance price the group must not be sellable,
// and an unrelated Grok-only price must not be treated as a Seedance slot.
func TestGroupHasSeedanceVideoPriceFailsClosed(t *testing.T) {
	require.False(t, GroupHasSeedanceVideoPrice(nil))
	require.False(t, GroupHasSeedanceVideoPrice(&Group{}))

	grokOnly := &Group{VideoModelPrices: map[string]map[string]float64{
		VideoPriceFamilyGrokImagineVideo: {VideoBillingResolution720P: 0.07},
	}}
	require.False(t, GroupHasSeedanceVideoPrice(grokOnly), "a Grok tariff is not a Seedance tariff")

	perModel := &Group{VideoModelPrices: map[string]map[string]float64{
		"seedance": {VideoBillingResolution720P: 0.11},
	}}
	require.True(t, GroupHasSeedanceVideoPrice(perModel))

	// A single configured tier in the Seedance slot is enough.
	require.True(t, GroupHasSeedanceVideoPrice(&Group{VideoModelPrices: map[string]map[string]float64{
		"seedance": {VideoBillingResolution1080P: 0.25},
	}}))

	// A metering plan is the first-class Seedance price source.
	require.True(t, GroupHasSeedanceVideoPrice(&Group{VideoPricePlans: map[string]domain.VideoPricePlan{
		VideoPriceFamilySeedance: {Mode: domain.VideoBillingModePerToken, PerTokenPrices: map[string]domain.VideoTokenPrice{
			VideoBillingResolution720P: {WithoutInputVideo: ptrFloat(70)},
		}},
	}}))

	// The flat video_price_* columns are xAI's per-second tariff and are NOT a
	// Seedance slot: a Seedance result never reaches that settlement path (its
	// VideoCount stays 0), so admitting them here would accept the create and then
	// settle at zero cost. Fail closed instead.
	flat := 0.2
	require.False(t, GroupHasSeedanceVideoPrice(&Group{VideoPrice720P: &flat}),
		"a flat Grok per-second tariff cannot settle a Seedance job")
	require.False(t, GroupHasSeedanceVideoPrice(&Group{
		VideoPrice480P: &flat, VideoPrice720P: &flat, VideoPrice1080P: &flat,
	}), "flat columns alone must not admit Seedance")

	// A plan that declares a mode but carries no price is not a configured price.
	require.False(t, GroupHasSeedanceVideoPrice(&Group{VideoPricePlans: map[string]domain.VideoPricePlan{
		VideoPriceFamilySeedance: {Mode: domain.VideoBillingModePerToken},
	}}), "a plan without any priced tier is not sellable")
}

