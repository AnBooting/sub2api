//go:build unit

package service

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestBillingContextRejectsExplicitInvalidGroupIDs(t *testing.T) {
	for _, test := range []struct {
		name string
		key  *APIKey
	}{
		{"legacy_zero", &APIKey{GroupID: requestPricingTestInt64(0)}},
		{"legacy_negative", &APIKey{GroupID: requestPricingTestInt64(-1)}},
		{"normal_zero", &APIKey{Groups: []APIKeyGroupLink{{Role: APIKeyGroupRoleNormal, GroupID: 0}}}},
		{"ha_zero", &APIKey{Groups: []APIKeyGroupLink{{Role: APIKeyGroupRoleHA, GroupID: 0}}}},
		{"normal_negative", &APIKey{Groups: []APIKeyGroupLink{{Role: APIKeyGroupRoleNormal, GroupID: -1, Group: &Group{ID: -1}}}}},
		{"ha_negative", &APIKey{Groups: []APIKeyGroupLink{{Role: APIKeyGroupRoleHA, GroupID: -1, Group: &Group{ID: -1}}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, request, err := (inflightEstimateDeps{billing: newTestBillingService()}).prepareRequestBilling(context.Background(), test.key, InflightEstimateRequest{Model: "claude-sonnet-4"})
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			require.Nil(t, request)
			require.Nil(t, GatewayRequestBillingFromContext(ctx))
		})
	}
	ctx, request, err := (inflightEstimateDeps{billing: newTestBillingService()}).prepareRequestBilling(context.Background(), &APIKey{}, InflightEstimateRequest{Model: "claude-sonnet-4"})
	require.NoError(t, err)
	require.Same(t, request, GatewayRequestBillingFromContext(ctx))
	require.Nil(t, request.RequestedGroupID())
	require.Equal(t, []int64{0}, request.NormalGroupIDs())
	_, err = request.ScopeFor(0, "", "")
	require.NoError(t, err)
}

func TestRequestBillingContextPerRequestMissingAndExplicitZero(t *testing.T) {
	zero, one, five := 0.0, 1.0, 5.0
	for _, test := range []struct {
		name                        string
		root                        *float64
		tiers                       []PricingInterval
		size                        string
		want                        float64
		unavailable, calculateError bool
	}{
		{name: "root_nil_token_only_tier", tiers: []PricingInterval{{TierLabel: "small", InputPrice: &one}}, size: "small", unavailable: true},
		{name: "root_five_matched_zero", root: &five, tiers: []PricingInterval{{TierLabel: "small", PerRequestPrice: &zero}}, size: "small", want: 0},
		{name: "root_nil_matched_zero", tiers: []PricingInterval{{TierLabel: "small", PerRequestPrice: &zero}}, size: "small", want: 0},
		{name: "root_one_unmatched_default", root: &one, tiers: []PricingInterval{{TierLabel: "small", PerRequestPrice: &zero}}, size: "unknown", want: 1},
		{name: "root_nil_unmatched_size", tiers: []PricingInterval{{TierLabel: "small", PerRequestPrice: &zero}}, size: "unknown", calculateError: true},
		{name: "context_zero_no_default", tiers: []PricingInterval{{MinTokens: 1, MaxTokens: requestPricingTestInt(20), PerRequestPrice: &zero}}, want: 0},
		{name: "context_gap_no_default", tiers: []PricingInterval{{MinTokens: 50, MaxTokens: requestPricingTestInt(60), PerRequestPrice: &zero}}, calculateError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			card := ChannelModelPricing{Models: []string{"charge"}, BillingMode: BillingModePerRequest, Currency: "CNY", PerRequestPrice: test.root, Intervals: test.tiers}
			group := &Group{ID: 1, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{card}}
			_, request, err := (inflightEstimateDeps{billing: newTestBillingService()}).prepareRequestBilling(context.Background(), &APIKey{GroupID: requestPricingTestInt64(1), Group: group}, InflightEstimateRequest{Model: "charge"})
			require.NoError(t, err)
			quote, err := request.PricingFor(PriceScope{GroupID: 1, BillingModel: "charge"})
			require.NoError(t, err)
			if test.unavailable {
				require.ErrorIs(t, quote.Availability(), ErrModelPricingUnavailable)
				_, err = quote.Calculate(CostInput{})
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				return
			}
			require.NoError(t, quote.Availability())
			cost, err := quote.Calculate(CostInput{RequestCount: 1, SizeTier: test.size, Tokens: UsageTokens{InputTokens: 10}})
			if test.calculateError {
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, cost.ActualCost)
			require.Equal(t, "CNY", cost.PricingCurrency)
		})
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), -1} {
		group := &Group{ID: 1, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{Models: []string{"charge"}, BillingMode: BillingModePerRequest, PerRequestPrice: &value}}}
		_, request, err := (inflightEstimateDeps{billing: newTestBillingService()}).prepareRequestBilling(context.Background(), &APIKey{GroupID: requestPricingTestInt64(1), Group: group}, InflightEstimateRequest{Model: "charge"})
		// An unhashable nonfinite source can fail at freeze; otherwise pricing itself
		// must be unavailable. Neither case is allowed to become a zero charge.
		if err != nil {
			require.Nil(t, request)
			continue
		}
		quote, err := request.PricingFor(PriceScope{GroupID: 1, BillingModel: "charge"})
		require.NoError(t, err)
		require.ErrorIs(t, quote.Availability(), ErrModelPricingUnavailable)
	}
}
func requestPricingTestInt(value int) *int { return &value }

func TestCalculateCostUnified_SizeAndContextTierFallback(t *testing.T) {
	zero, quarter, one, other := 0.0, 0.25, 1.0, 0.9
	for _, test := range []struct {
		name        string
		root        *float64
		tiers       []PricingInterval
		want        float64
		unavailable bool
	}{
		{name: "size_without_named_uses_context_before_default", root: &one, tiers: []PricingInterval{{MinTokens: 0, PerRequestPrice: &quarter}}, want: 0.25},
		{name: "size_without_named_context_zero_before_default", root: &one, tiers: []PricingInterval{{MinTokens: 0, PerRequestPrice: &zero}}, want: 0},
		{name: "size_without_named_context_price_without_default", tiers: []PricingInterval{{MinTokens: 0, PerRequestPrice: &quarter}}, want: 0.25},
		{name: "context_miss_uses_default", root: &one, tiers: []PricingInterval{{MinTokens: 11, PerRequestPrice: &quarter}}, want: 1},
		{name: "context_miss_without_default_rejects", tiers: []PricingInterval{{MinTokens: 11, PerRequestPrice: &quarter}}, unavailable: true},
		{name: "named_zero_before_context_and_default", root: &one, tiers: []PricingInterval{{TierLabel: "1K", PerRequestPrice: &zero}, {MinTokens: 0, PerRequestPrice: &quarter}}, want: 0},
		{name: "named_nil_uses_context_before_default", root: &one, tiers: []PricingInterval{{TierLabel: "1K", InputPrice: &one}, {MinTokens: 0, PerRequestPrice: &quarter}}, want: 0.25},
		{name: "other_named_price_is_not_context", tiers: []PricingInterval{{TierLabel: "2K", PerRequestPrice: &other}}, unavailable: true},
		{name: "other_named_price_does_not_hide_context", root: &one, tiers: []PricingInterval{{TierLabel: "2K", PerRequestPrice: &other}, {MinTokens: 0, PerRequestPrice: &quarter}}, want: 0.25},
	} {
		t.Run(test.name, func(t *testing.T) {
			billing := newTestBillingService()
			resolver := NewModelPricingResolver(nil, billing)
			group := &Group{ID: 1, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{Models: []string{"image-model"}, BillingMode: BillingModeImage, Currency: "CNY", PerRequestPrice: test.root, Intervals: test.tiers}}}
			// Resolve a real configured card, then preserve the existing nonempty
			// size + context input used by actual image settlement callers.
			resolved := resolver.Resolve(context.Background(), PricingInput{Model: "image-model", Group: group})
			input := CostInput{Ctx: context.Background(), Model: "image-model", RequestCount: 1, SizeTier: "1K", Tokens: UsageTokens{InputTokens: 10}, RateMultiplier: 1, Resolver: resolver, Resolved: resolved}
			cost, err := billing.CalculateCostUnified(input)
			if test.unavailable {
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				require.Nil(t, cost)
			} else {
				require.NoError(t, err)
				require.InDelta(t, test.want, cost.TotalCost, 1e-12)
				require.Equal(t, "CNY", cost.PricingCurrency)
			}
			// Exercise the opaque quote too; a helper-only success is insufficient.
			_, request, err := (inflightEstimateDeps{billing: billing}).prepareRequestBilling(context.Background(), &APIKey{GroupID: requestPricingTestInt64(1), Group: group}, InflightEstimateRequest{Model: "image-model"})
			require.NoError(t, err)
			scope, err := request.ScopeFor(1, "", "")
			require.NoError(t, err)
			quote, err := request.PricingFor(scope)
			require.NoError(t, err)
			cost, err = quote.Calculate(input)
			if test.unavailable {
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				require.Nil(t, cost)
				return
			}
			require.NoError(t, err)
			require.InDelta(t, test.want, cost.TotalCost, 1e-12)
			require.Equal(t, "CNY", cost.PricingCurrency)
		})
	}
}

func TestCalculateCostUnified_LegacyDefaultPresenceCompatibility(t *testing.T) {
	zero := 0.0
	for _, test := range []struct {
		name        string
		resolved    *ResolvedPricing
		size        string
		want        float64
		currency    string
		unavailable bool
	}{
		{name: "legacy_positive", resolved: &ResolvedPricing{Mode: BillingModePerRequest, DefaultPerRequestPrice: 0.07}, want: 0.14, currency: "USD"},
		{name: "legacy_zero_without_presence", resolved: &ResolvedPricing{Mode: BillingModePerRequest}, unavailable: true},
		{name: "legacy_negative", resolved: &ResolvedPricing{Mode: BillingModePerRequest, DefaultPerRequestPrice: -1}, unavailable: true},
		{name: "legacy_nan", resolved: &ResolvedPricing{Mode: BillingModePerRequest, DefaultPerRequestPrice: math.NaN()}, unavailable: true},
		{name: "legacy_infinite", resolved: &ResolvedPricing{Mode: BillingModePerRequest, DefaultPerRequestPrice: math.Inf(1)}, unavailable: true},
		{name: "matched_tier_zero_over_legacy_positive", resolved: &ResolvedPricing{Mode: BillingModePerRequest, DefaultPerRequestPrice: 0.07, RequestTiers: []PricingInterval{{TierLabel: "small", PerRequestPrice: &zero}}}, size: "small", want: 0, currency: "USD"},
		{name: "real_source_missing_default_cannot_use_stale_scalar", resolved: &ResolvedPricing{Mode: BillingModePerRequest, DefaultPerRequestPrice: 0.07, channelPricing: &ChannelModelPricing{BillingMode: BillingModePerRequest}}, unavailable: true},
		{name: "real_source_zero_over_legacy_positive", resolved: &ResolvedPricing{Mode: BillingModePerRequest, DefaultPerRequestPrice: 0.07, channelPricing: &ChannelModelPricing{BillingMode: BillingModePerRequest, Currency: "CNY", PerRequestPrice: &zero}}, want: 0, currency: "CNY"},
	} {
		t.Run(test.name, func(t *testing.T) {
			billing := newTestBillingService()
			resolver := NewModelPricingResolver(nil, billing)
			// The internal exported legacy scalar remains usable, but it is not
			// evidence of a real source for frozen request admission.
			if test.name == "legacy_positive" {
				require.ErrorIs(t, requestResolvedPricingAvailability(billing, "claude-sonnet-4", test.resolved), ErrModelPricingUnavailable)
			}
			cost, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "claude-sonnet-4", RequestCount: 2, RateMultiplier: 1, Resolver: resolver, Resolved: test.resolved, SizeTier: test.size})
			if test.unavailable {
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				require.Nil(t, cost)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, cost)
			require.InDelta(t, test.want, cost.TotalCost, 1e-12)
			require.InDelta(t, test.want, cost.ActualCost, 1e-12)
			require.Equal(t, test.currency, cost.PricingCurrency)
		})
	}
}

func TestRequestBillingContextSelectedAccountPolicyIsFrozen(t *testing.T) {
	for _, groupGate := range []bool{false, true} {
		name := "group_disabled"
		if groupGate {
			name = "group_enabled"
		}
		t.Run(name, func(t *testing.T) {
			one, zero, two := 1e-6, 0.0, 2.0
			// Real channel cards own manual ladders. Group token overrides are
			// deliberately flat under the existing resolver contract.
			cache := newEmptyChannelCache()
			cache.groupPlatform[1] = PlatformOpenAI
			cache.channelByGroupID[1] = &Channel{ID: 9, Status: StatusActive}
			cache.pricingByGroupModel[channelModelKey{groupID: 1, platform: PlatformOpenAI, model: "long-model"}] = &ChannelModelPricing{InputPrice: &one, OutputPrice: &one, CacheReadPrice: &zero, CacheWritePrice: &zero, Intervals: []PricingInterval{{MinTokens: 100, InputMultiplier: &two, OutputMultiplier: &two}}}
			group := &Group{ID: 1, Platform: PlatformOpenAI, RateMultiplier: 1, LongContextPricingEnabled: groupGate}
			results := map[int64]requestAccountMapping{11: {Model: "long-model", LongContextBillingEnabled: true}, 12: {Model: "long-model", LongContextBillingEnabled: false}}
			billing := newTestBillingService()
			channels := newTestChannelServiceWithCache(t, cache)
			deps := inflightEstimateDeps{billing: billing, resolver: NewModelPricingResolver(channels, billing), accountMappingSnapshot: func(context.Context, *APIKey, string) (map[int64]requestAccountMapping, error) { return results, nil }}
			_, request, err := deps.prepareRequestBilling(context.Background(), &APIKey{GroupID: requestPricingTestInt64(1), Group: group}, InflightEstimateRequest{Model: "long-model"})
			require.NoError(t, err)
			scope, err := request.ScopeFor(1, "", "")
			require.NoError(t, err)
			require.Equal(t, int64(9), scope.ChannelID)
			quote, err := request.PricingFor(scope)
			require.NoError(t, err)
			require.Equal(t, PricingSourceChannel, quote.Source())
			results[11] = requestAccountMapping{Model: "long-model", LongContextBillingEnabled: false}
			group.LongContextPricingEnabled = !groupGate
			for _, id := range []int64{11, 12, 11, 12} {
				callerFlag := id == 12 // deliberately opposite to each frozen account rule
				want := 200e-6
				if groupGate || id == 11 {
					want = 400e-6
				}
				cost, err := quote.Calculate(CostInput{SelectedAccountID: id, Tokens: UsageTokens{InputTokens: 200}, LongContextBillingEnabled: &callerFlag})
				require.NoError(t, err)
				require.InDelta(t, want, cost.TotalCost, 1e-12)
				require.Equal(t, groupGate, quote.resolved.longContextPricingEnabled, "alternating accounts cannot mutate the shared frozen group rule")
			}
			_, err = quote.Calculate(CostInput{SelectedAccountID: 99})
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			require.Equal(t, int64(0), scope.AccountID, "execution account does not change customer price scope")
		})
	}
}
