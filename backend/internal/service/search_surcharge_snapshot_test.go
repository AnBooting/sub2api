//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// Red-team R-3: a search surcharge was folded into TotalCost/ActualCost without a
// matching item. Because group_multiplier is derived as ActualCost/TotalCost
// (billing_service.go:EffectiveMultiplier), the multiplier was diluted and the
// contract's recompute check stopped holding:
//
//   total_credit == group_multiplier x sum(items[].subtotal)   (11-共享契约.md:47)
//
// This drives mergeSearchSurcharge — the production helper the billing path calls —
// so the test fails if the helper stops emitting an item.
func TestSearchSurchargeKeepsSnapshotRecomputable(t *testing.T) {
	bs := &BillingService{}
	price := 2.0 // per 1k calls
	group := &Group{ID: 5, RateMultiplier: 1.5, SearchPricePer1k: &price}

	cost := &CostBreakdown{
		TotalCost:  1.0,
		ActualCost: 1.5,
		BillingMode: string(BillingModeToken),
		Unit:        PricingSnapshotUnitPer1MTokens,
		PricingItems: []CostBreakdownItem{{
			Kind: "input", BaseCreditPrice: 2_000_000, Usage: 500, NormalizedUsage: 0.0005, Subtotal: 1.0,
		}},
	}

	searchCost := bs.CalculateSearchCost(3, group.GetSearchPricePer1k(), 1.5)
	require.NotNil(t, searchCost)
	require.NotZero(t, searchCost.TotalCost, "precondition: the surcharge is non-zero")
	mergeSearchSurcharge(cost, searchCost, 3)

	var snapshot map[string]any
	require.NoError(t, json.Unmarshal(costPricingSnapshot(cost), &snapshot))
	require.EqualValues(t, 2, snapshot["v"], "a bill with a surcharge must keep a v2 snapshot")

	items := snapshot["items"].([]any)
	require.Len(t, items, 2, "the surcharge must appear as its own item")
	require.Equal(t, "search", items[1].(map[string]any)["kind"])

	sum := 0.0
	for _, raw := range items {
		sum += raw.(map[string]any)["subtotal"].(float64)
	}
	multiplier := snapshot["group_multiplier"].(float64)
	totalCredit := snapshot["total_credit"].(float64)
	require.InDelta(t, multiplier*sum, totalCredit, 1e-9,
		"a search surcharge must be represented in items, or the snapshot cannot be recomputed")

	// The surcharge item must itself be self-consistent.
	surcharge := items[1].(map[string]any)
	require.InDelta(t, surcharge["base_credit_price"].(float64)*surcharge["normalized_usage"].(float64),
		surcharge["subtotal"].(float64), 1e-9)
}

// A zero surcharge must not add an empty item (the historical shape is preserved).
func TestSearchSurchargeZeroAddsNoItem(t *testing.T) {
	cost := &CostBreakdown{TotalCost: 1, ActualCost: 1, PricingItems: []CostBreakdownItem{{Kind: "input", Subtotal: 1}}}
	mergeSearchSurcharge(cost, &CostBreakdown{}, 2)
	require.Len(t, cost.PricingItems, 1, "a zero surcharge must not manufacture an item")
	require.InDelta(t, 1.0, cost.TotalCost, 1e-12)
}
