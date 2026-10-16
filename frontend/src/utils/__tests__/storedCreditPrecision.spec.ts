import { describe, expect, it, vi } from 'vitest'
vi.mock('@/i18n', () => ({ getLocale: () => 'en-US' }))
import { formatCustomerCredit, formatStoredCustomerCredit } from '../billingCredits'
describe('stored Credit values preserve the original decimal, not a rounded summary', () => {
  it.each([
    [1.23456789, '1.23456789 Credit'],
    [0.617283945, '0.617283945 Credit'],
    ['1.23456789123456789123456789', '1.23456789123456789123456789 Credit'],
    ['0.617283945000000000000001', '0.617283945000000000000001 Credit'],
    [0, '0 Credit'], ['0.00000000000000000000', '0.00000000000000000000 Credit'],
    [1.234e-9, '0.000000001234 Credit'], ['6.17e-10', '0.000000000617 Credit'],
    ['1.2345e2', '123.45 Credit'],
    ['1e-1000000', '1e-1000000 Credit'], ['1e1000000', '1e1000000 Credit'],
  ])('displays %s exactly as %s', (value, expected) => { expect(formatStoredCustomerCredit(value)).toBe(expected) })
  it.each([null, undefined, '', 'NaN', 'Infinity', 'invalid', Number.NaN, Infinity])('keeps unknown/invalid %s unknown, never free', (value) => { expect(formatStoredCustomerCredit(value)).toBe('—') })
  it('leaves ordinary customer balance summary precision unchanged', () => { expect(formatCustomerCredit(100)).toBe('100.00 Credit'); expect(formatCustomerCredit(38.5)).toBe('38.50 Credit') })
})
