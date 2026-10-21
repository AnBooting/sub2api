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

const group = (state: unknown) => ({ deepseek_pricing_state: state })

describe('GroupPricingStateBadge', () => {
  it('shows the peak badge with the backend label and the effective multiplier', () => {
    const wrapper = mount(GroupPricingStateBadge, {
      props: {
        group: group({ state: 'peak', label: '高峰时段', timezone: 'Asia/Shanghai', multiplier: 1.5 }),
      },
    })

    const badge = wrapper.get('[data-testid="group-pricing-state-badge"]')
    expect(badge.text()).toContain('高峰时段')
    expect(badge.text()).toContain('×1.5')
    expect(badge.classes()).toContain('bg-amber-100')
    expect(badge.attributes('title')).toContain('Asia/Shanghai')
    wrapper.unmount()
  })

  it('shows the valley badge in the normal color', () => {
    const wrapper = mount(GroupPricingStateBadge, {
      props: { group: group({ state: 'valley', label: '低谷时段', multiplier: 0.5 }) },
    })

    const badge = wrapper.get('[data-testid="group-pricing-state-badge"]')
    expect(badge.text()).toContain('低谷时段')
    expect(badge.classes()).toContain('bg-emerald-100')
    wrapper.unmount()
  })

  it('shows the disabled state in gray', () => {
    const wrapper = mount(GroupPricingStateBadge, {
      props: { group: group({ state: 'none', label: '未启用' }) },
    })

    const badge = wrapper.get('[data-testid="group-pricing-state-badge"]')
    expect(badge.text()).toContain('未启用')
    expect(badge.classes()).toContain('bg-gray-100')
    wrapper.unmount()
  })

  it('falls back to the localized label when the backend label is missing', () => {
    const wrapper = mount(GroupPricingStateBadge, {
      props: { group: group({ state: 'peak' }) },
    })

    expect(wrapper.get('[data-testid="group-pricing-state-badge"]').text()).toContain('groupPricingState.badgePeak')
    wrapper.unmount()
  })

  it('renders nothing when the field is missing or invalid', () => {
    const missing = mount(GroupPricingStateBadge, { props: { group: { id: 1 } } })
    expect(missing.find('[data-testid="group-pricing-state"]').exists()).toBe(false)
    missing.unmount()

    const invalid = mount(GroupPricingStateBadge, { props: { group: group({ state: 'weird' }) } })
    expect(invalid.find('[data-testid="group-pricing-state"]').exists()).toBe(false)
    invalid.unmount()

    const empty = mount(GroupPricingStateBadge, { props: { group: undefined } })
    expect(empty.find('[data-testid="group-pricing-state"]').exists()).toBe(false)
    empty.unmount()
  })
})
