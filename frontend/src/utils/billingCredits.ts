import { ref } from 'vue'
import { getLocale } from '@/i18n'

export const billingCreditsEnabled = ref(false)
export function configureBillingCredits(enabled: boolean): void { billingCreditsEnabled.value = enabled }

export function formatBillingAmount(amount: number | string | null | undefined, digits?: number): string {
  const value = Number(amount ?? 0)
  const precision = digits ?? (typeof amount === 'string' && amount.includes('.') ? Math.min(12, amount.split('.')[1]!.length) : 2)
  const number = Number.isFinite(value) ? value.toFixed(precision) : (0).toFixed(precision)
  return billingCreditsEnabled.value ? `${number} ${getLocale().startsWith('zh') ? '额度' : 'credits'}` : `$${number}`
}
