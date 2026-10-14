package service

import (
	"fmt"
	"math"
)

// Keep nil distinct from an explicit zero at the runtime boundary too: legacy
// database rows and cache/import paths may bypass save-time validation.
func manualTokenPricingError(config *ChannelModelPricing) error {
	if config == nil {
		return nil
	}
	if config.Currency == "CNY" && (config.InputPrice == nil || config.OutputPrice == nil || config.CacheReadPrice == nil || config.CacheWritePrice == nil) {
		return fmt.Errorf("%w: manual CNY token pricing requires input, output, cache read and cache write; explicit zero is allowed", ErrModelPricingUnavailable)
	}
	for _, value := range []*float64{config.InputPrice, config.OutputPrice, config.CacheReadPrice, config.CacheWritePrice, config.CacheWrite1hPrice, config.ImageInputPrice, config.ImageOutputPrice} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0) {
			return fmt.Errorf("%w: invalid manual token price", ErrModelPricingUnavailable)
		}
	}
	return nil
}
