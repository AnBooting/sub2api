import { apiClient } from '../client'
import type { DeepSeekHolidayCalendar } from '@/utils/deepseekCalendar'

export interface PricingControls {
  remote_enabled: boolean
  native_cny_enabled: boolean
  // Omission preserves the latest server calendar; {} explicitly clears it.
  deepseek_holidays?: DeepSeekHolidayCalendar
}
export interface PricingStatus extends PricingControls {
  // Optional for an older backend: absence is unsupported/unknown, never false.
  deepseek_calendar_year_missing?: boolean
  usd_model_count: number
  usd_last_updated: string
  cny_model_count: number
  cny_last_updated: string
  cny_error: string
}
const path = '/admin/channels/pricing'
export async function getPricingControls(): Promise<PricingStatus> {
  return (await apiClient.get<PricingStatus>(`${path}/settings`)).data
}
export async function updatePricingControls(controls: PricingControls): Promise<PricingStatus> {
  return (await apiClient.put<PricingStatus>(`${path}/settings`, controls, { timeout: 65000 })).data
}
export async function refreshPricing(): Promise<PricingStatus> {
  return (await apiClient.post<PricingStatus>(`${path}/refresh`, undefined, { timeout: 65000 })).data
}
