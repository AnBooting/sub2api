//go:build unit

package service

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedImagePreflightInvalidFrozenRate(t *testing.T) {
	for _, rate := range []float64{-1, math.NaN(), math.Inf(1)} {
		f, ctx, bundle, scope := sharedImageFixture(t, BillingModeImage, 1)
		// Invalid non-finite live config is rejected while serializing the frozen version.
		if math.IsNaN(rate) || math.IsInf(rate, 0) {
			f.group.ImageRateMultiplier = rate
			_, _, err := f.svc.PrepareRequestBilling(context.Background(), f.key, InflightEstimateRequest{Model: "client-model"})
			require.Error(t, err)
		}
		// Corrupt a frozen test snapshot only to exercise consumer defense in depth.
		bundle.candidates[f.group.ID].group.ImageRateMultiplier = rate
		require.ErrorIs(t, bundle.ValidateSharedImagePricing(scope, "1K"), ErrModelPricingUnavailable)
		r := sharedLockedResult("claude-sonnet-4")
		r.ImageCount = 1
		require.ErrorIs(t, f.svc.RecordUsage(ctx, f.input(r)), ErrModelPricingUnavailable)
		require.Nil(t, f.ledger.lastCmd)
	}
}
func TestSharedImagePreflightTierAndExplicitZero(t *testing.T) {
	for _, zero := range []bool{false, true} {
		f, _, _, _ := sharedImageFixture(t, BillingModeImage, 2)
		price := 0.4
		if zero {
			price = 0
		}
		f.group.ModelPricing[0].PerRequestPrice = nil
		f.group.ModelPricing[0].Intervals = []PricingInterval{{TierLabel: "2K", PerRequestPrice: &price}}
		ctx, bundle, err := f.svc.PrepareRequestBilling(context.Background(), f.key, InflightEstimateRequest{Model: "client-model"})
		require.NoError(t, err)
		scope, err := bundle.ScopeFor(f.group.ID, "claude-sonnet-4", "")
		require.NoError(t, err)
		require.NoError(t, bundle.ValidateSharedImagePricing(scope, "2K"))
		require.ErrorIs(t, bundle.ValidateSharedImagePricing(scope, "1K"), ErrModelPricingUnavailable)
		ctx, err = WithGatewayRequestBillingAccount(ctx, f.group.ID, f.account.ID)
		require.NoError(t, err)
		result := sharedLockedResult("claude-sonnet-4")
		result.ImageCount = 2
		result.ImageSize = "2K"
		require.NoError(t, f.svc.RecordUsage(ctx, f.input(result)))
		require.InDelta(t, price*4, f.ledger.lastCmd.BalanceCost, 1e-12)
	}
}
func TestSharedImagePreflightNormalizesDefaultAndRejectsOtherModes(t *testing.T) {
	f, _, bundle, scope := sharedImageFixture(t, BillingModeImage, 1)
	require.NoError(t, bundle.ValidateSharedImagePricing(scope, ""))
	require.ErrorIs(t, bundle.ValidateSharedImagePricing(PriceScope{GroupID: 999, BillingModel: "client-model"}, "1K"), ErrModelPricingUnavailable)
	quote, err := bundle.PricingFor(scope)
	require.NoError(t, err)
	quote.resolved.Mode = BillingModeVideo
	_, err = calculateLockedSharedQuoteCost(quote, &ForwardResult{ImageCount: 1}, CostInput{})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Nil(t, f.ledger.lastCmd)
}
