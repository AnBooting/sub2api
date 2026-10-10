import { apiClient } from '../client'
import type { DomesticProviderId } from '@/utils/domesticProviders'

export interface DomesticPricingCatalogEntry {
  provider: DomesticProviderId
  model: string
  input_price_per_token: number | null
  output_price_per_token: number | null
  cache_read_price_per_token?: number | null
  cache_write_price_per_token?: number | null
  currency?: 'USD'
  source: string
  version: number
  updated_at: string
  status: 'active' | 'pending_pricing'
  override?: boolean
  peak_rate_multiplier?: number
  peak_schedule?: string
}

export interface DomesticPricingCatalogResponse {
  items: DomesticPricingCatalogEntry[]
  currency: 'USD'
  unit: 'token'
  display_unit: 'million_tokens'
}

export function catalogItems(
  data: DomesticPricingCatalogEntry[] | DomesticPricingCatalogResponse
): DomesticPricingCatalogEntry[] {
  if (Array.isArray(data)) return data
  return Array.isArray(data?.items) ? data.items : []
}

export async function getCatalog(): Promise<DomesticPricingCatalogEntry[]> {
  const { data } = await apiClient.get<DomesticPricingCatalogResponse>('/admin/domestic-pricing/catalog')
  return catalogItems(data)
}

export default { getCatalog }
