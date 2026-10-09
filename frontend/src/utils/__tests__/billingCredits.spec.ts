import { afterEach, describe, expect, it, vi } from 'vitest'
vi.mock('@/i18n', () => ({ getLocale: () => 'zh-CN' }))
import { configureBillingCredits, formatBillingAmount } from '../billingCredits'
import { formatCreditScaled, formatScaled } from '../pricing'

afterEach(() => configureBillingCredits(false))
describe('native currency credit display', () => {
  it('preserves tiny debits and uses the same credit amount without FX', () => {
    configureBillingCredits(true)
    expect(formatBillingAmount('0.00000123')).toBe('0.00000123 额度')
    expect(formatBillingAmount(100)).toBe('100.00 额度')
    expect(formatCreditScaled(2e-6, 1e6, 2)).toBe('2.00 额度')
    expect(formatScaled(2e-6, 1e6, 2, 'CNY')).toBe('¥2.00')
    expect(formatScaled(2e-6, 1e6, 2, 'USD')).toBe('$2.00')
  })
  it('retains legacy USD display when the feature is disabled', () => {
    expect(formatBillingAmount(100)).toBe('$100.00')
    expect(formatCreditScaled(2e-6, 1e6, 2)).toBe('$2.00')
  })
})
