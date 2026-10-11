package service

import (
	"fmt"
	"math"
)

// Source presence is independent of monetary value. Four explicit zeros form a
// complete USD/manual token card; nil is not implicitly free.
type TokenPricePresence struct {
	Known      bool `json:"known"`
	Input      bool `json:"input"`
	Output     bool `json:"output"`
	CacheRead  bool `json:"cache_read"`
	CacheWrite bool `json:"cache_write"`
}

func completeTokenPricePresence() TokenPricePresence {
	return TokenPricePresence{Known: true, Input: true, Output: true, CacheRead: true, CacheWrite: true}
}
func tokenPriceCompletenessError(pricing *ModelPricing) error {
	if pricing == nil {
		return ErrModelPricingUnavailable
	}
	if pricing.tokenPricingError != nil {
		return pricing.tokenPricingError
	}
	// Original native cards retain pointer dimensions/capabilities; unsupported
	// native cache-write is not invented. Actual cache usage is checked there.
	if pricing.NativeCard != nil && !pricing.manualTokenPricing {
		return nil
	}
	p := pricing.TokenPricePresence
	if !p.Known || !p.Input || !p.Output || !p.CacheRead || !p.CacheWrite {
		return fmt.Errorf("%w: token price source requires input, output, cache read and cache write (explicit zero allowed)", ErrModelPricingUnavailable)
	}
	for _, value := range []float64{pricing.InputPricePerToken, pricing.OutputPricePerToken, pricing.CacheReadPricePerToken, pricing.CacheCreationPricePerToken} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return fmt.Errorf("%w: invalid token price", ErrModelPricingUnavailable)
		}
	}
	return nil
}
func applyTokenPricePresence(pricing *ModelPricing, manual *ChannelModelPricing) {
	if pricing == nil || manual == nil {
		return
	}
	pricing.TokenPricePresence.Known = true
	if manual.InputPrice != nil {
		pricing.TokenPricePresence.Input = true
	}
	if manual.OutputPrice != nil {
		pricing.TokenPricePresence.Output = true
	}
	if manual.CacheReadPrice != nil {
		pricing.TokenPricePresence.CacheRead = true
	}
	if manual.CacheWritePrice != nil {
		pricing.TokenPricePresence.CacheWrite = true
	}
}
