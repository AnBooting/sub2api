import { describe, expect, it } from 'vitest'
import type { ChannelModelPricing, ModelDefaultPricing } from '@/api/admin/channels'
import {
  missingRulePriceFields,
  priceStatusOf,
  priceSourceKindOf,
  ruleHasAnyPrice,
  summarizeGroupPricing
} from '../priceStatus'

const rule = (overrides: Partial<ChannelModelPricing> = {}): ChannelModelPricing => ({
  platform: 'openai',
  models: ['gpt-5.5'],
  billing_mode: 'token',
  input_price: null,
  output_price: null,
  cache_write_price: null,
  cache_read_price: null,
  image_input_price: null,
  image_output_price: null,
  per_request_price: null,
  intervals: [],
  time_pricing: null,
  ...overrides
})

const card = (overrides: Partial<ModelDefaultPricing> = {}): ModelDefaultPricing => ({
  found: true,
  snapshot_ready: true,
  currency: 'USD',
  input_price: 0.000001,
  output_price: 0.000002,
  ...overrides
})

describe('priceStatusOf', () => {
  it('没有任何规则但模型有官方内置价时按官方内置价计费', () => {
    const status = priceStatusOf(undefined, card({ pricing_source: 'fallback' }))
    expect(status.availability).toBe('billable')
    expect(status.source).toBe('builtin')
    expect(status.builtinAvailable).toBe(true)
    expect(status.pricingSource).toBe('fallback')
  })

  it('没有任何规则且模型没有价格来源时会被拒绝', () => {
    const status = priceStatusOf(undefined, undefined)
    expect(status.availability).toBe('rejected')
    expect(status.source).toBe('none')
  })

  it('规则填全时按手工价计费', () => {
    const status = priceStatusOf(
      rule({ input_price: 1, output_price: 2 }),
      card({ pricing_source: 'fallback' })
    )
    expect(status.availability).toBe('billable')
    expect(status.source).toBe('manual')
    expect(status.missingFields).toEqual([])
  })

  it('USD 规则缺字段但模型有官方内置价时仍可计费（上游 fallbackPrices 场景）', () => {
    const status = priceStatusOf(
      rule({ input_price: 1, output_price: null }),
      card({ pricing_source: 'fallback' })
    )
    expect(status.availability).toBe('billable')
    expect(status.source).toBe('builtin')
    expect(status.missingFields).toEqual(['output_price'])
  })

  it('USD 规则缺字段且没有官方内置价时会被拒绝', () => {
    const status = priceStatusOf(rule({ input_price: 1, output_price: null }), undefined)
    expect(status.availability).toBe('rejected')
    expect(status.source).toBe('none')
    expect(status.missingFields).toEqual(['output_price'])
  })

  it('CNY 规则缺缓存价时即使有官方 CNY 价卡也会被拒绝', () => {
    const status = priceStatusOf(
      rule({ currency: 'CNY', input_price: 1, output_price: 2 }),
      card({
        currency: 'CNY',
        pricing_source: 'native_cny',
        input_price: 1,
        output_price: 2,
        cache_write_price: 1,
        cache_read_price: 1
      })
    )
    expect(status.availability).toBe('rejected')
    expect(status.source).toBe('none')
    expect(status.missingFields).toEqual(['cache_write_price', 'cache_read_price'])
  })

  it('CNY 规则四个必需价格都填全时按手工价计费', () => {
    const status = priceStatusOf(
      rule({
        currency: 'CNY',
        input_price: 1,
        output_price: 2,
        cache_write_price: 1,
        cache_read_price: 1
      }),
      undefined
    )
    expect(status.availability).toBe('billable')
    expect(status.source).toBe('manual')
  })

  it('只登记模型没填价的 USD 规则按官方内置价计费', () => {
    const status = priceStatusOf(rule(), card({ pricing_source: 'catalog' }))
    expect(status.availability).toBe('billable')
    expect(status.source).toBe('builtin')
  })

  it('按次计费模式缺价时会被拒绝（没有官方内置价兜底）', () => {
    const status = priceStatusOf(
      rule({ billing_mode: 'per_request', per_request_price: null }),
      card({ pricing_source: 'fallback' })
    )
    expect(status.availability).toBe('rejected')
    expect(status.missingFields).toEqual(['per_request_price'])
  })

  it('按次计费模式填了价时按手工价计费', () => {
    const status = priceStatusOf(
      rule({ billing_mode: 'per_request', per_request_price: 0.1 }),
      card({ pricing_source: 'fallback' })
    )
    expect(status.availability).toBe('billable')
    expect(status.source).toBe('manual')
  })

  it('价卡快照 found=false 时不算有官方内置价', () => {
    const status = priceStatusOf(undefined, card({ found: false, pricing_source: 'fallback' }))
    expect(status.availability).toBe('rejected')
    expect(status.builtinAvailable).toBe(false)
  })
})

describe('priceSourceKindOf', () => {
  it('manual 归为手工价', () => {
    expect(priceSourceKindOf(card({ pricing_source: 'manual' }))).toBe('manual')
  })

  it('fallback 与 catalog 都归为官方内置价', () => {
    expect(priceSourceKindOf(card({ pricing_source: 'fallback' }))).toBe('builtin')
    expect(priceSourceKindOf(card({ pricing_source: 'catalog' }))).toBe('builtin')
  })

  it('没有可用快照时归为没有价格来源', () => {
    expect(priceSourceKindOf(undefined)).toBe('none')
    expect(priceSourceKindOf(card({ found: false }))).toBe('none')
  })
})

describe('missingRulePriceFields / ruleHasAnyPrice', () => {
  it('USD 只要求输入价与输出价', () => {
    expect(missingRulePriceFields(rule({ input_price: 1, output_price: 2 }))).toEqual([])
    expect(missingRulePriceFields(rule({ input_price: 1 }))).toEqual(['output_price'])
  })

  it('CNY 另外要求缓存写价与缓存读价', () => {
    expect(
      missingRulePriceFields(
        rule({ currency: 'CNY', input_price: 1, output_price: 2 })
      )
    ).toEqual(['cache_write_price', 'cache_read_price'])
  })

  it('按次模式有计费层级就不算缺价', () => {
    const withTier = rule({
      billing_mode: 'per_request',
      per_request_price: null,
      intervals: [{ start: 0, end: 100, price: 1 }]
    })
    expect(missingRulePriceFields(withTier)).toEqual([])
  })

  it('ruleHasAnyPrice 只认真正填了价格的规则', () => {
    expect(ruleHasAnyPrice(rule())).toBe(false)
    expect(ruleHasAnyPrice(rule({ input_price: 0 }))).toBe(true)
  })
})

describe('summarizeGroupPricing', () => {
  it('没有规则时是 no_rules', () => {
    expect(summarizeGroupPricing([]).status).toBe('no_rules')
  })

  it('规则只登记模型没填价时是 builtin_only', () => {
    const summary = summarizeGroupPricing([rule({ models: ['gpt-5.5', 'gpt-5.4'] })])
    expect(summary.status).toBe('builtin_only')
    expect(summary.models).toEqual(['gpt-5.5', 'gpt-5.4'])
    expect(summary.incompleteModels).toEqual([])
  })

  it('规则填全时是 complete', () => {
    const summary = summarizeGroupPricing([
      rule({ models: ['gpt-5.5'], input_price: 1, output_price: 2 })
    ])
    expect(summary.status).toBe('complete')
    expect(summary.incompleteModels).toEqual([])
  })

  it('规则缺价字段时是 incomplete 并列出受影响的模型', () => {
    const summary = summarizeGroupPricing([
      rule({ models: ['gpt-5.5'], input_price: 1, output_price: 2 }),
      rule({ models: ['gpt-5.4'], input_price: 1, output_price: null })
    ])
    expect(summary.status).toBe('incomplete')
    expect(summary.incompleteModels).toEqual(['gpt-5.4'])
    expect(summary.models).toEqual(['gpt-5.5', 'gpt-5.4'])
  })
})
