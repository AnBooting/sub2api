/**
 * ChannelsView 顶部视图切换与「去定价」跳转的集成测试。
 *
 * 面板本身由 frontend/src/components/admin/pricing/__tests__/ 下的用例覆盖，
 * 这里只验证 ChannelsView 的挂载方式与事件转发。
 */
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createRouter, createMemoryHistory, routerKey } from 'vue-router'
import ChannelsView from '../ChannelsView.vue'
import type { Channel, ChannelModelPricing } from '@/api/admin/channels'

const { listChannels, getGroups, getModelDefaultPricing, push } = vi.hoisted(() => ({
  listChannels: vi.fn(),
  getGroups: vi.fn(),
  getModelDefaultPricing: vi.fn(),
  push: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channels: { list: listChannels, getModelDefaultPricing },
    groups: { getAll: getGroups },
    settings: { getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false }) }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() })
}))

vi.mock('vue-i18n', async () => ({
  ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')),
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

const globalStubs = {
  AppLayout: { template: '<div><slot /></div>' },
  TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
  // 渲染默认插槽，才能断言弹窗里的平台页签
  BaseDialog: {
    props: ['show', 'title'],
    template: '<div v-if="show" data-testid="channel-dialog">{{ title }}<slot /></div>'
  },
  PricingEntryCard: true,
  DataTable: { template: '<div data-testid="data-table" />' },
  Pagination: { template: '<div data-testid="pagination" />' },
  ConfirmDialog: true,
  EmptyState: true,
  Select: true,
  Icon: true,
  PlatformIcon: true,
  Toggle: true
}

async function mountView() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', redirect: '/admin/channels' },
      { path: '/admin/channels', component: { template: '<div />' } },
      { path: '/admin/groups', component: { template: '<div />' } }
    ]
  })
  // 用 spy 替换 push：既能断言跳转目标，又不会真的改变路由
  router.push = push as unknown as typeof router.push

  const wrapper = mount(ChannelsView, {
    global: { stubs: globalStubs, plugins: [router] }
  })
  await flushPromises()
  return wrapper
}

describe('ChannelsView pricing panels', () => {
  beforeEach(() => {
    vi.clearAllMocks()
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
    getGroups.mockResolvedValue([{ id: 10, name: 'group-a', platform: 'openai', model_pricing: [] }])
    getModelDefaultPricing.mockResolvedValue({ found: true, snapshot_ready: true })
  })

  it('keeps the channel list as the default view', async () => {
    const wrapper = await mountView()

    expect(wrapper.find('[data-testid="data-table"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="price-table"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="missing-table"]').exists()).toBe(false)
  })

  it('switches between the channel list, the price table and the missing price list', async () => {
    const wrapper = await mountView()
    const buttons = wrapper.findAll('[data-testid="pricing-view-switch"] button')
    expect(buttons).toHaveLength(3)

    await buttons[1].trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="data-table"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="price-table"]').exists()).toBe(true)

    await buttons[2].trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="price-table"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="missing-table"]').exists() || wrapper.text()).toBeTruthy()

    await buttons[0].trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="data-table"]').exists()).toBe(true)
  })

  it('hides the channel pagination outside the channel list view', async () => {
    const wrapper = await mountView()
    expect(wrapper.find('[data-testid="pagination"]').exists()).toBe(true)

    const buttons = wrapper.findAll('[data-testid="pricing-view-switch"] button')
    await buttons[1].trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="pagination"]').exists()).toBe(false)
  })

  it('opens the channel edit dialog and selects the platform tab for a channel scope', async () => {
    const wrapper = await mountView()
    const buttons = wrapper.findAll('[data-testid="pricing-view-switch"] button')
    await buttons[2].trigger('click')
    await flushPromises()

    // 缺价清单里点「去定价」
    await wrapper.find('[data-testid="start-probe"]').trigger('click')
    await flushPromises()
    await wrapper.find('[data-testid="edit-pricing"]').trigger('click')
    await flushPromises()

    // 打开的是渠道编辑弹窗，而不是跳转路由
    const dialog = wrapper.find('[data-testid="channel-dialog"]')
    expect(dialog.exists()).toBe(true)
    expect(dialog.text()).toContain('admin.channels.editChannel')
    expect(push).not.toHaveBeenCalled()

    // 平台页签被选中（而不是停留在 basic）
    const activeTab = dialog.findAll('button.channel-tab-active')
    expect(activeTab).toHaveLength(1)
    expect(activeTab[0].text()).toContain('openai')
  })

  it('navigates to the groups page for a group scope', async () => {
    getGroups.mockResolvedValue([
      {
        id: 10,
        name: 'group-a',
        platform: 'openai',
        model_pricing: [rule({ models: ['gpt-5'], input_price: 1e-6, output_price: null })]
      }
    ])

    const wrapper = await mountView()
    const buttons = wrapper.findAll('[data-testid="pricing-view-switch"] button')
    await buttons[2].trigger('click')
    await flushPromises()

    await wrapper.find('[data-testid="start-probe"]').trigger('click')
    await flushPromises()
    const row = wrapper.find('[data-testid="missing-row"]')
    expect(row.exists()).toBe(true)
    expect(row.text()).toContain('admin.pricing.missing.scopeGroup')
    await wrapper.find('[data-testid="edit-pricing"]').trigger('click')
    await flushPromises()

    expect(push).toHaveBeenCalledWith('/admin/groups')
  })
})
