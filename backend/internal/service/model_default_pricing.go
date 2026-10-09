package service

import "time"

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
	result := ModelDefaultPricing{
		Found: true, SnapshotReady: true, Currency: p.Currency, PricingSource: p.PricingSource,
		InputPrice: p.InputPricePerToken, OutputPrice: p.OutputPricePerToken,
		CacheWritePrice: &p.CacheCreationPricePerToken, CacheReadPrice: &p.CacheReadPricePerToken,
		ImageInputPrice: p.ImageInputPricePerToken, ImageOutputPrice: p.ImageOutputPricePerToken,
		ReasoningEffortMultipliers: p.ReasoningEffortMultipliers,
	}
	if result.Currency == "" {
		result.Currency = "USD"
	}
	// Catalog entries for per-image/per-second billing can exist without any
	// token rates. Do not turn them into a free token card during auto pricing.
	if p.NativeCard == nil && p.InputPricePerToken == 0 && p.OutputPricePerToken == 0 &&
		p.ImageInputPricePerToken == 0 && p.ImageOutputPricePerToken == 0 {
		result.SnapshotReady = false
	}
	if p.SupportsCacheBreakdown {
		if p.CacheCreation5mPrice > 0 {
			result.CacheWritePrice = &p.CacheCreation5mPrice
		}
		result.CacheWrite1hPrice = &p.CacheCreation1hPrice
	}
	if p.NativeCard != nil {
		at := p.NativePricingAt
		if at.IsZero() {
			at = time.Now()
		}
		rates := p.NativeCard.ratesAt(0, at)
		if rates.CacheRead == nil {
			result.CacheReadPrice = nil
		}
		if rates.CacheWrite == nil {
			result.CacheWritePrice = nil
		}
		result.SnapshotReady = result.CacheReadPrice != nil && result.CacheWritePrice != nil
	}
	return result
}
