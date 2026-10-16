//go:build unit

package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func sharedImageFixture(t *testing.T, mode BillingMode, rate float64) (*sharedLockedFixture, context.Context, *RequestBillingContext, PriceScope) {
	f := newSharedLockedFixture(t, true, false)
	f.group.RateMultiplier = 2
	f.group.ImageRateIndependent = true
	f.group.ImageRateMultiplier = rate
	price := 0.25
	f.group.ModelPricing = []ChannelModelPricing{{Models: []string{"client-model", "claude-sonnet-4"}, BillingMode: mode, PerRequestPrice: &price}}
	f.account.Credentials["model_mapping"] = map[string]any{"client-model": "claude-sonnet-4"}
	snap := &inflightSnapshotCacheStub{byBucket: map[string][]Account{inflightBucketKey(f.group.ID, PlatformAnthropic): {*f.account}}}
	attachInflightSnapshot(f.svc, snap)
	ctx, bundle, err := f.svc.PrepareRequestBilling(context.Background(), f.key, InflightEstimateRequest{Model: "client-model"})
	require.NoError(t, err)
	ctx, err = WithGatewayRequestBillingAccount(ctx, f.group.ID, f.account.ID)
	require.NoError(t, err)
	scope, err := bundle.ScopeFor(f.group.ID, "claude-sonnet-4", "")
	require.NoError(t, err)
	return f, ctx, bundle, scope
}
func TestSharedLockedImageUsesFrozenPriceAndMultiplier(t *testing.T) {
	for _, mode := range []BillingMode{BillingModeImage, BillingModePerRequest} {
		for _, size := range []string{"1K", "2K", "4K"} {
			t.Run(fmt.Sprint(mode)+size, func(t *testing.T) {
				f, ctx, bundle, scope := sharedImageFixture(t, mode, 3)
				require.NoError(t, bundle.ValidateSharedImagePricing(scope, size))
				*f.group.ModelPricing[0].PerRequestPrice = 99
				f.group.ImageRateMultiplier = 88
				f.group.RateMultiplier = 77
				result := sharedLockedResult("claude-sonnet-4")
				result.ImageCount = 2
				result.ImageSize = size
				require.NoError(t, f.svc.RecordUsage(ctx, f.input(result)))
				require.NotNil(t, f.ledger.lastCmd)
				require.InDelta(t, 1.5, f.ledger.lastCmd.BalanceCost, 1e-12)
				require.InDelta(t, 3, f.logs.lastLog.RateMultiplier, 1e-12)
				require.Equal(t, 2, f.logs.lastLog.ImageCount)
			})
		}
	}
}
func TestSharedLockedImageTokenCardDoesNotDoubleCharge(t *testing.T) {
	f := newSharedLockedFixture(t, true, false)
	ctx := f.lockedContext(t, "claude-sonnet-4")
	result := sharedLockedResult("claude-sonnet-4")
	result.ImageCount = 4
	require.NoError(t, f.svc.RecordUsage(ctx, f.input(result)))
	require.InDelta(t, 0.002, f.ledger.lastCmd.BalanceCost, 1e-12)
}
func TestSharedLockedImageResponseBaselineUsesSameFormula(t *testing.T) {
	for _, price := range []float64{0.1, 0.5, 0} {
		t.Run(fmt.Sprint(price), func(t *testing.T) {
			f, _, _, _ := sharedImageFixture(t, BillingModeImage, 3)
			f.group.ModelPricing = append(f.group.ModelPricing, ChannelModelPricing{Models: []string{"response-image"}, BillingMode: BillingModeImage, PerRequestPrice: &price})
			ctx, bundle, err := f.svc.PrepareRequestBilling(context.Background(), f.key, InflightEstimateRequest{Model: "client-model"})
			require.NoError(t, err)
			// Isolate response-model policy while preserving real frozen cards/account facts.
			bundle.candidates[f.group.ID].mapping.BillingModelSource = BillingModelSourceResponse
			ctx, err = WithGatewayRequestBillingAccount(ctx, f.group.ID, f.account.ID)
			require.NoError(t, err)
			result := sharedLockedResult("claude-sonnet-4")
			result.UpstreamResponseModel = "response-image"
			result.ImageCount = 3
			result.ImageSize = "2K"
			require.NoError(t, f.svc.RecordUsage(ctx, f.input(result)))
			want := 2.25
			if price == 0.1 {
				want = 0.9
			}
			require.InDelta(t, want, f.ledger.lastCmd.BalanceCost, 1e-12)
			require.InDelta(t, 3, f.logs.lastLog.RateMultiplier, 1e-12)
		})
	}
}
func TestSharedLockedImageScalarOnlyDoesNotCreateQuote(t *testing.T) {
	f := newSharedLockedFixture(t, false, false)
	p := 0.2
	f.group.ImagePrice1K = &p
	ctx := f.lockedContextUnvalidated(t, "unknown-image-model")
	result := sharedLockedResult("unknown-image-model")
	result.ImageCount = 1
	require.ErrorIs(t, f.svc.RecordUsage(ctx, f.input(result)), ErrModelPricingUnavailable)
	require.Nil(t, f.ledger.lastCmd)
}
func TestSharedLockedImageSharedAndZeroRate(t *testing.T) {
	for _, rate := range []float64{0, 2} {
		f, _, _, _ := sharedImageFixture(t, BillingModeImage, rate)
		f.group.ImageRateIndependent = rate == 0
		ctx, bundle, err := f.svc.PrepareRequestBilling(context.Background(), f.key, InflightEstimateRequest{Model: "client-model"})
		require.NoError(t, err)
		ctx, err = WithGatewayRequestBillingAccount(ctx, f.group.ID, f.account.ID)
		require.NoError(t, err)
		// A frozen token peak must not multiply per-image charges.
		bundle.candidates[f.group.ID].tokenMultiplier = 20
		r := sharedLockedResult("claude-sonnet-4")
		r.ImageCount = 2
		require.NoError(t, f.svc.RecordUsage(ctx, f.input(r)))
		require.InDelta(t, 0.5*rate, f.ledger.lastCmd.BalanceCost, 1e-12)
	}
}
func TestSharedLockedImageZeroCountRetainsRequestCardSemantics(t *testing.T) {
	for _, mode := range []BillingMode{BillingModeImage, BillingModePerRequest} {
		f, ctx, _, _ := sharedImageFixture(t, mode, 3)
		require.NoError(t, f.svc.RecordUsage(ctx, f.input(sharedLockedResult("claude-sonnet-4"))))
		require.InDelta(t, 0.5, f.ledger.lastCmd.BalanceCost, 1e-12)
	}
}
func TestSharedImagePreflightNilBundle(t *testing.T) {
	var bundle *RequestBillingContext
	require.ErrorIs(t, bundle.ValidateSharedImagePricing(PriceScope{}, "1K"), ErrModelPricingUnavailable)
}
func TestSharedLockedImageResponseCannotBypassMissingBaselineTier(t *testing.T) {
	f, _, _, _ := sharedImageFixture(t, BillingModeImage, 3)
	p := 0.1
	expensive := 9.0
	f.group.ModelPricing[0].PerRequestPrice = nil
	f.group.ModelPricing[0].Intervals = []PricingInterval{{TierLabel: "4K", PerRequestPrice: &p}}
	f.group.ModelPricing = append(f.group.ModelPricing, ChannelModelPricing{Models: []string{"response-image"}, BillingMode: BillingModeImage, PerRequestPrice: &expensive})
	ctx, bundle, err := f.svc.PrepareRequestBilling(context.Background(), f.key, InflightEstimateRequest{Model: "client-model"})
	require.NoError(t, err)
	bundle.candidates[f.group.ID].mapping.BillingModelSource = BillingModelSourceResponse
	ctx, err = WithGatewayRequestBillingAccount(ctx, f.group.ID, f.account.ID)
	require.NoError(t, err)
	r := sharedLockedResult("claude-sonnet-4")
	r.ImageCount = 2
	r.ImageSize = "1K"
	r.UpstreamResponseModel = "response-image"
	require.ErrorIs(t, f.svc.RecordUsage(ctx, f.input(r)), ErrModelPricingUnavailable)
	require.Nil(t, f.ledger.lastCmd)
	require.Nil(t, f.logs.lastLog)
}
