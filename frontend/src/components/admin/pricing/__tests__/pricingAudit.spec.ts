/**
 * 价格审计判定逻辑的单元测试。
 *
 * 这些用例直接调用生产函数（collectPricingAuditData / resolveMissingPriceRows /
 * buildPriceTableRows），不重写判定逻辑。
 */
import { describe, expect, it } from 'vitest'
import {
  buildPriceTableRows,
  collectPricingAuditData,
  effectiveRuleRefs,
  isRuleComplete,
  missingPriceFields,
  resolveMissingPriceRows,
  requiredPriceFields
} from '../pricingAudit'
import type { PricingAuditData } from '../pricingAudit'
import type { Channel, ChannelModelPricing } from '@/api/admin/channels'
import type { AdminGroup } from '@/types'

function rule(partial: Partial<ChannelModelPricing>): ChannelModelPricing {
  return {
    currency: 'USD',
    platform: 'openai',
    models: [],
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
    ...partial
  }
}

function channel(partial: Partial<Channel>): Channel {
  return {
    id: 1,
    name: 'channel-a',
    description: '',
    status: 'active',
    billing_model_source: 'requested',
    restrict_models: false,
    group_ids: [],
    model_pricing: [],
    model_mapping: {},
    apply_pricing_to_account_stats: false,
    account_stats_pricing_rules: [],
    created_at: '',
    updated_at: '',
    ...partial
  } as Channel
}

function group(partial: Partial<AdminGroup>): AdminGroup {
  return {
    id: 10,
    name: 'group-a',
    platform: 'openai',
    model_pricing: [],
    ...partial
  } as AdminGroup
}

describe('collectPricingAuditData', () => {
  it('collects candidate models from channel and group rules and skips wildcards', () => {
    const data = collectPricingAuditData(
      [
        channel({
          id: 1,
          name: 'ch',
          model_pricing: [rule({ models: ['gpt-5', 'gpt-*'] })]
        })
      ],
      [
        group({
          id: 10,
          model_pricing: [rule({ models: ['gpt-5', 'claude-sonnet-4'] })]
        })
      ]
    )

    expect(data.models).toEqual(['claude-sonnet-4', 'gpt-5'])
    expect(data.scopes.map(scope => scope.scope)).toEqual(['group', 'channel'])
  })
})

describe('requiredPriceFields / missingPriceFields', () => {
  it('requires input and output for USD token rules', () => {
    expect(requiredPriceFields({ billing_mode: 'token', currency: 'USD' })).toEqual([
      'input_price',
      'output_price'
    ])
    expect(
      missingPriceFields(rule({ input_price: 1e-6, output_price: 2e-6 }))
    ).toEqual([])
  })

  it('adds cache read/write for CNY token rules', () => {
    expect(requiredPriceFields({ billing_mode: 'token', currency: 'CNY' })).toEqual([
      'input_price',
      'output_price',
      'cache_write_price',
      'cache_read_price'
    ])
    expect(
      missingPriceFields(
        rule({ currency: 'CNY', input_price: 1e-6, output_price: 2e-6 })
      )
    ).toEqual(['cache_write_price', 'cache_read_price'])
  })

  it('requires per_request_price for per-request, image and video modes', () => {
    for (const billing_mode of ['per_request', 'image', 'video'] as const) {
      expect(requiredPriceFields({ billing_mode, currency: 'USD' })).toEqual([
        'per_request_price'
      ])
      expect(missingPriceFields(rule({ billing_mode }))).toEqual(['per_request_price'])
      expect(
        isRuleComplete(rule({ billing_mode, per_request_price: 0.01 }))
      ).toBe(true)
    }
  })
})

describe('resolveMissingPriceRows', () => {
  const groupRule = rule({ models: ['gpt-5'], input_price: 1e-6, output_price: null })

  it('flags an incomplete group rule even when the default price card is complete', () => {
    const data = collectPricingAuditData(
      [],
      [group({ id: 10, name: 'group-a', model_pricing: [groupRule] })]
    )

    const rows = resolveMissingPriceRows(data, {
      // 价卡价格完整，但分组规则命中后整条生效，缺价不能被价卡补上。
      results: {
        'gpt-5': {
          found: true,
          snapshot_ready: true,
          currency: 'USD',
          input_price: 3e-6,
          output_price: 15e-6
        }
      },
      failures: {}
    })

    expect(rows).toHaveLength(1)
    expect(rows[0].model).toBe('gpt-5')
    expect(rows[0].scope).toBe('group')
    expect(rows[0].scopeId).toBe(10)
    expect(rows[0].missingFields).toEqual(['output_price'])
    expect(rows[0].priceSource).toBe('rule')
  })

  it('does not flag a model whose manual rule is complete', () => {
    const data = collectPricingAuditData(
      [],
      [
        group({
          id: 10,
          model_pricing: [rule({ models: ['gpt-5'], input_price: 1e-6, output_price: 2e-6 })]
        })
      ]
    )

    expect(resolveMissingPriceRows(data, { results: {}, failures: {} })).toEqual([])
  })

  it('ignores a channel rule that is shadowed by a group rule of the bound group', () => {
    const data = collectPricingAuditData(
      [
        channel({
          id: 1,
          name: 'ch',
          group_ids: [10],
          model_pricing: [rule({ models: ['gpt-5'], input_price: 1e-6, output_price: 2e-6 })]
        })
      ],
      [
        group({
          id: 10,
          model_pricing: [rule({ models: ['gpt-5'], input_price: 1e-6, output_price: null })]
        })
      ]
    )

    const rows = resolveMissingPriceRows(data, { results: {}, failures: {} })
    expect(rows).toHaveLength(1)
    expect(rows[0].scope).toBe('group')
    expect(rows[0].missingFields).toEqual(['output_price'])
  })

  it('reports a channel rule when the bound group has no group rule for the model', () => {
    const data = collectPricingAuditData(
      [
        channel({
          id: 1,
          name: 'ch',
          group_ids: [10],
          model_pricing: [rule({ models: ['gpt-5'], input_price: 1e-6, output_price: null })]
        })
      ],
      [group({ id: 10, model_pricing: [] })]
    )

    const rows = resolveMissingPriceRows(data, { results: {}, failures: {} })
    expect(rows).toHaveLength(1)
    expect(rows[0].scope).toBe('channel')
    expect(rows[0].scopeId).toBe(1)
    expect(rows[0].missingFields).toEqual(['output_price'])
  })

  it('falls back to the default price card when the matching rule carries no price at all', () => {
    const data = collectPricingAuditData(
      [
        channel({
          id: 1,
          name: 'ch',
          model_pricing: [rule({ models: ['gpt-5'] })]
        })
      ],
      []
    )

    // 价卡价格完整 → 不缺价（由价卡兜底）
    expect(
      resolveMissingPriceRows(data, {
        results: {
          'gpt-5': { found: true, snapshot_ready: true, input_price: 1e-6, output_price: 2e-6 }
        },
        failures: {}
      })
    ).toEqual([])

    // 价卡没有该模型 → 缺价，缺「全部必需价格字段」
    const missing = resolveMissingPriceRows(data, {
      results: { 'gpt-5': { found: false } },
      failures: {}
    })
    expect(missing).toHaveLength(1)
    expect(missing[0].priceSource).toBe('card')
    expect(missing[0].kind).toBe('card_not_found')
    expect(missing[0].missingFields).toEqual(['input_price', 'output_price'])

    // 价卡快照未就绪 → 文案表达「价卡尚未就绪」
    const notReady = resolveMissingPriceRows(data, {
      results: { 'gpt-5': { found: true, snapshot_ready: false } },
      failures: {}
    })
    expect(notReady).toHaveLength(1)
    expect(notReady[0].kind).toBe('card_not_ready')
  })

  it('marks a model as probe failure without blocking the other models', () => {
    const data = collectPricingAuditData(
      [
        channel({
          id: 1,
          name: 'ch',
          model_pricing: [
            rule({ models: ['gpt-5'] }),
            rule({ models: ['gpt-6'] })
          ]
        })
      ],
      []
    )

    const rows = resolveMissingPriceRows(data, {
      // gpt-5 探测失败；gpt-6 探测到价卡里没有该模型。
      results: { 'gpt-6': { found: false } },
      failures: { 'gpt-5': 'network down' }
    })

    expect(rows).toHaveLength(2)
    const failed = rows.find(row => row.model === 'gpt-5')!
    expect(failed.probeFailed).toBe(true)
    expect(failed.kind).toBe('probe_failed')
    const other = rows.find(row => row.model === 'gpt-6')!
    expect(other.probeFailed).toBe(false)
    expect(other.kind).toBe('card_not_found')
  })
})

describe('effectiveRuleRefs', () => {
  it('gives group rules priority over channel rules', () => {
    const data: PricingAuditData = collectPricingAuditData(
      [channel({ id: 1, group_ids: [10], model_pricing: [rule({ models: ['gpt-5'] })] })],
      [group({ id: 10, model_pricing: [rule({ models: ['gpt-5'] })] })]
    )

    const refs = effectiveRuleRefs(data, 'gpt-5')
    expect(refs.map(ref => ref.scope)).toEqual(['group'])
  })
})

describe('buildPriceTableRows', () => {
  it('lists one row per rule and model with converted per-MTok prices', () => {
    const data = collectPricingAuditData(
      [
        channel({
          id: 1,
          name: 'ch',
          model_pricing: [
            rule({
              models: ['gpt-5'],
              input_price: 3e-6,
              output_price: 15e-6,
              cache_read_price: 0.3e-6,
              cache_write_price: 3.75e-6
            })
          ]
        })
      ],
      []
    )

    const rows = buildPriceTableRows(data, { results: {}, failures: {} })
    expect(rows).toHaveLength(1)
    expect(rows[0].model).toBe('gpt-5')
    expect(rows[0].currency).toBe('USD')
    expect(rows[0].unit).toBe('token')
    expect(rows[0].inputPrice).toBe(3)
    expect(rows[0].outputPrice).toBe(15)
    expect(rows[0].cacheReadPrice).toBe(0.3)
    expect(rows[0].cacheWritePrice).toBe(3.75)
    expect(rows[0].source).toBe('manual')
    expect(rows[0].scopeName).toBe('ch')
  })

  it('shows per-request price for per_request mode and marks unset prices as null', () => {
    const data = collectPricingAuditData(
      [
        channel({
          id: 1,
          model_pricing: [
            rule({ models: ['gpt-image'], billing_mode: 'image', per_request_price: 0.04 })
          ]
        })
      ],
      []
    )

    const rows = buildPriceTableRows(data, { results: {}, failures: {} })
    expect(rows[0].perRequestPrice).toBe(0.04)
    expect(rows[0].inputPrice).toBeNull()
    expect(rows[0].unit).toBe('image')
    expect(rows[0].source).toBe('manual')
  })

  it('marks a rule without prices as default, or as card-backed once the card is probed', () => {
    const data = collectPricingAuditData(
      [channel({ id: 1, model_pricing: [rule({ models: ['gpt-5'] })] })],
      []
    )

    expect(buildPriceTableRows(data, { results: {}, failures: {} })[0].source).toBe('default')
    expect(
      buildPriceTableRows(data, {
        results: {
          'gpt-5': { found: true, snapshot_ready: true, input_price: 1e-6, output_price: 2e-6 }
        },
        failures: {}
      })[0].source
    ).toBe('card')
  })
})
