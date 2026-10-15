//go:build unit

package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

// Regression: groupMediaPricingLooksIncomplete decides whether a group's media pricing
// projection must be reloaded from the database. It checked VideoModelPrices but not
// VideoPricePlans, so a group configured only with metering plans looked like a blank
// shell and the fresh-group reload was skipped, weakening the second-chance pricing
// guard.
func TestGroupMediaPricingLooksIncompleteCoversVideoPricePlans(t *testing.T) {
	require.True(t, groupMediaPricingLooksIncomplete(nil))
	require.True(t, groupMediaPricingLooksIncomplete(&Group{}),
		"a genuinely empty group is incomplete")

	// A plan-only group is fully priced and must not be treated as a blank shell.
	require.False(t, groupMediaPricingLooksIncomplete(&Group{
		VideoPricePlans: map[string]domain.VideoPricePlan{
			VideoPriceFamilySeedance: {Mode: domain.VideoBillingModePerRequest},
		},
	}), "VideoPricePlans is a first-class pricing source")

	// The pre-existing signals must keep working.
	require.False(t, groupMediaPricingLooksIncomplete(&Group{
		VideoModelPrices: map[string]map[string]float64{
			VideoPriceFamilySeedance: {VideoBillingResolution720P: 1},
		},
	}))
	price := 1.0
	require.False(t, groupMediaPricingLooksIncomplete(&Group{VideoPrice720P: &price}))
	require.False(t, groupMediaPricingLooksIncomplete(&Group{WebSearchPricePerCall: &price}))
	require.False(t, groupMediaPricingLooksIncomplete(&Group{ImageRateIndependent: true}))
}
