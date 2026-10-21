import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AvailableChannelsView from '../AvailableChannelsView.vue'

const { getAvailableChannels, getUserGroupRates } = vi.hoisted(() => ({
  getAvailableChannels: vi.fn(),
  getUserGroupRates: vi.fn(),
}))

vi.mock('@/api/channels', () => ({
  default: { getAvailable: getAvailableChannels },
  userChannelsAPI: { getAvailable: getAvailableChannels },
}))

vi.mock('@/api/groups', () => ({
  default: { getUserGroupRates, getAvailable: vi.fn() },
  userGroupsAPI: { getUserGroupRates, getAvailable: vi.fn() },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn() }),
}))

vi.mock('@/utils/featureFlags', () => ({
  FeatureFlags: { modelPlaza: 'modelPlaza' },
  isFeatureFlagEnabled: () => true,
  resolveFeatureFlag: () => true,
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

describe('AvailableChannelsView missing price notice', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getAvailableChannels.mockResolvedValue([
      {
        name: 'channel-a',
        description: '',
        platforms: [
          {
            platform: 'openai',
            groups: [
              {
                id: 1,
                name: 'standard',
                platform: 'openai',
                subscription_type: 'standard',
                rate_multiplier: 1,
                peak_rate_enabled: false,
                peak_start: '',
                peak_end: '',
                peak_rate_multiplier: 1,
                is_exclusive: false,
              },
            ],
            supported_models: [{ name: 'gpt-x', platform: 'openai', pricing: null }],
          },
        ],
      },
    ])
    getUserGroupRates.mockResolvedValue({})
  })

  it('shows the missing-price notice with a link to the price list', async () => {
    const wrapper = mount(AvailableChannelsView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
        },
      },
    })
    await flushPromises()

    const notice = wrapper.get('[data-testid="missing-price-notice"]')
    expect(notice.text()).toContain('missingPrice.title')
    expect(wrapper.find('[data-testid="missing-price-price-card-link"]').exists()).toBe(true)
    wrapper.unmount()
  })
})
