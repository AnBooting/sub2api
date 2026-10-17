//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// Adversarial probes for the C-1 recompute identity. A red-team would try to find inputs
// where the stored snapshot stops reconciling, or where group_multiplier becomes a
// meaningless value that the UI would render as a discount.
func TestSnapshotRecomputeAdversarialEdges(t *testing.T) {
	recompute := func(t *testing.T, cost *CostBreakdown) (float64, float64, float64) {
		t.Helper()
		var snapshot map[string]any
		require.NoError(t, json.Unmarshal(costPricingSnapshot(cost), &snapshot))
		require.EqualValues(t, 2, snapshot["v"], "the v2 shape is required for these probes")
		multiplier := snapshot["group_multiplier"].(float64)
		totalCredit := snapshot["total_credit"].(float64)
		sum := 0.0
		for _, raw := range snapshot["items"].([]any) {
			item := raw.(map[string]any)
			sum += item["subtotal"].(float64)
			// Self-consistency: subtotal must equal base price x normalized usage, or a
			// consumer recomputing from the unit price gets a different number.
			require.InDelta(t, item["base_credit_price"].(float64)*item["normalized_usage"].(float64),
				item["subtotal"].(float64), 1e-6, "subtotal must follow from the published unit price")
		}
		return multiplier, totalCredit, sum
	}

	t.Run("free group with usage keeps the identity", func(t *testing.T) {
		bs := newNativeBillingTest(t)
		group := &Group{ID: 3, RateMultiplier: 0}
		cost, err := bs.CalculateTokenCostForRequest(TokenCostRequest{
			Ctx: context.Background(), Model: "glm-test", Group: group,
			Tokens:         UsageTokens{InputTokens: 1000, OutputTokens: 500},
			RateMultiplier: 0, Resolver: NewModelPricingResolver(nil, bs),
		})
		require.NoError(t, err)
		m, credit, sum := recompute(t, cost)
		require.Zero(t, m, "a free group must not report a positive multiplier")
		require.InDelta(t, m*sum, credit, 1e-9)
	})

	t.Run("repeated multipliers do not compound the identity", func(t *testing.T) {
		bs := newNativeBillingTest(t)
		price := 3e-6
		group := &Group{ID: 4, ModelPricing: []ChannelModelPricing{{
			Models: []string{"glm-test"}, Currency: "USD", InputPrice: &price, OutputPrice: &price,
		}}}
		cost, err := bs.CalculateTokenCostForRequest(TokenCostRequest{
			Ctx: context.Background(), Model: "glm-test", Group: group,
			Tokens:         UsageTokens{InputTokens: 200_000, OutputTokens: 100_000},
			RateMultiplier: 1.25, Resolver: NewModelPricingResolver(nil, bs),
		})
		require.NoError(t, err)

		// Apply several times: each application must keep the identity, not accumulate drift.
		for _, m := range []float64{1.1, 1.1, 0.5} {
			applyCostBreakdownMultiplier(cost, m)
			mult, credit, sum := recompute(t, cost)
			require.InDelta(t, mult*sum, credit, 1e-9, "identity must hold after each multiplier")
		}
	})
}
