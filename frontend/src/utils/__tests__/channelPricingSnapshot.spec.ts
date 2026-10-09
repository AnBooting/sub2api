import { describe, expect, it } from 'vitest'
import { emptyModelPricing, fillPricingSnapshots, pricingModelNames, uncoveredPricingModels } from '../channelPricingSnapshot'

describe('channel price snapshots', () => {
  it('splits different currencies and preserves explicit zero prices', () => {
    const entry = { ...emptyModelPricing('gpt'), models: ['gpt', 'deepseek', 'missing'] }
    const result = fillPricingSnapshots([entry], new Map([
      ['gpt', { found: true, currency: 'USD' as const, input_price: 3e-6, output_price: 15e-6 }],
      ['deepseek', { found: true, currency: 'CNY' as const, input_price: 2e-6, output_price: 4e-6, cache_read_price: 0, cache_write_price: 0 }]
    ]))
    expect(result.filled).toBe(2)
    expect(result.missing).toEqual(['missing'])
    expect(result.pricing[0]).toMatchObject({ models: ['gpt'], currency: 'USD', input_price: 3, output_price: 15 })
    expect(result.pricing[1]).toMatchObject({ models: ['deepseek'], currency: 'CNY', cache_read_price: 0, cache_write_price: 0 })
    expect(result.pricing[2]).toMatchObject({ models: ['missing'], input_price: null })
  })

  it('preserves partial manual prices, zero prices, multipliers and non-token rules', () => {
    const entries = [
      { ...emptyModelPricing('a'), input_price: 0 },
      { ...emptyModelPricing('b'), output_price: 8 },
      { ...emptyModelPricing('c'), fast_multiplier: 2 },
      { ...emptyModelPricing('d'), billing_mode: 'image' as const, per_request_price: 1 }
    ]
    const result = fillPricingSnapshots(entries, new Map())
    expect(result.pricing).toEqual(entries)
    expect(result.filled).toBe(0)
    expect(result.missing).toEqual([])
  })

  it('never turns incomplete, invalid or missing prices into zero', () => {
    const entries = ['missing', 'cny', 'nan'].map(emptyModelPricing)
    const result = fillPricingSnapshots(entries, new Map([
      ['missing', { found: false }],
      ['cny', { found: true, currency: 'CNY' as const, input_price: 2e-6, output_price: 4e-6 }],
      ['nan', { found: true, input_price: NaN, output_price: 1e-6 }]
    ]))
    expect(result.filled).toBe(0)
    expect(result.pricing).toEqual(entries)
    expect(result.missing).toEqual(['missing', 'cny', 'nan'])
  })

  it('deduplicates across groups and respects existing wildcard pricing', () => {
    expect(uncoveredPricingModels([emptyModelPricing('gpt-*')], ['gpt-a', 'new', 'NEW', 'deepseek', '*']))
      .toEqual(['NEW', 'deepseek'])
  })

  it('uses channel mapping targets only for channel-mapped billing', () => {
    const mapping = { alias: 'gpt-real', 'test-*': 'deepseek-real', 'test-long*': 'qwen-real' }
    expect(pricingModelNames(['alias', 'test-a', 'test-longer', 'other'], mapping, 'channel_mapped'))
      .toEqual(['gpt-real', 'deepseek-real', 'qwen-real', 'other'])
    expect(pricingModelNames(['alias'], mapping, 'requested')).toEqual(['alias'])
  })
})
