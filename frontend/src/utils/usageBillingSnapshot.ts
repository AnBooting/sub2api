import type { PricingSnapshotV2, UsageLog } from '@/types'

type BillingRecord = Pick<UsageLog, 'pricing_snapshot' | 'served_group_id' | 'actual_cost'>

// Read stored facts only. Never use current group rates/prices or infer a new C6
// value from the historical group_id field.
export function storedCreditSnapshot(record: Pick<UsageLog, 'pricing_snapshot'>): PricingSnapshotV2 | null {
  const snapshot = record.pricing_snapshot
  return snapshot?.v === 2 && snapshot.quota_unit === 'credits' ? snapshot : null
}
export function storedServedGroupId(record: Pick<UsageLog, 'pricing_snapshot' | 'served_group_id'>): number | null {
  return record.served_group_id ?? storedCreditSnapshot(record)?.served_group_id ?? null
}
export function storedValue(value: number | string | null | undefined): string {
  if (value == null || (typeof value === 'string' && value.trim() === '')) return '—'
  return String(value)
}
function finiteNumber(value: number | string | null | undefined): number | null {
  if (value == null || (typeof value === 'string' && value.trim() === '')) return null
  const number = Number(value)
  return Number.isFinite(number) ? number : null
}
export function usageSnapshotConflicts(record: BillingRecord): Array<'served_group' | 'total_credit'> {
  const snapshot = storedCreditSnapshot(record)
  if (!snapshot) return []
  const conflicts: Array<'served_group' | 'total_credit'> = []
  if (record.served_group_id != null && snapshot.served_group_id != null && record.served_group_id !== snapshot.served_group_id) conflicts.push('served_group')
  const total = finiteNumber(snapshot.total_credit)
  const debit = finiteNumber(record.actual_cost)
  if (total != null && debit != null && Math.abs(total - debit) > 1e-8) conflicts.push('total_credit')
  return conflicts
}
export function usageRoutingExport(record: Pick<UsageLog, 'requested_group_id' | 'served_group_id' | 'pricing_snapshot' | 'failover' | 'attempt_count'>): Array<number | string> {
  return [record.requested_group_id ?? '', storedServedGroupId(record) ?? '', record.failover ?? '', record.attempt_count ?? '']
}
