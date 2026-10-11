package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestManualCNYMissingPricesRejectedAtBilling(t *testing.T) {
	bs := newNativeBillingTest(t)
	r := NewModelPricingResolver(nil, bs)
	zero, price := 0.0, 1e-6
	complete := ChannelModelPricing{Currency: "CNY", BillingMode: BillingModeToken,
		Models: []string{"glm-test"}, InputPrice: &price, OutputPrice: &price, CacheReadPrice: &zero, CacheWritePrice: &zero}
	for _, field := range []string{"input", "output", "cache_read", "cache_write"} {
		t.Run(field, func(t *testing.T) {
			card := complete.Clone()
			switch field {
			case "input":
				card.InputPrice = nil
			case "output":
				card.OutputPrice = nil
			case "cache_read":
				card.CacheReadPrice = nil
			case "cache_write":
				card.CacheWritePrice = nil
			}
			_, err := bs.GetModelPricingWithChannel("glm-test", &card)
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			group := &Group{ID: 1, ModelPricing: []ChannelModelPricing{card}}
			gid := int64(1)
			key := &APIKey{Group: group, GroupID: &gid}
			deps := inflightEstimateDeps{billing: bs, resolver: r}
			require.ErrorIs(t, deps.validateNativePricingRequest(context.Background(), key,
				InflightEstimateRequest{Model: "glm-test", Kind: InflightEstimateToken}), ErrModelPricingUnavailable)
			_, err = bs.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "glm-test", Group: group,
				Tokens: UsageTokens{InputTokens: 10, OutputTokens: 10}, Resolver: r, RateMultiplier: 1})
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			resolved := r.Resolve(context.Background(), PricingInput{Model: "glm-test", Group: group})
			_, err = bs.CalculateCostUnified(CostInput{Model: "glm-test", Resolved: resolved,
				Tokens: UsageTokens{InputTokens: 1}, RateMultiplier: 1})
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
		})
	}
	_, err := bs.GetModelPricingWithChannel("glm-test", &complete)
	require.NoError(t, err)
}

func TestManualCNYIncompleteForeignModelRejectedBeforeUpstream(t *testing.T) {
	bs := newNativeBillingTest(t)
	require.NoError(t, bs.pricingService.UpdateControls(context.Background(), PricingControls{}))
	price, gid := 1e-6, int64(1)
	group := &Group{ID: gid, ModelPricing: []ChannelModelPricing{{Models: []string{"gpt-5.4"}, Currency: "CNY", InputPrice: &price}}}
	deps := inflightEstimateDeps{billing: bs, resolver: NewModelPricingResolver(nil, bs)}
	err := deps.validateNativePricingRequest(context.Background(), &APIKey{Group: group, GroupID: &gid},
		InflightEstimateRequest{Model: "gpt-5.4", Kind: InflightEstimateToken})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestManualUSDUsesRequestTimeForInheritedDeepseekRates(t *testing.T) {
	bs := newTestBillingService()
	r := NewModelPricingResolver(nil, bs)
	price := 1e-6
	card := ChannelModelPricing{Models: []string{"deepseek-v4-pro"}, InputPrice: &price}
	group := &Group{ID: 1, ModelPricing: []ChannelModelPricing{card}}
	for _, before := range []bool{true, false} {
		at := deepseekProRoutesToFlashAt.Add(time.Minute)
		want := deepseekFlashOffPeakOutputPrice
		if before {
			at = deepseekProRoutesToFlashAt.Add(-time.Minute)
			want = deepseekProOffPeakOutputPrice
		}
		p, err := bs.getModelPricingWithChannelAt("deepseek-v4-pro", &card, at)
		require.NoError(t, err)
		require.Equal(t, want, p.OutputPricePerToken)
		cost, err := bs.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "deepseek-v4-pro", Group: group,
			Tokens: UsageTokens{OutputTokens: 100}, Resolver: r, RateMultiplier: 1, PricingAt: at})
		require.NoError(t, err)
		require.InDelta(t, want*100, cost.OutputCost, 1e-12)
	}
}

func TestPricingResolverUsesFrozenRequestContext(t *testing.T) {
	bs := newTestBillingService()
	r := NewModelPricingResolver(nil, bs)
	at := deepseekProRoutesToFlashAt.Add(-time.Hour)
	ctx := context.WithValue(context.Background(), gatewayTokenRequestPricingAtCtxKey{}, at)
	cost, err := bs.CalculateCostUnified(CostInput{Ctx: ctx, Model: "deepseek-v4-pro", Resolver: r,
		Tokens: UsageTokens{OutputTokens: 100}, RateMultiplier: 1})
	require.NoError(t, err)
	require.InDelta(t, deepseekProOffPeakOutputPrice*100*deepseekPeakMultiplierAt(at), cost.OutputCost, 1e-12)
}

func TestDeepseekHolidayCalendarAcrossCurrencies(t *testing.T) {
	bs := newNativeBillingTest(t)
	r := NewModelPricingResolver(nil, bs)
	// National Day weekday peak must remain off-peak, in Beijing's calendar.
	for _, at := range []time.Time{
		time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 10, 2, 0, 0, 0, time.UTC), // Saturday make-up workday
		time.Date(2026, 9, 20, 2, 0, 0, 0, time.UTC),  // Sunday make-up workday
	} {
		require.Equal(t, 1.0, deepseekPeakMultiplierAt(at))
		p, err := bs.pricingService.nativePricingAt("deepseek-flash", at)
		require.NoError(t, err)
		require.Equal(t, 1e-6, p.InputPricePerToken)
	}
	at := time.Date(2026, 10, 12, 2, 0, 0, 0, time.UTC)
	locked, err := bs.pricingService.nativePricingAt("deepseek-flash", at)
	require.NoError(t, err)
	require.Equal(t, 2e-6, locked.InputPricePerToken)
	require.NoError(t, bs.pricingService.UpdateDeepseekHolidays(context.Background(), []string{"2026-10-12"}))
	cost, err := bs.CalculateCostUnified(CostInput{Model: "deepseek-flash", Tokens: UsageTokens{InputTokens: 1_000_000}, Resolver: r, PricingAt: at, RateMultiplier: 1})
	require.NoError(t, err)
	require.Equal(t, 1.0, cost.TotalCost)
	// A previously captured native card keeps its calendar snapshot.
	p, err := resolveNativeTokenPricing(locked, UsageTokens{InputTokens: 1}, at)
	require.NoError(t, err)
	require.Equal(t, 2e-6, p.InputPricePerToken)
	require.NoError(t, bs.pricingService.UpdateControls(context.Background(), PricingControls{}))
	cost, err = bs.CalculateCostUnified(CostInput{Model: "deepseek-v4-flash", Tokens: UsageTokens{InputTokens: 1_000_000}, Resolver: r, PricingAt: at, RateMultiplier: 1})
	require.NoError(t, err)
	require.InDelta(t, 0.15, cost.TotalCost, 1e-12)
}

func TestDeepseekCalendarUsesBeijingDateAndPeakBoundaries(t *testing.T) {
	c, err := newDeepseekCalendar([]string{"2026-10-12"})
	require.NoError(t, err)
	require.True(t, deepseekOffPeakDay(time.Date(2026, 10, 11, 17, 0, 0, 0, time.UTC), c)) // Beijing Monday, not Sunday
	require.False(t, deepseekOffPeakDay(time.Date(2026, 10, 11, 17, 0, 0, 0, time.UTC), defaultDeepseekCalendar))
	base, peak := 1.0, 2.0
	card := &NativeCNYCard{Provider: "deepseek", NativeRates: NativeRates{Input: &base, Output: &base}, holidayCalendar: c,
		Schedule: &NativeSchedule{Timezone: "UTC", Windows: []NativeWindow{{Weekdays: []int{0}, HourRanges: []string{"17:00-24:00"}, NativeRates: NativeRates{Input: &peak}}}}}
	require.Equal(t, base, *card.ratesAt(0, time.Date(2026, 10, 11, 17, 0, 0, 0, time.UTC)).Input)
	for _, hour := range []int{9, 14} {
		require.Equal(t, 2.0, deepseekPeakMultiplierAt(time.Date(2026, 8, 24, hour, 0, 0, 0, time.FixedZone("test", 8*3600))))
	}
	for _, hour := range []int{8, 12, 18} {
		require.Equal(t, 1.0, deepseekPeakMultiplierAt(time.Date(2026, 8, 24, hour, 0, 0, 0, time.FixedZone("test", 8*3600))))
	}
	_, err = newDeepseekCalendar([]string{"2026-02-30"})
	require.Error(t, err)
}

func TestManualCNYDeepseekPriceIsOperatorDefined(t *testing.T) {
	bs := newNativeBillingTest(t)
	price, zero := 3e-6, 0.0
	group := &Group{ID: 1, ModelPricing: []ChannelModelPricing{{Currency: "CNY", Models: []string{"deepseek-flash"},
		InputPrice: &price, OutputPrice: &price, CacheReadPrice: &zero, CacheWritePrice: &zero}}}
	r := NewModelPricingResolver(nil, bs)
	for _, hour := range []int{2, 12} {
		cost, err := bs.CalculateCostUnified(CostInput{Model: "deepseek-flash", Group: group, Resolver: r,
			Tokens: UsageTokens{InputTokens: 1000}, RateMultiplier: 1, PricingAt: time.Date(2026, 10, 12, hour, 0, 0, 0, time.UTC)})
		require.NoError(t, err)
		require.InDelta(t, .003, cost.TotalCost, 1e-12)
	}
}

func TestDomesticModelFamiliesDoNotMatchGenericPrefixes(t *testing.T) {
	for _, model := range []string{"stepwise-foreign", "step-foreign", "sparkle-chat", "spark-foreign", "qwenish", "glmer"} {
		require.False(t, isNativeCNModel(model), model)
	}
	for _, model := range []string{"Step-3.5-Flash", "step3", "spark-max", "spark-x1", "vendor/Qwen3.5-Plus", "glm-5.2", "deepseek-v4-pro"} {
		require.True(t, isNativeCNModel(model), model)
	}
}
