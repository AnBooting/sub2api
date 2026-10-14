//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestBillingContextUSDMissingDimensionsNotZero(t *testing.T) {
	for _, missing := range []string{"input_cost_per_token", "output_cost_per_token", "cache_read_input_token_cost", "cache_creation_input_token_cost"} {
		t.Run(missing, func(t *testing.T) {
			entry := map[string]any{"input_cost_per_token": 0, "output_cost_per_token": 0, "cache_read_input_token_cost": 0, "cache_creation_input_token_cost": 0}
			delete(entry, missing)
			body, err := json.Marshal(map[string]any{"usd-priced": entry})
			require.NoError(t, err)
			billing := newTestBillingService()
			billing.pricingService = NewPricingService(billing.cfg, nil)
			billing.pricingService.pricingData, err = billing.pricingService.parsePricingData(body)
			require.NoError(t, err)
			_, request, err := (inflightEstimateDeps{billing: billing}).prepareRequestBilling(context.Background(), &APIKey{}, InflightEstimateRequest{Model: "usd-priced"})
			require.NoError(t, err)
			quote, err := request.PricingFor(PriceScope{BillingModel: "usd-priced"})
			require.NoError(t, err)
			require.ErrorIs(t, quote.Availability(), ErrModelPricingUnavailable)
			require.ErrorIs(t, request.validateRequestPricing(InflightEstimateRequest{Model: "usd-priced"}), ErrModelPricingUnavailable)
			_, err = quote.Calculate(CostInput{RateMultiplier: 0})
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
		})
	}
}

func TestRequestBillingContextUSDExplicitZerosCompleteAndRoundTripPresence(t *testing.T) {
	body := []byte(`{"usd-zero":{"input_cost_per_token":0,"output_cost_per_token":0,"cache_read_input_token_cost":0,"cache_creation_input_token_cost":0}}`)
	billing := newTestBillingService()
	billing.pricingService = NewPricingService(billing.cfg, nil)
	var err error
	billing.pricingService.pricingData, err = billing.pricingService.parsePricingData(body)
	require.NoError(t, err)
	_, request, err := (inflightEstimateDeps{billing: billing}).prepareRequestBilling(context.Background(), &APIKey{}, InflightEstimateRequest{Model: "usd-zero"})
	require.NoError(t, err)
	require.NoError(t, request.validateRequestPricing(InflightEstimateRequest{Model: "usd-zero"}))
	quote, err := request.PricingFor(PriceScope{BillingModel: "usd-zero"})
	require.NoError(t, err)
	require.NoError(t, quote.Availability())
	cost, err := quote.Calculate(CostInput{Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 1000, CacheReadTokens: 1000, CacheCreationTokens: 1000}})
	require.NoError(t, err)
	require.Zero(t, cost.ActualCost)
	raw := billing.pricingService.pricingData["usd-zero"]
	raw.TokenPricePresence.CacheWrite = false
	normalized, err := json.Marshal(map[string]*LiteLLMModelPricing{"usd-zero": raw})
	require.NoError(t, err)
	roundTrip, err := billing.pricingService.parsePricingData(normalized)
	require.NoError(t, err)
	require.False(t, roundTrip["usd-zero"].TokenPricePresence.CacheWrite, "flattened serialized zero must not erase source absence")
}
