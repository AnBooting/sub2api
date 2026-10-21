/**
 * 价格总表面板的组件测试。
 *
 * 面板调用的是生产 API 模块（@/api/admin）与生产判定函数，
 * 这里只替换数据来源（mock adminAPI），不断言任何被测逻辑的副本。
 */
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import PricingOverviewPanel from '../PricingOverviewPanel.vue'
import type { Channel, ChannelModelPricing } from '@/api/admin/channels'
import type { AdminGroup } from '@/types'

const { listChannels, getGroups, getModelDefaultPricing } = vi.hoisted(() => ({
  listChannels: vi.fn(),
  getGroups: vi.fn(),
  getModelDefaultPricing: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channels: { list: listChannels, getModelDefaultPricing },
    groups: { getAll: getGroups }
  }
}))

// t() 返回 key（带参数时把参数一起带上，方便断言条数/范围等具体值）
vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}${JSON.stringify(params)}` : key
  })
}))

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

async function mountPanel() {
  const wrapper = mount(PricingOverviewPanel)
  await flushPromises()
  return wrapper
}

describe('PricingOverviewPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listChannels.mockResolvedValue({ items: [], total: 0 })
    getGroups.mockResolvedValue([])
    getModelDefaultPricing.mockResolvedValue({ found: true, snapshot_ready: true })
  })

  it('renders configured prices per rule and scope', async () => {
    listChannels.mockResolvedValue({
      items: [
        channel({
          id: 1,
          name: 'channel-a',
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
      total: 1
    })

    const wrapper = await mountPanel()
    const rows = wrapper.findAll('[data-testid="price-row"]')

    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('gpt-5')
    expect(rows[0].text()).toContain('USD')
    expect(rows[0].text()).toContain('admin.pricing.units.token')
    // 每百万 token 的展示值（per-token × 1e6）
    expect(rows[0].text()).toContain('3')
    expect(rows[0].text()).toContain('15')
    expect(rows[0].text()).toContain('0.3')
    expect(rows[0].text()).toContain('3.75')
    expect(rows[0].text()).toContain('admin.pricing.sources.manual')
    expect(rows[0].text()).toContain('admin.pricing.table.scopeChannel')
  })

  it('filters rows by model name', async () => {
    listChannels.mockResolvedValue({
      items: [
        channel({
          id: 1,
          model_pricing: [
            rule({ models: ['gpt-5'], input_price: 1e-6, output_price: 2e-6 }),
            rule({ models: ['claude-sonnet-4'], input_price: 3e-6, output_price: 15e-6 })
          ]
        })
      ],
      total: 2
    })

    const wrapper = await mountPanel()
    expect(wrapper.findAll('[data-testid="price-row"]')).toHaveLength(2)

    await wrapper.find('[data-testid="filter-model"]').setValue('claude')
    await flushPromises()

    const rows = wrapper.findAll('[data-testid="price-row"]')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('claude-sonnet-4')
  })

  it('filters rows by currency', async () => {
    listChannels.mockResolvedValue({
      items: [
        channel({
          id: 1,
          model_pricing: [
            rule({ models: ['gpt-5'], input_price: 1e-6, output_price: 2e-6 }),
            rule({
              models: ['deepseek-chat'],
              currency: 'CNY',
              input_price: 2e-6,
              output_price: 4e-6,
              cache_read_price: 0,
              cache_write_price: 0
            })
          ]
        })
      ],
      total: 2
    })

    const wrapper = await mountPanel()
    const select = wrapper.find('[data-testid="filter-currency"]')
    const options = select.findAll('option').map(option => option.element.value)
    expect(options).toContain('USD')
    expect(options).toContain('CNY')

    await select.setValue('CNY')
    await flushPromises()

    const rows = wrapper.findAll('[data-testid="price-row"]')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('deepseek-chat')
    expect(rows[0].text()).toContain('CNY')
  })

  it('paginates rows client-side and reports the visible range', async () => {
    const models = Array.from({ length: 60 }, (_, index) => `model-${index}`)
    listChannels.mockResolvedValue({
      items: [
        channel({
          id: 1,
          model_pricing: [rule({ models, input_price: 1e-6, output_price: 2e-6 })]
        })
      ],
      total: 60
    })

    const wrapper = await mountPanel()
    expect(wrapper.findAll('[data-testid="price-row"]')).toHaveLength(50)
    expect(wrapper.get('[data-testid="total-count"]').text()).toContain('60')
    expect(wrapper.get('[data-testid="row-range"]').text()).toContain('50')

    await wrapper.find('[data-testid="next-page"]').trigger('click')
    await flushPromises()

    const rows = wrapper.findAll('[data-testid="price-row"]')
    expect(rows).toHaveLength(10)
    expect(rows[0].text()).toContain('model-50')
  })

  it('shows the empty state when no pricing rule data exists', async () => {
    const wrapper = await mountPanel()
    expect(wrapper.findAll('[data-testid="price-row"]')).toHaveLength(0)
    expect(wrapper.text()).toContain('admin.pricing.table.empty')
  })

  it('shows the default price card column only after a detection run', async () => {
    listChannels.mockResolvedValue({
      items: [
        channel({
          id: 1,
          model_pricing: [rule({ models: ['gpt-5'], input_price: 1e-6, output_price: 2e-6 })]
        })
      ],
      total: 1
    })
    getModelDefaultPricing.mockResolvedValue({
      found: true,
      snapshot_ready: true,
      input_price: 3e-6,
      output_price: 15e-6
    })

    const wrapper = await mountPanel()
    expect(wrapper.get('[data-testid="price-row"]').text()).toContain('admin.pricing.table.cardNotProbed')

    await wrapper.find('[data-testid="detect"]').trigger('click')
    await flushPromises()

    expect(getModelDefaultPricing).toHaveBeenCalledWith('gpt-5', expect.anything())
    expect(wrapper.get('[data-testid="price-row"]').text()).toContain('admin.pricing.table.cardUsable')
  })
})
