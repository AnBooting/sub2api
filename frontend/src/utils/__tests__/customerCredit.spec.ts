import { afterEach, describe, expect, it, vi } from 'vitest'
vi.mock('@/i18n', () => ({ getLocale: () => 'en-US', i18n: { global: { t: (key: string) => key } } }))
import { configureBillingCredits, customerCreditHeader, formatCustomerCredit } from '../billingCredits'
import { formatCurrency } from '../format'
import { formatScaled } from '../pricing'
import { formatCustomerTokenPricePerMillion } from '../usagePricing'
afterEach(() => configureBillingCredits(false))
describe('explicit Customer Credit ledger display', () => {
  it.each([false, true])('does not change historical numeric values or units when native-CNY mode=%s', (mode) => {
    configureBillingCredits(mode)
    expect(formatCustomerCredit(100)).toBe('100.00 Credit')
    expect(formatCustomerCredit(38.5)).toBe('38.50 Credit')
    expect(formatCustomerCredit(0)).toBe('0.00 Credit')
    expect(formatCustomerCredit('0.00000123')).toBe('0.00000123 Credit')
    expect(formatCustomerCredit(0.00000002)).toBe('0.0000000200 Credit')
    expect(formatCurrency(2, 'USD')).toBe('$2.00')
    expect(formatCurrency(2)).toBe('$2.00')
    expect(formatCurrency(100, 'CNY')).toContain('¥100.00')
    expect(formatScaled(2e-6, 1e6, 2, 'CNY')).toBe('¥2.00')
    expect(formatScaled(2e-6, 1e6, 2, 'USD')).toBe('$2.00')
  })
  it.each([undefined, null, '', 'invalid', Number.NaN, Infinity])('never turns unknown price %s into a fabricated zero', (price) => {
    expect(formatCustomerCredit(price)).toBe('—')
  })
  it('labels exports and recorded-cost unit rates without any new billing calculation', () => {
    expect(customerCreditHeader('Billed Cost')).toBe('Billed Cost (Credit)')
    expect(formatCustomerTokenPricePerMillion(0, 500000)).toBe('0.0000 Credit')
    expect(formatCustomerTokenPricePerMillion(null, 500000)).toBe('—')
  })
})
