import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import GroupPricingStateBadge from '../GroupPricingStateBadge.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key}:${JSON.stringify(params)}` : key,
    }),
  }
})

describe('GroupPricingStateBadge', () => {
  it('shows the peak state with the backend label and multiplier', () => {
    const wrapper = mount(GroupPricingStateBadge, {
      props: {
        group: {
          deepseek_pricing_state: {
            state: 'peak',
            label: '峰价时段 09:00-12:00',
            timezone: 'Asia/Shanghai',
            multiplier: 1.5,
          },
        },
      },
    })

    const badge = wrapper.get('[data-testid="group-pricing-state-badge"]')
    expect(badge.text()).toContain('峰价时段 09:00-12:00')
    expect(badge.text()).toContain('×1.5')
    expect(badge.classes()).toContain('bg-amber-100')
    expect(badge.attributes('title')).toContain('Asia/Shanghai')
    wrapper.unmount()
  })

  it('shows the valley state in the normal color', () => {
    const wrapper = mount(GroupPricingStateBadge, {
      props: { group: { deepseek_pricing_state: { state: 'valley', label: '谷价时段' } } },
    })

    const badge = wrapper.get('[data-testid="group-pricing-state-badge"]')
    expect(badge.text()).toContain('谷价时段')
    expect(badge.classes()).toContain('bg-emerald-100')
    wrapper.unmount()
  })

  it('shows the disabled state in gray', () => {
    const wrapper = mount(GroupPricingStateBadge, {
      props: { group: { deepseek_pricing_state: { state: 'none', label: '未启用' } } },
    })

    expect(wrapper.get('[data-testid="group-pricing-state-badge"]').classes()).toContain('bg-gray-100')
    wrapper.unmount()
  })

  it('falls back to the localized label when the backend label is missing', () => {
    const wrapper = mount(GroupPricingStateBadge, {
      props: { group: { deepseek_pricing_state: { state: 'peak' } } },
    })

    expect(wrapper.get('[data-testid="group-pricing-state-badge"]').text()).toContain(
      'groupPricingState.badgePeak',
    )
    wrapper.unmount()
  })

  it('renders nothing when the backend does not send the state', () => {
    const wrapper = mount(GroupPricingStateBadge, { props: { group: { id: 1 } } })
    expect(wrapper.find('[data-testid="group-pricing-state"]').exists()).toBe(false)
    wrapper.unmount()

    const invalid = mount(GroupPricingStateBadge, {
      props: { group: { deepseek_pricing_state: { state: 'weird' } } },
    })
    expect(invalid.find('[data-testid="group-pricing-state"]').exists()).toBe(false)
    invalid.unmount()
  })
})
