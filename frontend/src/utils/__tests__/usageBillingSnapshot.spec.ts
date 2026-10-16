import { describe, expect, it } from 'vitest'
import type { UsageLog } from '@/types'
import { storedCreditSnapshot, storedServedGroupId, usageSnapshotConflicts, usageRoutingExport, storedValue } from '../usageBillingSnapshot'
const record = (overrides: Partial<UsageLog> = {}) => ({ group_id: 1, actual_cost: 1.2, ...overrides }) as UsageLog
const snapshot = { v: 2 as const, quota_unit: 'credits' as const, served_group_id: 2, group_multiplier: 1.2, total_credit: 1.2 }
describe('stored snapshot and nullable routing facts', () => {
  it.each([null, undefined, { v: 1 as const, currency: 'USD', input_per_token: 0.1 }])('does not invent routing or lock prices for historical snapshot %s', (pricing_snapshot) => {
    const old = record({ pricing_snapshot })
    expect(storedCreditSnapshot(old)).toBeNull()
    expect(storedServedGroupId(old)).toBeNull()
    expect(usageRoutingExport(old)).toEqual(['', '', '', ''])
  })
  it('uses only stored served group facts, never the legacy group ID', () => {
    expect(storedServedGroupId(record({ pricing_snapshot: snapshot }))).toBe(2)
    expect(storedServedGroupId(record({ served_group_id: 3, pricing_snapshot: snapshot }))).toBe(3)
    expect(usageSnapshotConflicts(record({ served_group_id: 3, actual_cost: 7, pricing_snapshot: snapshot }))).toEqual(['served_group', 'total_credit'])
  })
  it('keeps explicit zeros and HA/attempts for export without truthy fallbacks', () => {
    const current = record({ requested_group_id: 1, served_group_id: 2, failover: 'ha', attempt_count: 3, actual_cost: 0, pricing_snapshot: { ...snapshot, group_multiplier: 0, total_credit: 0 } })
    expect(usageRoutingExport(current)).toEqual([1, 2, 'ha', 3])
    expect(usageSnapshotConflicts(current)).toEqual([])
    expect(storedValue(0)).toBe('0')
    expect(storedValue(null)).toBe('—')
  })
  it('refuses to interpret non-Credit v2 data as a customer Credit price', () => {
    const invalid = record({ pricing_snapshot: { ...snapshot, quota_unit: 'USD' } as unknown as UsageLog['pricing_snapshot'] })
    expect(storedCreditSnapshot(invalid)).toBeNull()
  })
})
