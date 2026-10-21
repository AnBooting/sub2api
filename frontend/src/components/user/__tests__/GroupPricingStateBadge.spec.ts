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

const peakState = {
  state: 'peak' as const,
  label: '峰价时段 09:00-12:00',
  timezone: 'Asia/Shanghai',
  multiplier: 2,
  next_switch_at: '2026-10-22T09:00:00+08:00',
}

function mountBadge(pricingState: unknown) {
  return mount(GroupPricingStateBadge, {
    props: { group: { deepseek_pricing_state: pricingState } },
  })
}

describe('GroupPricingStateBadge', () => {
  it('shows the peak state with the backend label and multiplier', () => {
    const wrapper = mountBadge(peakState)

    const badge = wrapper.get('[data-testid="group-pricing-state-badge"]')
    expect(badge.text()).toContain('峰价时段 09:00-12:00')
    expect(badge.text()).toContain('×2')
    expect(badge.classes()).toContain('bg-amber-100')
    expect(badge.attributes('title')).toContain('Asia/Shanghai')
    wrapper.unmount()
  })

  it('shows the valley state in the normal color', () => {
    const wrapper = mountBadge({ state: 'valley', label: '谷价时段' })

    const badge = wrapper.get('[data-testid="group-pricing-state-badge"]')
    expect(badge.text()).toContain('谷价时段')
    expect(badge.classes()).toContain('bg-emerald-100')
    wrapper.unmount()
  })

  it('shows the disabled state in gray', () => {
    const wrapper = mountBadge({ state: 'none', label: '未启用' })

    expect(wrapper.get('[data-testid="group-pricing-state-badge"]').classes()).toContain('bg-gray-100')
    wrapper.unmount()
  })

  it('falls back to the localized label when the backend label is missing', () => {
    const wrapper = mountBadge({ state: 'peak' })

    expect(wrapper.get('[data-testid="group-pricing-state-badge"]').text()).toContain(
      'groupPricingState.badgePeak',
    )
    wrapper.unmount()
  })

  it('renders nothing when the backend does not send the state', () => {
    const wrapper = mount(GroupPricingStateBadge, { props: { group: { id: 1 } } })
    expect(wrapper.find('[data-testid="group-pricing-state"]').exists()).toBe(false)
    wrapper.unmount()

    const invalid = mountBadge({ state: 'weird' })
    expect(invalid.find('[data-testid="group-pricing-state"]').exists()).toBe(false)
    invalid.unmount()
  })

  it('shows the next switch time when the backend sends it', () => {
    const wrapper = mountBadge(peakState)

    const nextSwitch = wrapper.get('[data-testid="group-pricing-state-next-switch"]')
    expect(nextSwitch.text()).toContain('2026')
    expect(nextSwitch.text()).toMatch(/\d{2}:\d{2}/)
    expect(nextSwitch.attributes('title')).toContain('groupPricingState.nextSwitch')
    // tooltip 里也带一句「下一档切换时间」
    expect(wrapper.get('[data-testid="group-pricing-state-badge"]').attributes('title')).toContain(
      'groupPricingState.nextSwitch',
    )
    wrapper.unmount()
  })

  it('does not show the next switch time when the field is missing or null', () => {
    const missing = mountBadge({ state: 'peak', label: '峰', timezone: 'Asia/Shanghai' })
    expect(missing.find('[data-testid="group-pricing-state-next-switch"]').exists()).toBe(false)
    expect(missing.find('[data-testid="group-pricing-state-badge"]').exists()).toBe(true)
    missing.unmount()

    const nulled = mountBadge({ ...peakState, next_switch_at: null })
    expect(nulled.find('[data-testid="group-pricing-state-next-switch"]').exists()).toBe(false)
    nulled.unmount()
  })

  it('does not show the next switch time for an invalid value', () => {
    const wrapper = mountBadge({ ...peakState, next_switch_at: 'not-a-date' })

    expect(wrapper.find('[data-testid="group-pricing-state-next-switch"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="group-pricing-state-badge"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('does not show the next switch time when the state is none', () => {
    const wrapper = mountBadge({ ...peakState, state: 'none' })

    expect(wrapper.find('[data-testid="group-pricing-state-next-switch"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
