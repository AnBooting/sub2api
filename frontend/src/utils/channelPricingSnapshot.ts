import type { ModelDefaultPricing } from '@/api/admin/channels'
import type { PricingFormEntry } from '@/components/admin/channel/types'
import { createDefaultTimePricingForm, perTokenToMTok } from '@/components/admin/channel/types'
import { supplementIdentityMappings } from './channelModelMapping'

export function pricingModelNames(models: string[], mapping: Record<string, string>, source: string): string[] {
  if (source !== 'channel_mapped') return models
  const rules = Object.entries(mapping).sort(([a], [b]) => b.length - a.length)
  return models.map(model => {
    const key = model.toLowerCase()
    const exact = rules.find(([from]) => from.toLowerCase() === key)
    const wildcard = rules.find(([from]) => from.endsWith('*') && key.startsWith(from.slice(0, -1).toLowerCase()))
    return (exact || wildcard)?.[1].trim() || model
  })
}

export function emptyModelPricing(model: string): PricingFormEntry {
  return {
    models: [model], billing_mode: 'token',
    input_price: null, output_price: null, cache_write_price: null, cache_write_1h_price: null,
    cache_read_price: null, image_input_price: null, image_output_price: null, per_request_price: null,
    intervals: [], time_pricing: createDefaultTimePricingForm()
  }
}

export function hasManualPricing(entry: PricingFormEntry): boolean {
  return entry.billing_mode !== 'token' || [
    entry.input_price, entry.output_price, entry.cache_write_price, entry.cache_write_1h_price,
    entry.cache_read_price, entry.image_input_price, entry.image_output_price, entry.per_request_price,
    entry.fast_multiplier, entry.flex_multiplier
  ].some(value => value != null && value !== '') || entry.intervals.length > 0 ||
    entry.time_pricing.periods.length > 0 || Object.keys(entry.reasoning_effort_multipliers || {}).length > 0
}

export function uncoveredPricingModels(entries: PricingFormEntry[], models: string[]): string[] {
  const existing = Object.fromEntries(entries.flatMap(entry => entry.models.map(model => [model, model])))
  const result = supplementIdentityMappings(existing, models)
  return Object.keys(result.mapping).filter(model => !Object.prototype.hasOwnProperty.call(existing, model))
}

export function usablePriceSnapshot(price: ModelDefaultPricing | undefined): price is ModelDefaultPricing {
  if (!price?.found || price.snapshot_ready === false) return false
  if (price.input_price == null || price.output_price == null) return false
  if (price.currency === 'CNY' && (price.cache_write_price == null || price.cache_read_price == null)) return false
  return [price.input_price, price.output_price, price.cache_write_price, price.cache_write_1h_price,
    price.cache_read_price, price.image_input_price, price.image_output_price]
    .every(value => value == null || (Number.isFinite(value) && value >= 0))
}

// Fill only untouched rules. Split multi-model entries so different prices and
// currencies cannot accidentally be shared by all models in the entry.
export function fillPricingSnapshots(entries: PricingFormEntry[], snapshots: Map<string, ModelDefaultPricing>) {
  let filled = 0
  const missing: string[] = []
  const pricing = entries.flatMap(entry => {
    if (hasManualPricing(entry)) return [entry]
    const resolved: PricingFormEntry[] = []
    const remaining: string[] = []
    for (const model of entry.models) {
      if (model.includes('*')) { remaining.push(model); continue }
      const price = snapshots.get(model)
      if (!usablePriceSnapshot(price)) { remaining.push(model); missing.push(model); continue }
      filled++
      resolved.push({
        ...entry, models: [model], currency: price.currency || 'USD',
        input_price: perTokenToMTok(price.input_price ?? null),
        output_price: perTokenToMTok(price.output_price ?? null),
        cache_write_price: perTokenToMTok(price.cache_write_price ?? null),
        cache_write_1h_price: perTokenToMTok(price.cache_write_1h_price ?? null),
        cache_read_price: perTokenToMTok(price.cache_read_price ?? null),
        image_input_price: perTokenToMTok(price.image_input_price ?? null),
        image_output_price: perTokenToMTok(price.image_output_price ?? null),
        reasoning_effort_multipliers: price.reasoning_effort_multipliers ?? null
      })
    }
    if (remaining.length > 0 || entry.models.length === 0) resolved.push({ ...entry, models: remaining })
    return resolved
  })
  return { pricing, filled, missing }
}
