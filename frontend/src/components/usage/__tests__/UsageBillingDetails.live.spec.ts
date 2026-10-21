import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { UsageLog } from '@/types'
import UsageBillingDetails from '../UsageBillingDetails.vue'

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

const record = {
  group_id: 1,
  model: 'gpt-realtime',
  actual_cost: 0,
  total_cost: 0,
  request_type: 'live',
  duration_ms: 90_000,
} as unknown as UsageLog

describe('Live usage records', () => {
  it('shows the session duration and the billing basis for a Live row', () => {
    const wrapper = mount(UsageBillingDetails, { props: { record } })

    expect(wrapper.find('[data-testid="usage-live-details"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="usage-live-duration"]').text()).toContain('usage.liveDurationMinutes')
    expect(wrapper.get('[data-testid="usage-live-duration"]').text()).toContain('"minutes":1')
    expect(wrapper.get('[data-testid="usage-live-duration"]').text()).toContain('"seconds":30')
    expect(wrapper.get('[data-testid="usage-live-billing"]').text()).toBe('usage.liveNoCharge')
    wrapper.unmount()
  })

  it('does not show Live details for other request types', () => {
    const wrapper = mount(UsageBillingDetails, {
      props: { record: { ...record, request_type: 'stream' } as unknown as UsageLog },
    })

    expect(wrapper.find('[data-testid="usage-live-details"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
