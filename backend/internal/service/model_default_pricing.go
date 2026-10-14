package service

import (
	"maps"
	"time"
)

// ModelDefaultPricing is a one-time snapshot of current base token rates.
// Nullable cache fields distinguish unknown native CNY rates from explicit zero.
type ModelDefaultPricing struct {
	Found                      bool               `json:"found"`
	SnapshotReady              bool               `json:"snapshot_ready"`
	Currency                   string             `json:"currency"`
	PricingSource              string             `json:"pricing_source"`
	InputPrice                 float64            `json:"input_price"`
	OutputPrice                float64            `json:"output_price"`
	CacheWritePrice            *float64           `json:"cache_write_price"`
	CacheWrite1hPrice          *float64           `json:"cache_write_1h_price"`
	CacheReadPrice             *float64           `json:"cache_read_price"`
	ImageInputPrice            float64            `json:"image_input_price"`
	ImageOutputPrice           float64            `json:"image_output_price"`
	ReasoningEffortMultipliers map[string]float64 `json:"reasoning_effort_multipliers"`
}

func (p *ModelPricing) DefaultPricingSnapshot() ModelDefaultPricing {
	if p == nil {
		return ModelDefaultPricing{}
	}
	result := ModelDefaultPricing{
		Found: true, SnapshotReady: true, Currency: p.Currency, PricingSource: p.PricingSource,
		InputPrice: p.InputPricePerToken, OutputPrice: p.OutputPricePerToken,
		CacheWritePrice: &p.CacheCreationPricePerToken, CacheReadPrice: &p.CacheReadPricePerToken,
		ImageInputPrice: p.ImageInputPricePerToken, ImageOutputPrice: p.ImageOutputPricePerToken,
		ReasoningEffortMultipliers: maps.Clone(p.ReasoningEffortMultipliers),
	}
	if result.Currency == "" {
		result.Currency = "USD"
	}
	// Catalog entries for per-image/per-second billing can exist without any
	// token rates. Do not turn them into a free token card during auto pricing.
	if p.NativeCard == nil && !p.manualTokenPricing && p.InputPricePerToken == 0 && p.OutputPricePerToken == 0 &&
		p.ImageInputPricePerToken == 0 && p.ImageOutputPricePerToken == 0 {
		result.SnapshotReady = false
	}
	if p.SupportsCacheBreakdown {
		if p.CacheCreation5mPrice > 0 {
			result.CacheWritePrice = &p.CacheCreation5mPrice
		}
		result.CacheWrite1hPrice = &p.CacheCreation1hPrice
	}
	if p.NativeCard != nil && !p.manualTokenPricing {
		base := *p.NativeCard
		at := p.NativePricingAt
		if at.IsZero() {
			at = time.Now()
		}
		// Only DeepSeek manual cards automatically reapply the official peak
		// policy. Other native schedules retain their current-window snapshot.
		if base.deepSeek {
			base.Schedule, base.deepSeek = nil, false
		}
		rates := base.ratesAt(0, at)
		result.CacheReadPrice, result.CacheWritePrice = nativePerToken(rates.CacheRead), nativePerToken(rates.CacheWrite)
		if !validNativeRates(rates, true) {
			result.SnapshotReady = false
			return result
		}
		result.InputPrice, result.OutputPrice = *rates.Input/1e6, *rates.Output/1e6
		if rates.CacheRead == nil {
			result.CacheReadPrice = nil
		}
		if rates.CacheWrite == nil {
			result.CacheWritePrice = nil
		}
		result.SnapshotReady = result.CacheReadPrice != nil && result.CacheWritePrice != nil
	}
	if p.tokenPricingError != nil {
		result.SnapshotReady = false
	}
	return result
}
