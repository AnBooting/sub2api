//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// Red-team R-5 concerns the Free-Fast path (openai_gateway_usage.go:275-282): it
// overrides cost.ActualCost with the standard-tier amount while TotalCost and the
// items keep the priority-tier values.
//
// The reviewer claimed the C-1 recompute check fails there. Because group_multiplier
// is DERIVED as ActualCost/TotalCost, the identity still holds arithmetically — this
// test pins that, so the finding is classified on evidence rather than assumption.
// The real residual issue is that group_multiplier then reports a ratio
// (standard/priority) rather than the group's multiplier, which is a labelling
// concern, not a broken check.
func TestFreeFastOverrideKeepsRecomputeIdentityButDistortsMultiplier(t *testing.T) {
	// priority-tier bill, then the Free-Fast override to the standard amount.
	cost := &CostBreakdown{
		TotalCost:  2.0,
		ActualCost: 3.0, // 2.0 x 1.5
		PricingItems: []CostBreakdownItem{{
			Kind: "input", BaseCreditPrice: 4_000_000, Usage: 500, NormalizedUsage: 0.0005, Subtotal: 2.0,
		}},
	}
	standardActual := 1.5 // standard tier charged instead
	cost.ActualCost = standardActual

	var snapshot map[string]any
	require.NoError(t, json.Unmarshal(costPricingSnapshot(cost), &snapshot))

	sum := 0.0
	for _, raw := range snapshot["items"].([]any) {
		sum += raw.(map[string]any)["subtotal"].(float64)
	}
	multiplier := snapshot["group_multiplier"].(float64)
	totalCredit := snapshot["total_credit"].(float64)

	// The contract's identity holds even here, because the multiplier is derived.
	require.InDelta(t, multiplier*sum, totalCredit, 1e-9)
	require.InDelta(t, standardActual, totalCredit, 1e-12)

	// But the reported multiplier is a ratio, not the group's multiplier.
	require.InDelta(t, standardActual/cost.TotalCost, multiplier, 1e-12)
	require.NotEqual(t, 1.5, multiplier,
		"documents the labelling gap: the group multiplier is no longer reported")
}
