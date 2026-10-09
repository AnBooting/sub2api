package service

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

const nativeCatalogFixture = `{
 "deepseek":{"models":{"deepseek-flash":{"cost":{"input":0.15,"output":0.6,"currency_options":{"CNY":{"input":1,"output":4,"cache_read":0.02,"schedule":{"timezone":"UTC","windows":[{"weekdays":[1,2,3,4,5],"hours":["01:00-04:00"],"input":2,"output":8,"cache_read":0.04}]}}}}}}},
 "zhipuai":{"models":{"glm-test":{"cost":{"currency":"CNY","input":2,"output":3,"cache_read":0.2,"cache_write":0,"tiers":[{"tier":{"type":"context","size":100},"input":4,"output":6}]}}}},
 "alibaba-cn":{"models":{"qwen-usd-estimate":{"cost":{"input":0.1,"output":0.2}}}}
}`

func newNativeBillingTest(t *testing.T) *BillingService {
	t.Helper()
	cfg := &config.Config{}
	cfg.Pricing.DataDir = t.TempDir()
	svc := NewPricingService(cfg, &controlsPricingClient{})
	cards, err := parseNativeCNYCatalog([]byte(nativeCatalogFixture), "fixture", time.Now())
	require.NoError(t, err)
	svc.nativeCNYCards = cards
	require.NoError(t, svc.UpdateControls(context.Background(), PricingControls{NativeCNYEnabled: true}))
	bs := NewBillingService(cfg, svc)
	return bs
}

func TestNativeCNYCurrencySelectionAndMissingPrice(t *testing.T) {
	bs := newNativeBillingTest(t)
	_, err := bs.GetModelPricing("qwen-usd-estimate")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	p, err := bs.GetModelPricing("vendor/GLM-test")
	require.NoError(t, err)
	require.Equal(t, "CNY", p.Currency)
	_, err = bs.GetModelPricing("gpt-5.4")
	require.NoError(t, err)
	require.NoError(t, bs.pricingService.UpdateControls(context.Background(), PricingControls{}))
	_, err = bs.GetModelPricing("deepseek-flash")
	require.NoError(t, err)
}

func TestNativeCNYTierAndNoFXCreditBilling(t *testing.T) {
	bs := newNativeBillingTest(t)
	resolver := NewModelPricingResolver(nil, bs)
	for _, n := range []int{100, 101} {
		cost, err := bs.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "glm-test", Tokens: UsageTokens{InputTokens: n, OutputTokens: 10}, RateMultiplier: 2, Resolver: resolver})
		require.NoError(t, err)
		want := (float64(n)*2 + 10*3) / 1e6
		if n > 100 {
			want = (float64(n)*4 + 10*6) / 1e6
		}
		require.InDelta(t, want, cost.TotalCost, 1e-12)
		require.InDelta(t, want*2, cost.ActualCost, 1e-12)
		require.Equal(t, "CNY", cost.PricingCurrency)
	}
	_, err := bs.CalculateCost("deepseek-flash", UsageTokens{CacheCreationTokens: 1}, 1)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestNativeCNYPeakBoundaryWithoutUSDOverride(t *testing.T) {
	bs := newNativeBillingTest(t)
	resolver := NewModelPricingResolver(nil, bs)
	for _, hour := range []int{1, 4} {
		cost, err := bs.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "deepseek-flash", Tokens: UsageTokens{InputTokens: 1_000_000}, RateMultiplier: 1, Resolver: resolver, PricingAt: time.Date(2026, 10, 12, hour, 0, 0, 0, time.UTC)})
		require.NoError(t, err)
		want := 1.0
		if hour == 1 {
			want = 2
		}
		require.Equal(t, want, cost.ActualCost)
	}
}

func TestNativeCNYSupplementOnlyOriginalCurrency(t *testing.T) {
	cards, err := parseNativeCNYSupplement([]byte(`{"models":[{"id":"qwen3-5","billing_currency":"CNY","input_price_orig_per_m":2,"output_price_orig_per_m":4,"input_price_cny_per_m":999},{"id":"glm-usd","billing_currency":"USD","input_price_orig_per_m":1,"output_price_orig_per_m":2,"input_price_cny_per_m":7},{"id":"kimi-retired","billing_currency":"CNY","retired":true,"input_price_orig_per_m":2,"output_price_orig_per_m":4}]}`), "fixture", time.Now())
	require.NoError(t, err)
	require.Len(t, cards, 1)
	require.Equal(t, 2.0, *cards["qwen35"].Input)
}

func TestNativeCNYManualOverrideCurrencyAndAudit(t *testing.T) {
	bs := newNativeBillingTest(t)
	resolver := NewModelPricingResolver(nil, bs)
	price, zero := 5e-6, 0.0
	for _, currency := range []string{"USD", "CNY"} {
		group := &Group{ID: 1, ModelPricing: []ChannelModelPricing{{Models: []string{"glm-test"}, Currency: currency, InputPrice: &price, OutputPrice: &price, CacheWritePrice: &zero, CacheReadPrice: &zero}}}
		cost, err := bs.CalculateTokenCostForRequest(TokenCostRequest{Ctx: context.Background(), Model: "glm-test", Group: group, Tokens: UsageTokens{InputTokens: 1_000_000, CacheReadTokens: 1_000_000}, RateMultiplier: 1, Resolver: resolver})
		require.NoError(t, err)
		require.Equal(t, 5.0, cost.ActualCost)
		require.Equal(t, currency, cost.PricingCurrency)
		var snapshot map[string]any
		require.NoError(t, json.Unmarshal(costPricingSnapshot(cost), &snapshot))
		require.Equal(t, currency, snapshot["currency"])
		require.Equal(t, "manual", snapshot["source"])
		require.Equal(t, "credits", snapshot["quota_unit"])
	}
}

func TestNativeCNYScheduleShowsEveryTierAndCacheUnknown(t *testing.T) {
	bs := newNativeBillingTest(t)
	resolver := NewModelPricingResolver(nil, bs)
	sched, err := bs.ResolveContextPricingSchedule(context.Background(), resolver, ContextPricingScheduleInput{Model: "glm-test"})
	require.NoError(t, err)
	require.Len(t, sched.Tiers, 2)
	require.Equal(t, 100, *sched.Tiers[0].MaxTokens)
	require.Equal(t, 4e-6, *sched.Tiers[1].Input)
	sched, err = bs.ResolveContextPricingSchedule(context.Background(), resolver, ContextPricingScheduleInput{Model: "deepseek-flash"})
	require.NoError(t, err)
	require.Nil(t, sched.Tiers[0].CacheWrite)
}

func TestNativeCNYPreflightRejectsUnpricedWithoutReservations(t *testing.T) {
	bs := newNativeBillingTest(t)
	deps := inflightEstimateDeps{billing: bs, resolver: NewModelPricingResolver(nil, bs)}
	require.ErrorIs(t, deps.validateNativePricingRequest(context.Background(), nil, InflightEstimateRequest{Model: "qwen-usd-estimate", Kind: InflightEstimateToken}), ErrModelPricingUnavailable)
	require.NoError(t, deps.validateNativePricingRequest(context.Background(), nil, InflightEstimateRequest{Model: "glm-test", Kind: InflightEstimateToken}))
	price, zero := 1e-6, 0.0
	key := &APIKey{Group: &Group{ModelPricing: []ChannelModelPricing{{Models: []string{"qwen-usd-estimate"}, Currency: "CNY", InputPrice: &price, OutputPrice: &price, CacheReadPrice: &zero, CacheWritePrice: &zero}}}}
	require.NoError(t, deps.validateNativePricingRequest(context.Background(), key, InflightEstimateRequest{Model: "qwen-usd-estimate", Kind: InflightEstimateToken}))
}

type delayedNativeCNYClient struct {
	started chan context.Context
	release chan struct{}
}

func (c *delayedNativeCNYClient) FetchPricingJSON(ctx context.Context, _ string) ([]byte, error) {
	c.started <- ctx
	<-c.release
	return []byte(nativeCatalogFixture), nil
}
func (*delayedNativeCNYClient) FetchHashText(context.Context, string) (string, error) {
	panic("CNY must not fetch USD hashes")
}

func TestNativeCNYDisableRejectsLateResultAndKeepsOfflineCache(t *testing.T) {
	bs := newNativeBillingTest(t)
	svc := bs.pricingService
	cached, err := json.Marshal(svc.nativeCNYCards)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(svc.nativeCNYPath(), cached, 0600))
	client := &delayedNativeCNYClient{started: make(chan context.Context, 2), release: make(chan struct{})}
	svc.remoteClient = client
	require.NoError(t, svc.UpdateControls(context.Background(), PricingControls{RemoteEnabled: true, NativeCNYEnabled: true}))
	done := make(chan error, 1)
	go func() { done <- svc.syncNativeCNY(true) }()
	ctx := <-client.started
	require.NoError(t, svc.UpdateControls(context.Background(), PricingControls{RemoteEnabled: false, NativeCNYEnabled: true}))
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	close(client.release)
	require.ErrorIs(t, <-done, ErrPricingPullDisabled)
	after, err := os.ReadFile(svc.nativeCNYPath())
	require.NoError(t, err)
	require.Equal(t, cached, after)
	next := NewPricingService(svc.cfg, client)
	require.NoError(t, next.UpdateControls(context.Background(), PricingControls{NativeCNYEnabled: true}))
	next.loadNativeCNYCache()
	_, err = next.nativePricingAt("glm-test", time.Now())
	require.NoError(t, err)
	require.False(t, next.nativeCNYUpdated.IsZero())
	require.ErrorIs(t, next.ForceUpdate(), ErrPricingPullDisabled)
}
