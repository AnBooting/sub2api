import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  BUILTIN_PLATFORM_CATALOG,
  resetPlatformCatalog,
  setPlatformCatalog
} from '@/constants/platformCatalog'
import ChannelsView from '@/views/admin/ChannelsView.vue'

const { listChannels, getGroups, syncUpstreamModels, getModelDefaultPricing, getModelMappingCandidates, showSuccess, showError } = vi.hoisted(() => ({
  listChannels: vi.fn(),
  getGroups: vi.fn(),
  syncUpstreamModels: vi.fn(),
  getModelDefaultPricing: vi.fn(),
  getModelMappingCandidates: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channels: { list: listChannels, getModelDefaultPricing },
    groups: { getAll: getGroups, getModelMappingCandidates, syncUpstreamModels },
    settings: { getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false }) }
  }
}))

vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess, showError }) }))

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

function mountView() {
  return mount(ChannelsView, {
    global: {
      stubs: {
        AppLayout: defineComponent({ template: '<main><slot /></main>' }),
        TablePageLayout: defineComponent({
          template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>'
        }),
        BaseDialog: defineComponent({
          props: ['show'],
          template: '<section v-if="show"><slot /><slot name="footer" /></section>'
        }),
        PricingEntryCard: defineComponent({
          name: 'PricingEntryCard',
          props: ['entry'],
          template: '<div class="pricing-entry" :data-pricing="JSON.stringify(entry)">{{ entry.models.join(", ") }}</div>'
        }),
        DataTable: true,
        Pagination: true,
        ConfirmDialog: true,
        EmptyState: true,
        Select: true,
        Icon: true,
        PlatformIcon: true,
        Toggle: true
      }
    }
  })
}

async function openDialog(wrapper: ReturnType<typeof mountView>) {
  await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === 'admin.channels.createChannel')!.trigger('click')
  await flushPromises()
}

async function enablePlatform(wrapper: ReturnType<typeof mountView>, platform: string) {
  const label = wrapper.findAll('label').find(label => label.text() === `admin.groups.platforms.${platform}`)
  expect(label).toBeDefined()
  await label!.get('input[type="checkbox"]').setValue(true)
  await wrapper.findAll('button.channel-tab').find(button => button.text() === `admin.groups.platforms.${platform}`)!.trigger('click')
}

function visibleSyncButtons(wrapper: ReturnType<typeof mountView>) {
  return wrapper.findAll('button').filter(button => button.isVisible() && [
    'admin.channels.form.syncLatestModels',
    'admin.channels.form.syncingModels'
  ].includes(button.text()))
}

describe('ChannelsView pricing model sync', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    listChannels.mockResolvedValue({ items: [], total: 0 })
    getGroups.mockResolvedValue([])
    syncUpstreamModels.mockReset()
    getModelDefaultPricing.mockReset()
    getModelMappingCandidates.mockReset()
  })

  afterEach(() => {
    resetPlatformCatalog()
  })

  it.each([
    ['openai', true],
    ['opencode_go', true],
    ['typesafe', true],
    ['command_code', true],
    ['cline', true]
  ] as const)('offers upstream model sync for %s without requiring a pricing catalog', async (platform, expected) => {
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, platform)

    expect(visibleSyncButtons(wrapper)).toHaveLength(expected ? 1 : 0)
    expect(visibleSyncButtons(wrapper)[0].attributes('disabled')).toBeDefined()
    expect(syncUpstreamModels).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('offers upstream sync for a new platform even without a pricing catalog', async () => {
    const platform = { id: 'acme_router', display_name: 'Acme Router', gateway: 'openai' as const, cn_provider: false }
    setPlatformCatalog({
      ...BUILTIN_PLATFORM_CATALOG,
      platforms: [...BUILTIN_PLATFORM_CATALOG.platforms, platform]
    })
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, platform.id)
    expect(visibleSyncButtons(wrapper)).toHaveLength(1)

    setPlatformCatalog({
      ...BUILTIN_PLATFORM_CATALOG,
      platforms: [...BUILTIN_PLATFORM_CATALOG.platforms, { ...platform, litellm_provider: 'acme' }]
    })
    await flushPromises()
    expect(visibleSyncButtons(wrapper)).toHaveLength(1)
    wrapper.unmount()
  })

  it('blocks additional syncs while one request is in flight and adds its models once', async () => {
    let resolveSync!: (value: { models: string[] }) => void
    getGroups.mockResolvedValue([{ id: 3, name: 'Group A', platform: 'openai' }, { id: 4, name: 'Group B', platform: 'openai' }])
    syncUpstreamModels.mockReturnValue(new Promise(resolve => { resolveSync = resolve }))
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, 'openai')
    for (const name of ['Group A', 'Group B']) {
      await wrapper.findAll('label').find(label => label.text().includes(name))!.get('input').setValue(true)
    }
    await visibleSyncButtons(wrapper)[0].trigger('click')
    await visibleSyncButtons(wrapper)[0].trigger('click')
    await enablePlatform(wrapper, 'opencode_go')

    expect(visibleSyncButtons(wrapper)[0].attributes('disabled')).toBeDefined()
    await visibleSyncButtons(wrapper)[0].trigger('click')
    expect(syncUpstreamModels).toHaveBeenCalledOnce()
    expect(syncUpstreamModels).toHaveBeenCalledWith([3, 4], 'openai')

    resolveSync({ models: ['gpt-latest'] })
    await flushPromises()
    expect(wrapper.findAll('.pricing-entry')).toHaveLength(1)
    expect(wrapper.get('.pricing-entry').text()).toBe('gpt-latest')
    expect(visibleSyncButtons(wrapper)[0].attributes('disabled')).toBeUndefined()
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('syncs only returned upstream models and snapshots separate USD and CNY prices', async () => {
    getGroups.mockResolvedValue([{ id: 3, name: 'Upstream group', platform: 'openai' }])
    syncUpstreamModels.mockResolvedValue({ models: ['gpt-supported', 'deepseek-supported', 'gpt-supported'], failed_accounts: 0 })
    getModelDefaultPricing.mockImplementation((model: string) => Promise.resolve(model.startsWith('deepseek')
      ? { found: true, snapshot_ready: true, currency: 'CNY', input_price: 2e-6, output_price: 4e-6, cache_read_price: 0, cache_write_price: 0 }
      : { found: true, currency: 'USD', input_price: 3e-6, output_price: 15e-6 }))
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, 'openai')
    await wrapper.findAll('label').find(label => label.text().includes('Upstream group'))!.get('input').setValue(true)
    await visibleSyncButtons(wrapper)[0].trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.pricing-entry')).toHaveLength(2)
    await wrapper.findAll('button').find(button => button.text() === 'admin.channels.form.autoPricing')!.trigger('click')
    await flushPromises()
    const entries = wrapper.findAll('.pricing-entry').map(card => JSON.parse(card.attributes('data-pricing')))
    expect(entries).toEqual(expect.arrayContaining([
      expect.objectContaining({ models: ['deepseek-supported'], currency: 'CNY', input_price: 2, output_price: 4, cache_write_price: 0 }),
      expect.objectContaining({ models: ['gpt-supported'], currency: 'USD', input_price: 3, output_price: 15 })
    ]))
    await wrapper.findAll('button').find(button => button.text() === 'admin.channels.form.autoPricing')!.trigger('click')
    await flushPromises()
    expect(getModelDefaultPricing).toHaveBeenCalledTimes(2) // saved form values count as manual prices
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps incomplete CNY prices unset and reports the missing model', async () => {
    getGroups.mockResolvedValue([{ id: 3, name: 'Upstream group', platform: 'openai' }])
    getModelMappingCandidates.mockResolvedValue(['deepseek-missing'])
    getModelDefaultPricing.mockResolvedValue({ found: true, snapshot_ready: false, currency: 'CNY', input_price: 2e-6, output_price: 4e-6, cache_write_price: null })
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, 'openai')
    await wrapper.findAll('label').find(label => label.text().includes('Upstream group'))!.get('input').setValue(true)
    await wrapper.findAll('button').find(button => button.text() === 'admin.channels.form.autoMapping')!.trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'admin.channels.form.autoPricing')!.trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.pricing-entry')).toHaveLength(0)
    expect(showError).toHaveBeenCalledWith('admin.channels.form.autoPricingMissing')
    wrapper.unmount()
  })

  it('discards upstream sync results when linked groups change while loading', async () => {
    getGroups.mockResolvedValue([{ id: 3, name: 'Upstream group', platform: 'openai' }])
    let resolveSync!: (value: { models: string[] }) => void
    syncUpstreamModels.mockReturnValue(new Promise(resolve => { resolveSync = resolve }))
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, 'openai')
    const checkbox = wrapper.findAll('label').find(label => label.text().includes('Upstream group'))!.get('input')
    await checkbox.setValue(true)
    await visibleSyncButtons(wrapper)[0].trigger('click')
    await checkbox.setValue(false)
    resolveSync({ models: ['stale-model'] })
    await flushPromises()
    expect(wrapper.findAll('.pricing-entry')).toHaveLength(0)
    expect(showError).toHaveBeenCalledWith('admin.channels.form.modelActionChanged')
    wrapper.unmount()
  })

  it('does not overwrite a manual edit made while automatic pricing is loading', async () => {
    getGroups.mockResolvedValue([{ id: 3, name: 'Upstream group', platform: 'openai' }])
    syncUpstreamModels.mockResolvedValue({ models: ['gpt-supported'], failed_accounts: 0 })
    let resolvePrice!: (value: { found: boolean, input_price: number, output_price: number }) => void
    getModelDefaultPricing.mockReturnValue(new Promise(resolve => { resolvePrice = resolve }))
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, 'openai')
    await wrapper.findAll('label').find(label => label.text().includes('Upstream group'))!.get('input').setValue(true)
    await visibleSyncButtons(wrapper)[0].trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'admin.channels.form.autoPricing')!.trigger('click')
    const card = wrapper.findComponent({ name: 'PricingEntryCard' })
    card.vm.$emit('update', { ...card.props('entry'), input_price: 99 })
    resolvePrice({ found: true, input_price: 3e-6, output_price: 15e-6 })
    await flushPromises()
    expect(JSON.parse(wrapper.get('.pricing-entry').attributes('data-pricing')).input_price).toBe(99)
    expect(showError).toHaveBeenCalledWith('admin.channels.form.modelActionChanged')
    wrapper.unmount()
  })

  it('imports models from selected upstream groups without adding pricing entries', async () => {
    getGroups.mockResolvedValue([{ id: 3, name: 'Upstream group', platform: 'openai' }])
    getModelMappingCandidates.mockResolvedValue(['gpt-custom', 'deepseek-custom', 'gpt-custom'])
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, 'openai')
    const importButton = () => wrapper.findAll('button').find(button => button.text() === 'admin.channels.form.autoMapping')!
    expect(importButton().attributes('disabled')).toBeDefined()
    await wrapper.findAll('label').find(label => label.text().includes('Upstream group'))!.get('input').setValue(true)
    await importButton().trigger('click')
    await flushPromises()
    expect(getModelMappingCandidates).toHaveBeenCalledWith(3, 'openai')
    expect(wrapper.findAll('input').map(input => input.element.value)).toEqual(expect.arrayContaining(['gpt-custom', 'deepseek-custom']))
    expect(wrapper.findAll('.pricing-entry')).toHaveLength(0)
    await importButton().trigger('click')
    await flushPromises()
    expect(wrapper.findAll('input').filter(input => input.element.value === 'gpt-custom')).toHaveLength(2)
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('discards results when linked groups change while loading', async () => {
    getGroups.mockResolvedValue([{ id: 3, name: 'Upstream group', platform: 'openai' }])
    let resolveModels!: (value: string[]) => void
    getModelMappingCandidates.mockReturnValue(new Promise(resolve => { resolveModels = resolve }))
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, 'openai')
    const checkbox = wrapper.findAll('label').find(label => label.text().includes('Upstream group'))!.get('input')
    await checkbox.setValue(true)
    await wrapper.findAll('button').find(button => button.text() === 'admin.channels.form.autoMapping')!.trigger('click')
    await checkbox.setValue(false)
    resolveModels(['stale-model'])
    await flushPromises()
    expect(wrapper.findAll('input').some(input => input.element.value === 'stale-model')).toBe(false)
    expect(showError).toHaveBeenCalledWith('admin.channels.form.autoMappingGroupsChanged')
    wrapper.unmount()
  })
})
