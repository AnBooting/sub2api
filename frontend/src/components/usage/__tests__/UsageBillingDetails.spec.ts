import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { UsageLog } from '@/types'
import UsageBillingDetails from '../UsageBillingDetails.vue'
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
const oldRecord = { group_id: 1, group: { name: 'Original A', rate_multiplier: 999 }, model: 'model-original', actual_cost: 38.5, account: { name: 'secret-account' }, upstream_request_id: 'secret-upstream-id' } as unknown as UsageLog
const snapshot = {
  v: 2 as const, quota_unit: 'credits' as const, request_started_at: '2026-10-12T08:00:00Z', price_version: 'version-locked', source: 'native_cny', currency: 'CNY', unit: 'per_1m_tokens',
  period: { label: 'peak', timezone: 'Asia/Shanghai', window: '14:00-18:00' },
  items: [{ kind: 'input', base_credit_price: 2, usage: 500000, normalized_usage: 0.5, subtotal: 1 }],
  served_group_id: 2, group_multiplier: 1.2, total_credit: 1.2,
}
describe('readonly stored billing detail', () => {
  it.each([undefined, null, { v: 1 as const, currency: 'USD', input_per_token: 9 }])('does not reconstruct historical nullable facts or locked prices (%s)', (pricing_snapshot) => {
    const wrapper = mount(UsageBillingDetails, { props: { record: { ...oldRecord, pricing_snapshot } } })
    expect(wrapper.text()).toContain('Original A')
    expect(wrapper.get('[data-testid="served-group"]').text()).toBe('—')
    expect(wrapper.get('[data-testid="usage-failover"]').text()).toBe('—')
    expect(wrapper.get('[data-testid="attempt-count"]').text()).toBe('—')
    expect(wrapper.get('[data-testid="recorded-credit"]').text()).toBe('38.50000000 Credit')
    expect(wrapper.find('[data-testid="missing-snapshot"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="locked-price-items"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('secret-account')
    expect(wrapper.text()).not.toContain('secret-upstream-id')
  })
  it('shows served B, HA and send count plus the server locked values, never the live group rate', () => {
    const wrapper = mount(UsageBillingDetails, { props: { record: { ...oldRecord, actual_cost: 1.2, requested_group_id: 1, served_group_id: 2, failover: 'ha', attempt_count: 3, pricing_snapshot: snapshot } } })
    expect(wrapper.get('[data-testid="requested-group"]').text()).toBe('#1')
    expect(wrapper.get('[data-testid="served-group"]').text()).toBe('#2')
    expect(wrapper.get('[data-testid="usage-failover"]').text()).toBe('usage.failoverKinds.ha')
    expect(wrapper.get('[data-testid="attempt-count"]').text()).toBe('3')
    expect(wrapper.get('[data-testid="locked-multiplier"]').text()).toBe('1.2')
    expect(wrapper.get('[data-testid="locked-credit"]').text()).toBe('1.20000000 Credit')
    expect(wrapper.get('[data-testid="source-currency"]').text()).toBe('CNY')
    expect(wrapper.get('[data-testid="locked-price-items"]').text()).toContain('2.00 Credit')
    expect(wrapper.text()).toContain('version-locked')
    expect(wrapper.text()).not.toContain('999')
    expect(wrapper.find('[data-testid="snapshot-conflict"]').exists()).toBe(false)
  })
  it('retains a stored zero multiplier/price/debit and uses snapshot served group only as a stored fact', () => {
    const wrapper = mount(UsageBillingDetails, { props: { record: { ...oldRecord, actual_cost: 0, served_group_id: null, pricing_snapshot: { ...snapshot, group_multiplier: 0, total_credit: 0, items: [{ kind: 'input', base_credit_price: 0, usage: 0, normalized_usage: 0, subtotal: 0 }] } } } })
    expect(wrapper.get('[data-testid="served-group"]').text()).toContain('#2')
    expect(wrapper.get('[data-testid="served-group"]').text()).toContain('usage.fromSnapshot')
    expect(wrapper.get('[data-testid="locked-multiplier"]').text()).toBe('0')
    expect(wrapper.get('[data-testid="locked-credit"]').text()).toBe('0.00000000 Credit')
    expect(wrapper.get('[data-testid="locked-price-items"]').text()).toContain('0.00 Credit')
  })
  it('shows missing saved price as unknown, not free or a current-rate replacement', () => {
    const wrapper = mount(UsageBillingDetails, { props: { record: { ...oldRecord, pricing_snapshot: { v: 2, quota_unit: 'credits', items: [{ kind: 'input' }] } } } })
    expect(wrapper.get('[data-testid="locked-multiplier"]').text()).toBe('—')
    expect(wrapper.get('[data-testid="locked-credit"]').text()).toBe('—')
    expect(wrapper.findAll('[data-testid="locked-price-items"] td').map((cell) => cell.text())).toEqual(['—', '—', '—', '—'])
    expect(wrapper.get('[data-testid="served-group"]').text()).toBe('—')
  })
  it('reports contradictions while retaining both stored values without rebilling', () => {
    const wrapper = mount(UsageBillingDetails, { props: { record: { ...oldRecord, served_group_id: 3, actual_cost: 38.5, pricing_snapshot: snapshot } } })
    expect(wrapper.get('[data-testid="snapshot-conflict"]').attributes('role')).toBe('alert')
    expect(wrapper.get('[data-testid="served-group"]').text()).toBe('#3')
    expect(wrapper.get('[data-testid="locked-credit"]').text()).toBe('1.20000000 Credit')
    expect(wrapper.get('[data-testid="recorded-credit"]').text()).toBe('38.50000000 Credit')
  })
})
