import { ref } from 'vue'
import { getLocale } from '@/i18n'

/** Customer ledger units never follow source-currency mode or apply FX. */
export const CUSTOMER_CREDIT_UNIT = 'Credit'
export function formatCustomerCredit(amount: number | string | null | undefined, digits?: number): string {
  if (amount == null || (typeof amount === 'string' && amount.trim() === '')) return '—'
  const value = Number(amount)
  if (!Number.isFinite(value)) return '—'
  const precision = digits ?? (typeof amount === 'string' && amount.includes('.')
    ? Math.min(12, amount.split('.')[1]!.length)
    : value !== 0 && Math.abs(value) < 0.01 ? Math.max(6, Math.min(12, Math.ceil(-Math.log10(Math.abs(value))) + 2)) : 2)
  return value.toFixed(precision) + ' ' + CUSTOMER_CREDIT_UNIT
}
export function customerCreditHeader(label: string): string { return label + ' (' + CUSTOMER_CREDIT_UNIT + ')' }

// Compatibility for legacy payment/pricing consumers. Customer ledger UIs use
// formatCustomerCredit explicitly, so this source-currency feature flag cannot relabel them.
export const billingCreditsEnabled = ref(false)
export function configureBillingCredits(enabled: boolean): void { billingCreditsEnabled.value = enabled }

export function formatBillingAmount(amount: number | string | null | undefined, digits?: number): string {
  const value = Number(amount ?? 0)
  const precision = digits ?? (typeof amount === 'string' && amount.includes('.') ? Math.min(12, amount.split('.')[1]!.length) : 2)
  const number = Number.isFinite(value) ? value.toFixed(precision) : (0).toFixed(precision)
  return billingCreditsEnabled.value ? `${number} ${getLocale().startsWith('zh') ? '额度' : 'credits'}` : `$${number}`
}
