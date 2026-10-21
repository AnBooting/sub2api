//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func pricingSafetyResolver(t *testing.T, billing *BillingService, model, source string, card ChannelModelPricing) (*ModelPricingResolver, *APIKey) {
	t.Helper()
	groupID := int64(17)
	group := &Group{ID: groupID, Platform: PlatformDeepseek, LongContextPricingEnabled: true}
	key := &APIKey{GroupID: &groupID, Group: group}
	if source == "group" {
		group.ModelPricing = []ChannelModelPricing{card}
		return NewModelPricingResolver(nil, billing), key
	}
	cache := newEmptyChannelCache()
	cache.pricingByGroupModel[channelModelKey{groupID: groupID, platform: PlatformDeepseek, model: model}] = &card
	cache.channelByGroupID[groupID] = &Channel{ID: 9, Status: StatusActive}
	cache.groupPlatform[groupID] = PlatformDeepseek
	return NewModelPricingResolver(newTestChannelServiceWithCache(t, cache), billing), key
}

func TestPricingSafetyManualCNYNilNeverBecomesZero(t *testing.T) {
	for _, source := range []string{"group", "channel"} {
		for missing := 0; missing < 5; missing++ {
			t.Run(fmt.Sprintf("%s/missing_%d", source, missing), func(t *testing.T) {
				billing := newNativeBillingTest(t)
				zero := 0.0
				card := ChannelModelPricing{Models: []string{"glm-test"}, Currency: "CNY", InputPrice: &zero, OutputPrice: &zero, CacheReadPrice: &zero, CacheWritePrice: &zero}
				switch missing {
				case 0:
					card.InputPrice = nil
				case 1:
					card.OutputPrice = nil
				case 2:
					card.CacheReadPrice = nil
				case 3:
					card.CacheWritePrice = nil
				case 4:
					card.InputPrice, card.OutputPrice, card.CacheReadPrice, card.CacheWritePrice = nil, nil, nil, nil
				}
				// A matching interval must not rescue an invalid legacy base card.
				card.Intervals = []PricingInterval{{MinTokens: 0, InputPrice: &zero, OutputPrice: &zero, CacheReadPrice: &zero, CacheWritePrice: &zero}}
				resolver, key := pricingSafetyResolver(t, billing, "glm-test", source, card)
				for _, enabled := range []bool{true, false} {
					require.NoError(t, billing.pricingService.UpdateControls(context.Background(), PricingControls{NativeCNYEnabled: enabled}))
					deps := inflightEstimateDeps{billing: billing, resolver: resolver}
					require.ErrorIs(t, deps.validateNativePricingRequest(context.Background(), key, InflightEstimateRequest{Model: "glm-test", Kind: InflightEstimateToken}), ErrModelPricingUnavailable)
				}
				_, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "glm-test", GroupID: key.GroupID, Group: key.Group, Resolver: resolver, Tokens: UsageTokens{InputTokens: 1}, RateMultiplier: 1})
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				_, err = billing.GetModelPricingWithChannel("glm-test", &card)
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
			})
		}
	}
}

func TestPricingSafetyManualCNYExplicitZeroAllowed(t *testing.T) {
	for _, source := range []string{"group", "channel"} {
		t.Run(source, func(t *testing.T) {
			billing := newNativeBillingTest(t)
			zero := 0.0
			card := ChannelModelPricing{Models: []string{"glm-test"}, Currency: "CNY", InputPrice: &zero, OutputPrice: &zero, CacheReadPrice: &zero, CacheWritePrice: &zero}
			resolver, key := pricingSafetyResolver(t, billing, "glm-test", source, card)
			deps := inflightEstimateDeps{billing: billing, resolver: resolver}
			require.NoError(t, deps.validateNativePricingRequest(context.Background(), key, InflightEstimateRequest{Model: "glm-test", Kind: InflightEstimateToken}))
			cost, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "glm-test", GroupID: key.GroupID, Group: key.Group, Resolver: resolver, Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 1000, CacheReadTokens: 1000, CacheCreationTokens: 1000}, RateMultiplier: 2})
			require.NoError(t, err)
			require.Zero(t, cost.ActualCost)
			p, err := billing.GetModelPricingWithChannel("glm-test", &card)
			require.NoError(t, err)
			require.True(t, p.DefaultPricingSnapshot().SnapshotReady)
		})
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), -1} {
		zero := 0.0
		card := &ChannelModelPricing{Currency: "CNY", InputPrice: &value, OutputPrice: &zero, CacheReadPrice: &zero, CacheWritePrice: &zero}
		require.ErrorIs(t, manualTokenPricingError(card), ErrModelPricingUnavailable)
	}
}

func TestPricingSafetyDeepSeekSharedCalendarBoundaries(t *testing.T) {
	// This Monday is a synthetic statutory-holiday fixture, not production data.
	holiday := map[string][]string{"2026": {"2026-10-12"}}
	monday := time.Date(2026, 10, 12, 0, 0, 0, 0, deepSeekLocation)
	for _, boundary := range []struct {
		hour, minute      int
		before, at, after float64
	}{
		{9, 0, 1, 2, 2}, {12, 0, 2, 1, 1}, {14, 0, 1, 2, 2}, {18, 0, 2, 1, 1},
	} {
		point := monday.Add(time.Duration(boundary.hour)*time.Hour + time.Duration(boundary.minute)*time.Minute)
		for i, offset := range []time.Duration{-time.Second, 0, time.Second} {
			at := point.Add(offset)
			want := []float64{boundary.before, boundary.at, boundary.after}[i]
			for _, zone := range []*time.Location{time.UTC, time.FixedZone("other-server", -7*3600)} {
				require.Equal(t, want, chooseDeepSeekPricingPeriod(at.In(zone), map[string][]string{"2026": {}}).multiplier())
				require.Equal(t, 1.0, chooseDeepSeekPricingPeriod(at.In(zone), holiday).multiplier())
			}
		}
	}
	for _, day := range []time.Time{time.Date(2026, 10, 10, 10, 0, 0, 0, deepSeekLocation), time.Date(2026, 9, 20, 10, 0, 0, 0, deepSeekLocation)} {
		require.Equal(t, 1.0, chooseDeepSeekPricingPeriod(day, nil).multiplier(), "make-up workday stays offpeak")
	}
	require.True(t, chooseDeepSeekPricingPeriod(monday, nil).CalendarYearMissing)
	require.False(t, chooseDeepSeekPricingPeriod(monday, map[string][]string{"2026": {}}).CalendarYearMissing)
}

func TestPricingSafetyDeepSeekManualChannelAndNativePeriods(t *testing.T) {
	peak := time.Date(2026, 10, 12, 10, 0, 0, 0, deepSeekLocation)
	for _, source := range []string{"group", "channel"} {
		for _, currency := range []string{"USD", "CNY"} {
			t.Run(source+"/"+currency, func(t *testing.T) {
				billing := newNativeBillingTest(t)
				one, zero := 1e-6, 0.0
				card := ChannelModelPricing{Models: []string{"deepseek-flash"}, Currency: currency, InputPrice: &one, OutputPrice: &one, CacheReadPrice: &zero, CacheWritePrice: &zero}
				resolver, key := pricingSafetyResolver(t, billing, "deepseek-flash", source, card)
				for _, holiday := range []bool{false, true} {
					calendar := map[string][]string{"2026": {}}
					if holiday {
						calendar["2026"] = []string{"2026-10-12"}
					}
					require.NoError(t, billing.pricingService.UpdateControls(context.Background(), PricingControls{NativeCNYEnabled: true, DeepSeekHolidays: calendar}))
					cost, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "deepseek-flash", GroupID: key.GroupID, Group: key.Group, Resolver: resolver, Tokens: UsageTokens{InputTokens: 1_000_000}, RateMultiplier: 3, PricingAt: peak})
					require.NoError(t, err)
					// PRD-05 2.11: operator prices do not inherit official peak rules.
					want := 3.0
					require.Equal(t, want, cost.ActualCost)
					if currency == "CNY" {
						require.Nil(t, cost.PricingSnapshot.NativeCard, "manual prices do not inherit official schedules")
					}
				}
			})
		}
	}
	billing := newNativeBillingTest(t)
	resolver := NewModelPricingResolver(nil, billing)
	for _, holiday := range []bool{false, true} {
		calendar := map[string][]string{"2026": {}}
		if holiday {
			calendar["2026"] = []string{"2026-10-12"}
		}
		require.NoError(t, billing.pricingService.UpdateControls(context.Background(), PricingControls{NativeCNYEnabled: true, DeepSeekHolidays: calendar}))
		cost, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "vendor/deepseek-flash", Resolver: resolver, Tokens: UsageTokens{InputTokens: 1_000_000}, RateMultiplier: 1, PricingAt: peak})
		require.NoError(t, err)
		want := 2.0
		if holiday {
			want = 1
		}
		require.Equal(t, want, cost.ActualCost, "absolute native peak must not be multiplied twice")
		// The fixture omits the second peak window; the shared policy still covers it.
		cost, err = billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "deepseek-flash", Resolver: resolver, Tokens: UsageTokens{InputTokens: 1_000_000}, RateMultiplier: 1, PricingAt: peak.Add(5 * time.Hour)})
		require.NoError(t, err)
		require.Equal(t, want, cost.ActualCost)
	}
}

func TestPricingSafetyNativeDefaultSnapshotUsesCurrentWindow(t *testing.T) {
	billing := newNativeBillingTest(t)
	p, err := billing.pricingService.nativePricingAt("deepseek-flash", time.Date(2026, 10, 12, 10, 0, 0, 0, deepSeekLocation))
	require.NoError(t, err)
	require.Equal(t, 2e-6, p.InputPricePerToken)
	base := p.DefaultPricingSnapshot()
	require.Equal(t, 2e-6, base.InputPrice)
	require.Equal(t, 8e-6, base.OutputPrice)
	require.Equal(t, 0.04/1e6, *base.CacheReadPrice)
	require.Nil(t, base.CacheWritePrice, "unsupported native cache dimension remains missing")
}

func TestPricingSafetyHolidayControlsPersistAndClone(t *testing.T) {
	repo := &pricingSettingsRepo{values: map[string]string{}}
	service := NewPricingService(&config.Config{}, nil)
	service.settingsRepo = repo
	controls := PricingControls{DeepSeekHolidays: map[string][]string{"2026": {"2026-10-12"}, "2027": {}}}
	require.NoError(t, service.UpdateControls(context.Background(), controls))
	controls.DeepSeekHolidays["2026"][0] = "2026-10-13"
	read := service.GetControls()
	require.Equal(t, "2026-10-12", read.DeepSeekHolidays["2026"][0])
	read.DeepSeekHolidays["2026"][0] = "2026-10-14"
	require.Equal(t, "2026-10-12", service.GetControls().DeepSeekHolidays["2026"][0])
	next := NewPricingService(&config.Config{}, nil)
	next.settingsRepo = repo
	require.NoError(t, next.loadControls(context.Background()))
	require.Equal(t, service.GetControls(), next.GetControls())
	for _, days := range []map[string][]string{{"2026": {"2026-02-30"}}, {"2026": {"2027-01-01"}}, {"2026": {"2026-10-12", "2026-10-12"}}, {"bad": {}}, {"+123": {}}, {"-123": {}}} {
		require.ErrorIs(t, next.UpdateControls(context.Background(), PricingControls{DeepSeekHolidays: days}), ErrInvalidDeepSeekHolidays)
		require.Equal(t, service.GetControls(), next.GetControls())
	}
	repo.err = errors.New("save failed")
	require.Error(t, next.UpdateControls(context.Background(), PricingControls{}))
	require.Equal(t, service.GetControls(), next.GetControls())
}

func TestPricingSafetyNativeScheduleRejectsOverlapAndEmptyWindows(t *testing.T) {
	one, two := 1.0, 2.0
	card := &NativeCNYCard{NativeRates: NativeRates{Input: &one, Output: &one}}
	window := func(days []int, hours string) NativeWindow {
		return NativeWindow{NativeRates: NativeRates{Input: &two}, Weekdays: days, HourRanges: []string{hours}}
	}
	for _, windows := range [][]NativeWindow{
		{window([]int{1}, "09:00-12:00"), window([]int{1}, "11:00-14:00")},
		{window([]int{1}, "22:00-02:00"), window([]int{2}, "01:00-03:00")},
		{window([]int{1}, "00:00-00:00")}, {window([]int{1}, "24:00-01:00")},
		{window([]int{1}, "invalid")}, {window([]int{7}, "09:00-12:00")},
	} {
		card.Schedule = &NativeSchedule{Timezone: "UTC", Windows: windows}
		require.False(t, validNativeCard(card))
	}
	card.Schedule = &NativeSchedule{Timezone: "UTC", Windows: []NativeWindow{window([]int{1}, "22:00-02:00"), window([]int{2}, "02:00-03:00")}}
	require.True(t, validNativeCard(card), "adjacent windows with complete base have no gap or overlap")
	for _, at := range []time.Time{time.Date(2026, 10, 12, 23, 0, 0, 0, time.UTC), time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC)} {
		require.Equal(t, two, *card.ratesAt(0, at).Input)
	}
	require.Equal(t, one, *card.ratesAt(0, time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)).Input)
	card.Input = nil
	require.False(t, validNativeCard(card), "a missing default price cannot fill schedule gaps")
}

func TestPricingSafetyNativePeakPolicyWithoutWindowsAndWithTiers(t *testing.T) {
	billing := newNativeBillingTest(t)
	one, four, zero, two, eight, three, six := 1.0, 4.0, 0.0, 2.0, 8.0, 3.0, 6.0
	for _, absolute := range []bool{false, true} {
		card := &NativeCNYCard{NativeRates: NativeRates{Input: &one, Output: &four, CacheRead: &zero, CacheWrite: &zero}}
		tier := NativeTier{NativeRates: NativeRates{Input: &three, Output: &six}}
		tier.Tier.Type, tier.Tier.Size = "context", 100
		card.Tiers = []NativeTier{tier}
		if absolute {
			card.Schedule = &NativeSchedule{Timezone: "Asia/Shanghai", Windows: []NativeWindow{{NativeRates: NativeRates{Input: &two, Output: &eight}, Weekdays: []int{1, 2, 3, 4, 5}, HourRanges: []string{"09:00-12:00", "14:00-18:00"}}}}
		}
		require.True(t, validNativeCard(card))
		billing.pricingService.nativeCNYCards["deepseekpolicy"] = card
		for _, hour := range []int{8, 10, 15} {
			at := time.Date(2026, 10, 12, hour, 0, 0, 0, deepSeekLocation)
			for _, tokens := range []int{100, 101} {
				pricing, err := billing.pricingService.nativePricingAt("deepseek-policy", at)
				require.NoError(t, err)
				pricing, err = resolveNativeTokenPricing(pricing, UsageTokens{InputTokens: tokens}, at)
				require.NoError(t, err)
				want := one
				if tokens > 100 {
					want = three
				}
				want *= deepseekPeakMultiplierWithCalendar(at, defaultDeepseekCalendar)
				require.Equal(t, want/1e6, pricing.InputPricePerToken, "absolute=%t hour=%d tokens=%d", absolute, hour, tokens)
			}
		}
	}
	billing.pricingService.nativeCNYCards["deepseekpolicy"] = &NativeCNYCard{NativeRates: NativeRates{Input: &zero, Output: &zero, CacheRead: &zero, CacheWrite: &zero}}
	pricing, err := billing.pricingService.nativePricingAt("deepseek-policy", time.Date(2026, 10, 12, 10, 0, 0, 0, deepSeekLocation))
	require.NoError(t, err)
	require.Zero(t, pricing.InputPricePerToken, "the 2x policy preserves a complete explicit-zero native card")
}

func TestPricingSafetyDeepSeekCurrentAutofillThenPeakDoesNotDouble(t *testing.T) {
	billing := newNativeBillingTest(t)
	zero := 0.0
	billing.pricingService.nativeCNYCards["deepseekflash"].CacheWrite = &zero
	peak := time.Date(2026, 10, 12, 10, 0, 0, 0, deepSeekLocation)
	pricing, err := billing.pricingService.nativePricingAt("deepseek-flash", peak)
	require.NoError(t, err)
	require.Equal(t, 2e-6, pricing.InputPricePerToken)
	snapshot := pricing.DefaultPricingSnapshot()
	require.True(t, snapshot.SnapshotReady)
	require.Equal(t, 2e-6, snapshot.InputPrice)
	card := ChannelModelPricing{Models: []string{"deepseek-flash"}, Currency: snapshot.Currency, InputPrice: &snapshot.InputPrice, OutputPrice: &snapshot.OutputPrice, CacheReadPrice: snapshot.CacheReadPrice, CacheWritePrice: snapshot.CacheWritePrice}
	resolver, key := pricingSafetyResolver(t, billing, "deepseek-flash", "group", card)
	cost, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "deepseek-flash", GroupID: key.GroupID, Group: key.Group, Resolver: resolver, Tokens: UsageTokens{InputTokens: 1_000_000}, RateMultiplier: 1, PricingAt: peak})
	require.NoError(t, err)
	require.Equal(t, 2.0, cost.ActualCost, "current-window autofill keeps its saved price without another official multiplier")
}

func TestPricingSafetyNativeSnapshotMissingTokenRatesIsNotReady(t *testing.T) {
	zero := 0.0
	for _, rates := range []NativeRates{
		{Output: &zero, CacheRead: &zero, CacheWrite: &zero},
		{Input: &zero, CacheRead: &zero, CacheWrite: &zero},
	} {
		pricing := &ModelPricing{Currency: "CNY", NativeCard: &NativeCNYCard{NativeRates: rates}}
		require.NotPanics(t, func() { require.False(t, pricing.DefaultPricingSnapshot().SnapshotReady) })
	}
	var unavailable *ModelPricing
	require.False(t, unavailable.DefaultPricingSnapshot().Found)
}

func TestPricingSafetyCalendarCanonicalYearNullAndEmptyObject(t *testing.T) {
	pricing := NewPricingService(&config.Config{}, nil)
	require.NotNil(t, pricing.GetControls().DeepSeekHolidays)
	require.Equal(t, map[string][]string{}, pricing.GetControls().DeepSeekHolidays)
	require.Equal(t, map[string][]string{}, pricing.GetPricingControlStatus()["deepseek_holidays"])
	calendar := map[string][]string{"0001": {"0001-01-01"}}
	require.NoError(t, validateDeepSeekHolidays(calendar))
	period := chooseDeepSeekPricingPeriod(time.Date(1, 1, 1, 10, 0, 0, 0, deepSeekLocation), calendar)
	require.False(t, period.CalendarYearMissing)
	require.Equal(t, "offpeak", period.Label)
	valid := PricingControls{DeepSeekHolidays: calendar}
	require.NoError(t, pricing.UpdateControls(context.Background(), valid))
	require.ErrorIs(t, validateDeepSeekHolidays(map[string][]string{"2026": nil}), ErrInvalidDeepSeekHolidays)
	require.ErrorIs(t, pricing.UpdateControls(context.Background(), PricingControls{DeepSeekHolidays: map[string][]string{"2026": nil}}), ErrInvalidDeepSeekHolidays)
	require.Equal(t, valid, pricing.GetControls())
	// Preserve original JSON semantics: null is not an explicitly maintained [].
	repo := &pricingSettingsRepo{values: map[string]string{SettingKeyPricingControls: `{"deepseek_holidays":{"2026":null}}`}}
	pricing.settingsRepo = repo
	require.NoError(t, pricing.loadControls(context.Background()))
	require.NotEmpty(t, pricing.GetPricingControlStatus()["deepseek_calendar_error"])
	require.Equal(t, defaultDeepseekCalendar.dates, pricing.DeepseekHolidayDates())
	repo.values[SettingKeyPricingControls] = `{"deepseek_holidays":{"2026":[]}}`
	require.NoError(t, pricing.loadControls(context.Background()))
	require.NotNil(t, pricing.GetControls().DeepSeekHolidays["2026"])
}
