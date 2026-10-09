package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNativeDefaultPricingSnapshotPreservesUnknownCacheRates(t *testing.T) {
	zero, input, output := 0.0, 2.0, 4.0
	p := &ModelPricing{Currency: "CNY", InputPricePerToken: input / 1e6, OutputPricePerToken: output / 1e6,
		NativeCard: &NativeCNYCard{NativeRates: NativeRates{Input: &input, Output: &output, CacheRead: &zero}}}
	snapshot := p.DefaultPricingSnapshot()
	require.True(t, snapshot.Found)
	require.False(t, snapshot.SnapshotReady)
	require.NotNil(t, snapshot.CacheReadPrice)
	require.Zero(t, *snapshot.CacheReadPrice)
	require.Nil(t, snapshot.CacheWritePrice)
	p.NativeCard.CacheWrite = &zero
	require.True(t, p.DefaultPricingSnapshot().SnapshotReady)
}

func TestNativeDefaultPricingSnapshotUsesCurrentWindow(t *testing.T) {
	input, output, cache := 2.0, 4.0, 0.0
	card := &NativeCNYCard{NativeRates: NativeRates{Input: &input, Output: &output}, Schedule: &NativeSchedule{Timezone: "UTC", Windows: []NativeWindow{
		{NativeRates: NativeRates{CacheRead: &cache, CacheWrite: &cache}, Weekdays: []int{1}, HourRanges: []string{"00:00-24:00"}},
	}}}
	p := &ModelPricing{Currency: "CNY", NativeCard: card, NativePricingAt: time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)}
	require.True(t, p.DefaultPricingSnapshot().SnapshotReady)
	p.NativePricingAt = p.NativePricingAt.Add(24 * time.Hour)
	require.False(t, p.DefaultPricingSnapshot().SnapshotReady)
}

func TestModelOnlyChannelRulePreservesNativeCNYAndMissingPrice(t *testing.T) {
	bs := newNativeBillingTest(t)
	r := NewModelPricingResolver(nil, bs)
	rule := &ChannelModelPricing{Currency: "USD", BillingMode: BillingModeToken, Models: []string{"glm-test"}}
	base, err := bs.GetModelPricing("glm-test")
	require.NoError(t, err)
	resolved := &ResolvedPricing{BasePricing: base}
	r.applyTokenOverrides(rule, resolved, "glm-test")
	require.Same(t, base, resolved.BasePricing)
	require.Equal(t, "CNY", resolved.BasePricing.Currency)
	p, err := bs.GetModelPricingWithChannel("glm-test", rule)
	require.NoError(t, err)
	require.Equal(t, "CNY", p.Currency)
	_, err = bs.GetModelPricingWithChannel("qwen-usd-estimate", rule)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	unavailable := &ResolvedPricing{}
	r.applyTokenOverrides(rule, unavailable, "qwen-usd-estimate")
	require.Nil(t, unavailable.BasePricing)
}

func TestDefaultPricingSnapshotWithoutTokenRatesIsNotReady(t *testing.T) {
	p := &ModelPricing{Currency: "USD"}
	require.False(t, p.DefaultPricingSnapshot().SnapshotReady)
	p.ImageOutputPricePerToken = 3e-5
	require.True(t, p.DefaultPricingSnapshot().SnapshotReady)
}
