package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"sort"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// Request copies reuse existing resolvers/calculators with no repositories,
// remote clients, scheduler or refresh paths. Every mutable pointer is owned.
func cloneRequestNativeRates(r NativeRates) NativeRates {
	return NativeRates{Input: cloneGroupValuePointer(r.Input), Output: cloneGroupValuePointer(r.Output), CacheRead: cloneGroupValuePointer(r.CacheRead), CacheWrite: cloneGroupValuePointer(r.CacheWrite), Reasoning: cloneGroupValuePointer(r.Reasoning)}
}
func cloneRequestNativeTiers(source []NativeTier) []NativeTier {
	out := append([]NativeTier(nil), source...)
	for i := range out {
		out[i].NativeRates = cloneRequestNativeRates(source[i].NativeRates)
	}
	return out
}
func cloneRequestNativeCard(source *NativeCNYCard) *NativeCNYCard {
	if source == nil {
		return nil
	}
	out := *source
	out.NativeRates = cloneRequestNativeRates(source.NativeRates)
	out.Tiers = cloneRequestNativeTiers(source.Tiers)
	out.holidays = cloneDeepSeekHolidays(source.holidays)
	if source.Schedule != nil {
		schedule := *source.Schedule
		schedule.Windows = append([]NativeWindow(nil), source.Schedule.Windows...)
		for i := range schedule.Windows {
			schedule.Windows[i].NativeRates = cloneRequestNativeRates(source.Schedule.Windows[i].NativeRates)
			schedule.Windows[i].Tiers = cloneRequestNativeTiers(source.Schedule.Windows[i].Tiers)
			schedule.Windows[i].Weekdays = append([]int(nil), source.Schedule.Windows[i].Weekdays...)
			schedule.Windows[i].HourRanges = append([]string(nil), source.Schedule.Windows[i].HourRanges...)
		}
		out.Schedule = &schedule
	}
	return &out
}
func cloneRequestModelPricing(source *ModelPricing) *ModelPricing {
	if source == nil {
		return nil
	}
	out := *source
	out.NativeCard = cloneRequestNativeCard(source.NativeCard)
	out.FastMultiplier = cloneGroupValuePointer(source.FastMultiplier)
	out.FlexMultiplier = cloneGroupValuePointer(source.FlexMultiplier)
	out.ReasoningEffortMultipliers = maps.Clone(source.ReasoningEffortMultipliers)
	return &out
}
func cloneRequestPricingCatalog(source *PricingService) *PricingService {
	if source == nil {
		return nil
	}
	// Same lock order as remote publication: controls before catalog.
	source.controlsMu.Lock()
	defer source.controlsMu.Unlock()
	source.mu.RLock()
	defer source.mu.RUnlock()
	controls := source.controlsLocked()
	out := &PricingService{cfg: &config.Config{}, controls: &controls, pricingData: make(map[string]*LiteLLMModelPricing, len(source.pricingData)), nativeCNYCards: make(map[string]*NativeCNYCard, len(source.nativeCNYCards)), lastUpdated: source.lastUpdated, nativeCNYUpdated: source.nativeCNYUpdated, localHash: source.localHash, customFilesHash: source.customFilesHash}
	for model, price := range source.pricingData {
		if price != nil {
			copy := *price
			out.pricingData[model] = &copy
		}
	}
	for model, card := range source.nativeCNYCards {
		out.nativeCNYCards[model] = cloneRequestNativeCard(card)
	}
	return out
}
func cloneRequestBillingService(source *BillingService) *BillingService {
	if source == nil {
		return nil
	}
	out := &BillingService{cfg: &config.Config{}, pricingService: cloneRequestPricingCatalog(source.pricingService), fallbackPrices: make(map[string]*ModelPricing, len(source.fallbackPrices))}
	// Only public billing scalars are copied, never application credentials.
	if source.cfg != nil {
		out.cfg.Default.RateMultiplier = source.cfg.Default.RateMultiplier
	}
	for model, price := range source.fallbackPrices {
		out.fallbackPrices[model] = cloneRequestModelPricing(price)
	}
	return out
}
func cloneRequestCustomerChannel(source *Channel) *Channel {
	if source == nil {
		return nil
	}
	out := &Channel{ID: source.ID, Status: source.Status, BillingModelSource: source.BillingModelSource, RestrictModels: source.RestrictModels}
	out.ModelPricing = make([]ChannelModelPricing, len(source.ModelPricing))
	for i := range source.ModelPricing {
		out.ModelPricing[i] = cloneAPIKeyGroupPricing(source.ModelPricing[i])
	}
	out.ModelMapping = make(map[string]map[string]string, len(source.ModelMapping))
	for platform, mapping := range source.ModelMapping {
		out.ModelMapping[platform] = maps.Clone(mapping)
	}
	return out
}
func cloneRequestChannelCache(source *channelCache) *channelCache {
	out := newEmptyChannelCache()
	for key, price := range source.pricingByGroupModel {
		if price != nil {
			copy := cloneAPIKeyGroupPricing(*price)
			out.pricingByGroupModel[key] = &copy
		}
	}
	for key, prices := range source.wildcardByGroupPlatform {
		for _, price := range prices {
			if price == nil || price.pricing == nil {
				continue
			}
			copy := cloneAPIKeyGroupPricing(*price.pricing)
			out.wildcardByGroupPlatform[key] = append(out.wildcardByGroupPlatform[key], &wildcardPricingEntry{prefix: price.prefix, pricing: &copy})
		}
	}
	out.mappingByGroupModel = maps.Clone(source.mappingByGroupModel)
	for key, mappings := range source.wildcardMappingByGP {
		for _, mapping := range mappings {
			if mapping != nil {
				copy := *mapping
				out.wildcardMappingByGP[key] = append(out.wildcardMappingByGP[key], &copy)
			}
		}
	}
	// Account-stat supplier pricing and unrelated features never enter customer scopes.
	for id, channel := range source.channelByGroupID {
		out.channelByGroupID[id] = cloneRequestCustomerChannel(channel)
	}
	for id, channel := range source.byID {
		out.byID[id] = cloneRequestCustomerChannel(channel)
	}
	out.groupPlatform = maps.Clone(source.groupPlatform)
	out.loadedAt = source.loadedAt
	return out
}
func cloneRequestChannelService(ctx context.Context, source *ChannelService) (*ChannelService, error) {
	if source == nil {
		return nil, nil
	}
	cache, err := source.loadCache(ctx)
	if err != nil {
		return nil, fmt.Errorf("freeze request channel pricing: %w", err)
	}
	if cache == nil {
		return nil, fmt.Errorf("%w: channel pricing snapshot missing", ErrModelPricingUnavailable)
	}
	return &ChannelService{frozenCache: cloneRequestChannelCache(cache)}, nil
}

// Content version covers candidate rules and catalogs, not only update timestamps.
func requestCatalogVersion(billing *BillingService, channels *ChannelService, candidates map[int64]*requestBillingCandidate) (string, error) {
	data := map[string]any{"fallback": billing.fallbackPrices}
	if pricing := billing.pricingService; pricing != nil {
		data["usd"], data["native"], data["controls"] = pricing.pricingData, pricing.nativeCNYCards, pricing.controlsLocked()
	}
	if channels != nil {
		cache := channels.frozenCache
		prices, mappings := map[string]any{}, map[string]string{}
		for key, price := range cache.pricingByGroupModel {
			encoded, _ := json.Marshal([]any{key.groupID, key.platform, key.model})
			prices[string(encoded)] = price
		}
		for key, mapping := range cache.mappingByGroupModel {
			encoded, _ := json.Marshal([]any{key.groupID, key.platform, key.model})
			mappings[string(encoded)] = mapping
		}
		wildPrices, wildMappings := map[string]any{}, map[string]any{}
		for key, entries := range cache.wildcardByGroupPlatform {
			encoded, _ := json.Marshal([]any{key.groupID, key.platform})
			values := make([]any, 0, len(entries))
			for _, entry := range entries {
				values = append(values, []any{entry.prefix, entry.pricing})
			}
			wildPrices[string(encoded)] = values
		}
		for key, entries := range cache.wildcardMappingByGP {
			encoded, _ := json.Marshal([]any{key.groupID, key.platform})
			values := make([]any, 0, len(entries))
			for _, entry := range entries {
				values = append(values, []string{entry.prefix, entry.target})
			}
			wildMappings[string(encoded)] = values
		}
		data["prices"], data["mappings"], data["wild_prices"], data["wild_mappings"] = prices, mappings, wildPrices, wildMappings
		data["channels"], data["platforms"] = cache.channelByGroupID, cache.groupPlatform
	}
	ids := make([]int64, 0, len(candidates))
	for id := range candidates {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	entries := make([]any, 0, len(ids))
	for _, id := range ids {
		c := candidates[id]
		entries = append(entries, []any{id, c.group, c.multiplier, c.mapping, c.allowedUpstreamModels, c.accountUpstreamModels, c.accountLongContext})
	}
	data["candidates"] = entries
	body, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("request price version: %w", err)
	}
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
