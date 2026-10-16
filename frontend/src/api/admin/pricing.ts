import { apiClient } from '../client'
import type { DeepSeekHolidayCalendar } from '@/utils/deepseekCalendar'

export interface PricingControls {
  remote_enabled: boolean
  native_cny_enabled: boolean
  // Omission preserves the latest server calendar; {} explicitly clears it.
  deepseek_holidays?: DeepSeekHolidayCalendar
  // PRD-01/K-6: admin-configurable pricing sync period in hours. Omission keeps
  // the stored value; 0 is rejected because a real period is at least one hour.
  update_interval_hours?: number
}
export interface PricingStatus extends PricingControls {
	deepseek_calendar_years?: number[]
	deepseek_calendar_error?: string
	usd_error?: string
  // Optional for an older backend: absence is unsupported/unknown, never false.
  deepseek_calendar_year_missing?: boolean
  // The period actually in effect (stored override, else the config default).
  effective_interval_hours?: number
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

export async function updateDeepseekHolidays(dates: string[]): Promise<PricingStatus> {
  return (await apiClient.put<PricingStatus>(`${path}/holidays`, { dates })).data
}
