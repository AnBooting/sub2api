package service

import (
	"fmt"
	"math"
)

// calculateLockedSharedQuoteCost is shared by actual usage, response-model
// baseline comparison and image preflight. It never consults live group prices.
func calculateLockedSharedQuoteCost(quote *LockedRequestPricing, result *ForwardResult, input CostInput) (*CostBreakdown, error) {
	if quote == nil || result == nil {
		return nil, fmt.Errorf("%w: missing locked image input", ErrModelPricingUnavailable)
	}
	if err := lockedSharedGatewayUsageMediaError(result); err != nil {
		return nil, err
	}
	if err := quote.Availability(); err != nil {
		return nil, err
	}
	switch quote.Mode() {
	case BillingModeToken:
		return quote.Calculate(input)
	case BillingModeImage, BillingModePerRequest:
	default:
		return nil, fmt.Errorf("%w: unsupported shared billing mode %q", ErrModelPricingUnavailable, quote.Mode())
	}
	// A zero-image result retains the existing per-request card semantics (one
	// request), not an inferred missing image count or a free image response.
	if result.ImageCount == 0 {
		return quote.Calculate(input)
	}
	rate := quote.BaseMultiplier()
	if group := quote.candidate.group; group != nil && group.ImageRateIndependent {
		rate = group.ImageRateMultiplier
	}
	if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 {
		return nil, fmt.Errorf("%w: invalid frozen image multiplier", ErrModelPricingUnavailable)
	}
	input.RequestCount = result.ImageCount
	input.SizeTier = NormalizeImageBillingTierOrDefault(result.ImageSize)
	return quote.CalculateWithRate(input, rate)
}

// ValidateSharedImagePricing validates one concrete target size against an
// existing frozen scope. Callers choose the scope from the same bundle/account
// binding used for forwarding; this method neither freezes again nor predicts
// which sizes an endpoint may produce. Token cards remain token-only charges.
func (r *RequestBillingContext) ValidateSharedImagePricing(scope PriceScope, size string) error {
	quote, err := r.PricingFor(scope)
	if err != nil {
		return err
	}
	_, err = calculateLockedSharedQuoteCost(quote, &ForwardResult{ImageCount: 1, ImageSize: size}, CostInput{})
	return err
}
