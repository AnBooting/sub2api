import { apiClient } from '../client'

export interface PricingControls {
  remote_enabled: boolean
  native_cny_enabled: boolean
}
export interface PricingStatus extends PricingControls {
	deepseek_holidays?: string[]
	deepseek_calendar_years?: number[]
	deepseek_calendar_error?: string
	usd_error?: string
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
