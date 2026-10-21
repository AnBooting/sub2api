import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import MissingPriceNotice from '../MissingPriceNotice.vue'

const { plazaEnabled } = vi.hoisted(() => ({ plazaEnabled: { value: true } }))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

vi.mock('@/utils/featureFlags', () => ({
  FeatureFlags: { modelPlaza: 'modelPlaza' },
  isFeatureFlagEnabled: () => plazaEnabled.value,
}))

describe('MissingPriceNotice', () => {
  it('explains that unpriced models are not served and links to the price list', () => {
    plazaEnabled.value = true
    const wrapper = mount(MissingPriceNotice, {
      global: { stubs: { RouterLink: { template: '<a><slot /></a>' } } },
    })

    expect(wrapper.get('[data-testid="missing-price-notice"]').text()).toContain('missingPrice.title')
    expect(wrapper.get('[data-testid="missing-price-notice"]').text()).toContain('missingPrice.description')
    expect(wrapper.find('[data-testid="missing-price-price-card-link"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('hides the price list link when the price card is disabled', () => {
    plazaEnabled.value = false
    const wrapper = mount(MissingPriceNotice, {
      global: { stubs: { RouterLink: { template: '<a><slot /></a>' } } },
    })

    expect(wrapper.find('[data-testid="missing-price-price-card-link"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
