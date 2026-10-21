import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ModelPlazaContent from '../ModelPlazaContent.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isAuthenticated: true }),
}))

const response = {
  description: '',
  groups: [
    {
      id: 1,
      name: 'Standard',
      description: '',
      platform: 'openai',
      subscription_type: 'standard',
      rate_multiplier: 1,
      peak_rate_enabled: false,
      peak_start: '',
      peak_end: '',
      peak_rate_multiplier: 1,
      is_exclusive: false,
      image_rate_independent: false,
      image_rate_multiplier: 1,
      video_rate_independent: false,
      video_rate_multiplier: 1,
      long_context_pricing_enabled: false,
      models: [],
    },
  ],
}

function mountContent() {
  return mount(ModelPlazaContent, {
    props: { response, loading: false, error: false },
    global: {
      stubs: {
        PlazaFilterBar: true,
        PlazaGroupSection: true,
        Icon: true,
      },
    },
  })
}

describe('ModelPlazaContent source price note', () => {
  it('explains that the table shows source-currency prices, not the Credit balance', () => {
    const wrapper = mountContent()

    expect(wrapper.get('[data-testid="model-plaza-source-price-note"]').text()).toBe(
      'modelPlaza.sourcePriceNote',
    )
    wrapper.unmount()
  })
})
