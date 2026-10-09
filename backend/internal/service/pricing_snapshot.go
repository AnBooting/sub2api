package service

import "encoding/json"

// PricingSnapshot records original currency and actual rates, while the debit
// remains a number of credits. Historical rows keep a nil snapshot.
func costPricingSnapshot(cost *CostBreakdown) json.RawMessage {
	if cost == nil {
		return nil
	}
	currency := cost.PricingCurrency
	if currency == "" {
		currency = "USD"
	}
	source := cost.PricingSource
	if source == "" {
		source = "default"
	}
	values := map[string]any{"currency": currency, "source": source, "quota_unit": "credits", "total_cost": cost.TotalCost, "actual_cost": cost.ActualCost}
	if p := cost.PricingSnapshot; p != nil {
		values["input_per_token"] = p.InputPricePerToken
		values["output_per_token"] = p.OutputPricePerToken
		values["cache_read_per_token"] = p.CacheReadPricePerToken
		values["cache_write_per_token"] = p.CacheCreationPricePerToken
		values["cache_write_1h_per_token"] = p.CacheCreation1hPrice
		if p.NativeCard != nil {
			values["fetched_at"] = p.NativeCard.FetchedAt
			values["provider"] = p.NativeCard.Provider
		}
	}
	body, err := json.Marshal(values)
	if err != nil {
		return nil
	}
	return body
}
