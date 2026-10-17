package service

import (
	"context"
	"fmt"
)

func (r *RequestBillingContext) validateRequestPricing(req InflightEstimateRequest) error {
	if r == nil {
		return ErrModelPricingUnavailable
	}
	if req.Model != r.requestedModel {
		return fmt.Errorf("%w: request model differs from the frozen request", ErrModelPricingUnavailable)
	}
	groupID := int64(0)
	if r.requestedGroupID != nil {
		groupID = *r.requestedGroupID
	}
	candidate, known := r.candidates[groupID]
	if !known {
		return fmt.Errorf("%w: requested group is not a frozen candidate", ErrModelPricingUnavailable)
	}
	expected := []string{candidate.mapping.MappedModel}
	if candidate.mapping.BillingModelSource == BillingModelSourceUpstream && len(candidate.allowedUpstreamModels) > 0 {
		expected = candidate.allowedUpstreamModels
	}
	for _, upstream := range expected {
		scope, err := r.ScopeFor(groupID, upstream, "")
		if err != nil {
			return err
		}
		quote, err := r.PricingFor(scope)
		if err != nil {
			return err
		}
		if err = quote.Availability(); err != nil {
			return err
		}
	}
	return nil
}
func (s *GatewayService) ValidateRequestPricing(ctx context.Context, key *APIKey, req InflightEstimateRequest) error {
	return GatewayRequestBillingFromContext(ctx).validateRequestPricing(req)
}
func (s *OpenAIGatewayService) ValidateRequestPricing(ctx context.Context, key *APIKey, req InflightEstimateRequest) error {
	return GatewayRequestBillingFromContext(ctx).validateRequestPricing(req)
}
