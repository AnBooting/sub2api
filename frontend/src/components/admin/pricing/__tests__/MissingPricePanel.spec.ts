/**
 * 缺价清单面板的组件测试。
 *
 * 面板调用生产 API 模块与生产判定函数，这里只替换数据来源（mock adminAPI），
 * 并用受控的 pending promise 断言并发上限与可中断行为。
 */
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import MissingPricePanel from '../MissingPricePanel.vue'
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
  const wrapper = mount(MissingPricePanel)
  await flushPromises()
  return wrapper
}

/** 受控的探测 mock：调用方手动 resolve，用于断言并发上限。 */
function useControlledProbe() {
  const resolvers: (() => void)[] = []
  getModelDefaultPricing.mockImplementation(
    (_model: string, options?: { signal?: AbortSignal }) =>
      new Promise((resolve, reject) => {
        resolvers.push(() => resolve({ found: true, snapshot_ready: true }))
        options?.signal?.addEventListener('abort', () => {
          const error = new Error('aborted')
          error.name = 'AbortError'
          reject(error)
        })
      })
  )
  return resolvers
}

describe('MissingPricePanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listChannels.mockResolvedValue({ items: [], total: 0 })
    getGroups.mockResolvedValue([])
    getModelDefaultPricing.mockResolvedValue({ found: true, snapshot_ready: true })
  })

  it('shows the "not detected yet" empty state before any detection run', async () => {
    const wrapper = await mountPanel()

    expect(wrapper.text()).toContain('admin.pricing.missing.notStarted')
    expect(wrapper.text()).not.toContain('admin.pricing.missing.noMissing')
    expect(wrapper.find('[data-testid="start-probe"]').exists()).toBe(true)
  })

  it('lists a model with an incomplete group rule and names the missing field', async () => {
    getGroups.mockResolvedValue([
      group({
        id: 10,
        name: 'group-a',
        model_pricing: [rule({ models: ['gpt-5'], input_price: 1e-6, output_price: null })]
      })
    ])
    getModelDefaultPricing.mockResolvedValue({
      found: true,
      snapshot_ready: true,
      input_price: 3e-6,
      output_price: 15e-6
    })

    const wrapper = await mountPanel()
    await wrapper.find('[data-testid="start-probe"]').trigger('click')
    await flushPromises()

    const rows = wrapper.findAll('[data-testid="missing-row"]')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('gpt-5')
    expect(rows[0].text()).toContain('admin.pricing.missing.scopeGroup')
    expect(rows[0].text()).toContain('admin.pricing.fields.outputPrice')
    expect(wrapper.text()).not.toContain('admin.pricing.missing.noMissing')
  })

  it('flags an incomplete group rule even when the default price card is complete', async () => {
    getGroups.mockResolvedValue([
      group({
        id: 10,
        model_pricing: [rule({ models: ['gpt-5'], input_price: 1e-6, output_price: null })]
      })
    ])
    getModelDefaultPricing.mockResolvedValue({
      found: true,
      snapshot_ready: true,
      input_price: 3e-6,
      output_price: 15e-6
    })

    const wrapper = await mountPanel()
    await wrapper.find('[data-testid="start-probe"]').trigger('click')
    await flushPromises()

    expect(wrapper.findAll('[data-testid="missing-row"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="missing-row"]').text()).toContain(
      'admin.pricing.fields.outputPrice'
    )
  })

  it('shows the "no missing model" empty state when every manual rule is complete', async () => {
    getGroups.mockResolvedValue([
      group({
        id: 10,
        model_pricing: [rule({ models: ['gpt-5'], input_price: 1e-6, output_price: 2e-6 })]
      })
    ])

    const wrapper = await mountPanel()
    await wrapper.find('[data-testid="start-probe"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('admin.pricing.missing.noMissing')
    expect(wrapper.text()).not.toContain('admin.pricing.missing.notStarted')
  })

  it('marks a failed probe on that row only and keeps the rest of the table', async () => {
    listChannels.mockResolvedValue({
      items: [
        channel({
          id: 1,
          name: 'channel-a',
          model_pricing: [
            rule({ models: ['gpt-5'] }),
            rule({ models: ['gpt-6'] })
          ]
        })
      ],
      total: 2
    })
    getModelDefaultPricing.mockImplementation((model: string) => {
      if (model === 'gpt-5') return Promise.reject(new Error('network down'))
      return Promise.resolve({ found: false })
    })

    const wrapper = await mountPanel()
    await wrapper.find('[data-testid="start-probe"]').trigger('click')
    await flushPromises()

    const rows = wrapper.findAll('[data-testid="missing-row"]')
    expect(rows).toHaveLength(2)
    const failedRow = rows.find(row => row.text().includes('gpt-5'))!
    expect(failedRow.text()).toContain('admin.pricing.missing.probeFailed')
    const otherRow = rows.find(row => row.text().includes('gpt-6'))!
    expect(otherRow.text()).toContain('admin.pricing.missing.allRequired')
  })

  it('emits an edit-pricing target for the channel scope of a missing model', async () => {
    listChannels.mockResolvedValue({
      items: [
        channel({
          id: 7,
          name: 'channel-a',
          group_ids: [10],
          model_pricing: [rule({ models: ['gpt-5'], input_price: 1e-6, output_price: null })]
        })
      ],
      total: 1
    })
    getGroups.mockResolvedValue([group({ id: 10, model_pricing: [] })])

    const wrapper = await mountPanel()
    await wrapper.find('[data-testid="start-probe"]').trigger('click')
    await flushPromises()

    await wrapper.find('[data-testid="edit-pricing"]').trigger('click')
    const emitted = wrapper.emitted('edit-pricing')
    expect(emitted).toBeTruthy()
    expect(emitted![0][0]).toMatchObject({
      channelId: 7,
      platform: 'openai',
      model: 'gpt-5'
    })
  })

  it('keeps at most 20 probes in flight and reports progress', async () => {
    const models = Array.from({ length: 60 }, (_, index) => `model-${index}`)
    listChannels.mockResolvedValue({
      items: [channel({ id: 1, model_pricing: [rule({ models })] })],
      total: 60
    })
    const resolvers = useControlledProbe()

    const wrapper = await mountPanel()
    await wrapper.find('[data-testid="start-probe"]').trigger('click')
    await flushPromises()

    // 同时在途请求不超过 20
    expect(getModelDefaultPricing).toHaveBeenCalledTimes(20)
    expect(resolvers).toHaveLength(20)
    expect(wrapper.get('[data-testid="probe-progress"]').text()).toContain(
      'admin.pricing.missing.progress{"done":0,"total":60}'
    )

    // 释放这一批后才会发起下一批，仍不超过 20 在途
    resolvers.splice(0).forEach(resolve => resolve())
    await flushPromises()
    expect(getModelDefaultPricing).toHaveBeenCalledTimes(40)
  })

  it('stops issuing new probes after the user cancels the detection', async () => {
    const models = Array.from({ length: 60 }, (_, index) => `model-${index}`)
    listChannels.mockResolvedValue({
      items: [channel({ id: 1, model_pricing: [rule({ models })] })],
      total: 60
    })
    useControlledProbe()

    const wrapper = await mountPanel()
    await wrapper.find('[data-testid="start-probe"]').trigger('click')
    await flushPromises()
    expect(getModelDefaultPricing).toHaveBeenCalledTimes(20)

    await wrapper.find('[data-testid="stop-probe"]').trigger('click')
    await flushPromises()

    // 取消后不再发起新请求
    expect(getModelDefaultPricing).toHaveBeenCalledTimes(20)
    expect(wrapper.get('[data-testid="probe-stopped"]').text()).toContain(
      'admin.pricing.missing.stopped'
    )
  })

  it('aborts the in-flight probes when the component is unmounted', async () => {
    const models = Array.from({ length: 30 }, (_, index) => `model-${index}`)
    listChannels.mockResolvedValue({
      items: [channel({ id: 1, model_pricing: [rule({ models })] })],
      total: 30
    })
    const signals: (AbortSignal | undefined)[] = []
    getModelDefaultPricing.mockImplementation(
      (_model: string, options?: { signal?: AbortSignal }) =>
        new Promise((_resolve, reject) => {
          signals.push(options?.signal)
          options?.signal?.addEventListener('abort', () => {
            const error = new Error('aborted')
            error.name = 'AbortError'
            reject(error)
          })
        })
    )

    const wrapper = await mountPanel()
    await wrapper.find('[data-testid="start-probe"]').trigger('click')
    await flushPromises()

    expect(signals.length).toBe(20)
    expect(signals.every(signal => signal?.aborted === false)).toBe(true)

    wrapper.unmount()
    await flushPromises()

    expect(signals.every(signal => signal?.aborted === true)).toBe(true)
  })
})
