import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import GroupLiveBadge from '../GroupLiveBadge.vue'
import type { Group } from '@/types'

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

const group = (over: Partial<Group>): Group =>
  ({ allow_live: false, audio_realtime_price_per_min: null, ...over } as Group)

describe('GroupLiveBadge', () => {
  it('shows the Live badge when the group enables Live', () => {
    const wrapper = mount(GroupLiveBadge, { props: { group: group({ allow_live: true }) } })

    expect(wrapper.get('[data-testid="group-live-badge"]').text()).toBe('groupLive.badge')
    expect(wrapper.find('[data-testid="group-live-price"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows the realtime per-minute price as a source price', () => {
    const wrapper = mount(GroupLiveBadge, { props: { group: group({ audio_realtime_price_per_min: 0.06 }) } })

    const price = wrapper.get('[data-testid="group-live-price"]').text()
    expect(price).toContain('groupLive.realtimePrice')
    expect(price).toContain('$0.06')
    expect(wrapper.find('[data-testid="group-live-badge"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('renders nothing when the group has no Live information', () => {
    const wrapper = mount(GroupLiveBadge, { props: { group: group({}) } })

    expect(wrapper.find('[data-testid="group-live-info"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
