package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestPR2MergeCalendarAPIsSharePersistedRules(t *testing.T) {
	s := NewPricingService(&config.Config{}, nil)
	repo := &pricingSettingsRepo{values: map[string]string{}}
	s.settingsRepo = repo
	at := time.Date(2026, 10, 12, 10, 0, 0, 0, deepSeekLocation)
	require.NoError(t, s.UpdateControls(context.Background(), PricingControls{UpdateIntervalHours: 6, DeepSeekHolidays: map[string][]string{"2026": {"2026-10-12"}, "2027": {}}}))
	require.Equal(t, []string{"2026-10-12"}, s.DeepseekHolidayDates())
	require.Equal(t, 1.0, deepseekPeakMultiplierWithCalendar(at, s.deepseekCalendarAt(at)))
	require.True(t, s.deepseekCalendarAt(at.AddDate(1, 0, 0)).years[2027], "explicit empty year stays covered")
	require.NoError(t, s.UpdateDeepseekHolidays(context.Background(), nil))
	require.False(t, s.GetControls().RemoteEnabled, "calendar-only save preserves switches")
	require.Equal(t, 6, s.GetControls().UpdateIntervalHours)
	require.Contains(t, s.GetControls().DeepSeekHolidays["2026"], "2026-10-01")
	repo.values[SettingKeyDeepseekHolidays] = `{broken legacy calendar}`
	restarted := NewPricingService(&config.Config{}, nil)
	restarted.settingsRepo = repo
	require.NoError(t, restarted.loadControls(context.Background()))
	require.Empty(t, restarted.GetPricingControlStatus()["deepseek_calendar_error"], "canonical calendar supersedes legacy data")
	require.Equal(t, s.GetControls(), restarted.GetControls())
	require.Equal(t, s.DeepseekHolidayDates(), restarted.DeepseekHolidayDates())
}

func TestPR2MergeLockedCalendarChangesOnlyNewRequests(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(map[bool]string{false: "USD", true: "CNY"}[native], func(t *testing.T) {
			billing := newNativeBillingTest(t)
			pricing := billing.pricingService
			at := time.Date(2026, 10, 12, 10, 0, 0, 0, deepSeekLocation)
			require.NoError(t, pricing.UpdateControls(context.Background(), PricingControls{NativeCNYEnabled: native, DeepSeekHolidays: map[string][]string{"2026": {}}}))
			deps := inflightEstimateDeps{billing: billing}
			model := "deepseek-flash"
			if !native {
				model = "deepseek-v4-flash"
			}
			req := InflightEstimateRequest{Model: model, PricingAt: at}
			oldCtx, old, err := deps.prepareRequestBilling(context.Background(), &APIKey{}, req)
			require.NoError(t, err)
			require.Equal(t, at, old.StartedAt(), "explicit request instant must reach the full snapshot")
			scope, err := old.ScopeFor(0, "", "")
			require.NoError(t, err)
			quote, err := old.PricingFor(scope)
			require.NoError(t, err)
			before, err := quote.Calculate(CostInput{Tokens: UsageTokens{InputTokens: 1_000_000}})
			require.NoError(t, err)
			require.Equal(t, "peak", quote.Period().Label)
			require.NoError(t, pricing.UpdateControls(context.Background(), PricingControls{NativeCNYEnabled: native, DeepSeekHolidays: map[string][]string{"2026": {"2026-10-12"}}}))
			frozenCtx, _ := (&GatewayService{billingService: billing}).WithGatewayTokenRequestPricing(oldCtx)
			require.Equal(t, 2.0, deepseekPeakMultiplierWithCalendar(at, requestDeepseekCalendar(frozenCtx, pricing, at)), "post-admission helpers must use the full bundle calendar")
			after, err := quote.Calculate(CostInput{Tokens: UsageTokens{InputTokens: 1_000_000}})
			require.NoError(t, err)
			require.Equal(t, before.ActualCost, after.ActualCost)
			require.Equal(t, "peak", quote.Period().Label)
			_, next, err := deps.prepareRequestBilling(context.Background(), &APIKey{}, req)
			require.NoError(t, err)
			nextQuote, err := next.PricingFor(scope)
			require.NoError(t, err)
			nextCost, err := nextQuote.Calculate(CostInput{Tokens: UsageTokens{InputTokens: 1_000_000}})
			require.NoError(t, err)
			require.Equal(t, "offpeak", nextQuote.Period().Label)
			require.InDelta(t, before.ActualCost/2, nextCost.ActualCost, 1e-12)
			require.NotEqual(t, quote.Version(), nextQuote.Version())
		})
	}
}

func TestPR2MergeRefreezeDoesNotReusePriorTurnInstant(t *testing.T) {
	billing := newTestBillingService()
	deps := inflightEstimateDeps{billing: billing}
	past := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ctx, first, err := deps.prepareRequestBilling(context.Background(), &APIKey{}, InflightEstimateRequest{Model: "gpt-5.4", PricingAt: past})
	require.NoError(t, err)
	_, next, err := deps.prepareRequestBilling(withoutGatewayRequestBilling(ctx), &APIKey{}, InflightEstimateRequest{Model: "gpt-5.4"})
	require.NoError(t, err)
	require.True(t, next.StartedAt().After(first.StartedAt()))
}

func TestPR2MergeAnnualCalendarSupportsMoreThanLegacyDateLimit(t *testing.T) {
	s := NewPricingService(&config.Config{}, nil)
	calendar := map[string][]string{}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3661; i++ {
		day := start.AddDate(0, 0, i)
		calendar[day.Format("2006")] = append(calendar[day.Format("2006")], day.Format(time.DateOnly))
	}
	require.NoError(t, s.UpdateControls(context.Background(), PricingControls{DeepSeekHolidays: calendar}))
	require.Len(t, s.DeepseekHolidayDates(), 3661)
}

func TestPR2MergeUSDManualValidationSurvivesCNYGuard(t *testing.T) {
	billing := newTestBillingService()
	one, invalid := 1e-6, -1.0
	card := ChannelModelPricing{Currency: "USD", InputPrice: &one, OutputPrice: &one, CacheWrite1hPrice: &invalid}
	_, err := billing.GetModelPricingWithChannel("gpt-5.4", &card)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}
