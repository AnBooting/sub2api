/** Existing account-statistics formula, not a supplier invoice or FX conversion.
 * The usage DTO has no supplier-currency/provenance field. A scalar can itself
 * originate from Customer pricing, so neither its presence nor a Customer price
 * snapshot/native-CNY flag is evidence that this statistic is USD (or CNY).
 */
export interface AccountStatsRecord {
  total_cost?: number | null
  account_stats_cost?: number | null
  account_rate_multiplier?: number | null
}
export function accountStatsAmount(record: AccountStatsRecord): number | null {
  const base = record.account_stats_cost ?? record.total_cost
  if (base == null) return null
  const result = base * (record.account_rate_multiplier ?? 1)
  return Number.isFinite(result) ? result : null
}
export function accountStatsExportValue(record: AccountStatsRecord, digits = 6): string {
  const amount = accountStatsAmount(record)
  return amount == null ? '' : amount.toFixed(digits)
}
