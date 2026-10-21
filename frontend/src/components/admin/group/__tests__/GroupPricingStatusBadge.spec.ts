import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { ChannelModelPricing } from '@/api/admin/channels'
import GroupPricingStatusBadge from '../GroupPricingStatusBadge.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key}:${JSON.stringify(params)}` : key
    })
  }
})

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

const mountBadge = (rules: ChannelModelPricing[] | null) =>
  mount(GroupPricingStatusBadge, { props: { rules } })

describe('GroupPricingStatusBadge', () => {
  it('没有规则时显示未配置定价规则', () => {
    const wrapper = mountBadge([])
    const badge = wrapper.get('[data-testid="group-pricing-status"]')
    expect(badge.attributes('data-status')).toBe('no_rules')
    expect(badge.text()).toBe('admin.groups.pricingStatus.noRules')
  })

  it('规则填全时显示定价完整', () => {
    const wrapper = mountBadge([rule({ input_price: 1, output_price: 2 })])
    const badge = wrapper.get('[data-testid="group-pricing-status"]')
    expect(badge.attributes('data-status')).toBe('complete')
    expect(badge.text()).toBe('admin.groups.pricingStatus.complete')
  })

  it('规则缺价字段时显示缺价模型数量', () => {
    const wrapper = mountBadge([
      rule({ models: ['gpt-5.5', 'gpt-5.4'], input_price: 1, output_price: null })
    ])
    const badge = wrapper.get('[data-testid="group-pricing-status"]')
    expect(badge.attributes('data-status')).toBe('incomplete')
    expect(badge.text()).toContain('admin.groups.pricingStatus.incomplete')
    expect(badge.text()).toContain('2')
  })

  it('规则只登记模型时显示按官方内置价计费', () => {
    const wrapper = mountBadge([rule()])
    const badge = wrapper.get('[data-testid="group-pricing-status"]')
    expect(badge.attributes('data-status')).toBe('builtin_only')
    expect(badge.text()).toBe('admin.groups.pricingStatus.builtinOnly')
  })

  it('提示语区分官方内置价与缺价拒绝', () => {
    const wrapper = mountBadge([rule({ models: ['gpt-5.4'], input_price: 1 })])
    const title = wrapper.get('[data-testid="group-pricing-status"]').attributes('title')
    expect(title).toContain('admin.groups.pricingStatus.tip.incomplete')
    expect(title).toContain('gpt-5.4')
  })

  it('null 规则列表按没有规则处理', () => {
    const wrapper = mountBadge(null)
    expect(
      wrapper.get('[data-testid="group-pricing-status"]').attributes('data-status')
    ).toBe('no_rules')
  })
})
