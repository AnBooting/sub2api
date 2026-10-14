//go:build unit

package service

import (
	"testing"

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

	// A single configured tier is enough; a flat column also counts.
	require.True(t, GroupHasSeedanceVideoPrice(&Group{VideoModelPrices: map[string]map[string]float64{
		"seedance": {VideoBillingResolution1080P: 0.25},
	}}))
	flat := 0.2
	require.True(t, GroupHasSeedanceVideoPrice(&Group{VideoPrice720P: &flat}))
}
