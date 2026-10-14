import { afterEach, describe, expect, it, vi } from 'vitest'
vi.mock('@/i18n', () => ({ getLocale: () => 'en-US', i18n: { global: { t: (key: string) => key } } }))
import { accountStatsAmount, accountStatsExportValue } from '../accountStatsDisplay'
import { configureBillingCredits } from '../billingCredits'
import { formatCurrency } from '../format'
afterEach(() => configureBillingCredits(false))
describe('account statistic numbers are not evidence of a supplier currency', () => {
  it.each([false, true])('retains scalar/formula/zero numbers independently of native-CNY mode=%s', (mode) => {
    configureBillingCredits(mode)
    expect(accountStatsAmount({ total_cost: 10, account_rate_multiplier: 1.5 })).toBe(15)
    expect(accountStatsExportValue({ total_cost: 10, account_rate_multiplier: 1.5 })).toBe('15.000000')
    expect(accountStatsExportValue({ total_cost: 100, account_stats_cost: 2, account_rate_multiplier: 1.5 })).toBe('3.000000')
    expect(accountStatsExportValue({ total_cost: 100, account_stats_cost: 0, account_rate_multiplier: 1.5 })).toBe('0.000000')
    expect(accountStatsExportValue({ total_cost: null, account_stats_cost: null })).toBe('')
    // Known-currency money is still formatted by explicit monetary context, not
    // the usage DTO or Customer price snapshot. No source amounts are converted.
    expect(formatCurrency(0, 'USD')).toBe('$0.00')
    expect(formatCurrency(100, 'CNY')).toContain('¥100.00')
  })
  it('keeps the legacy USD-era numeric formula compatible without inventing USD evidence in its untyped DTO', () => {
    expect(accountStatsAmount({ total_cost: 2, account_rate_multiplier: 1.2 })).toBe(2.4)
    expect(accountStatsExportValue({ total_cost: 2, account_rate_multiplier: 1.2 })).toBe('2.400000')
  })
  it('does not turn unknown/non-finite account statistics into zero', () => {
    expect(accountStatsAmount({})).toBeNull()
    expect(accountStatsAmount({ account_stats_cost: Infinity })).toBeNull()
    expect(accountStatsExportValue({ account_stats_cost: Number.NaN })).toBe('')
  })
})
