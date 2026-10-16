import { describe, expect, it } from 'vitest'
import { normalGroupEntries, normalGroupIds, replaceNormalGroups, isGroupChainConflict } from '../keyGroupChain'

describe('ordered key chain contract', () => {
  it('recognizes normalized and axios chain conflicts without throwing on non-errors', () => {
    expect(isGroupChainConflict({ status: 409, code: 'group_chain_conflict' })).toBe(true)
    expect(isGroupChainConflict({ response: { status: 409 } })).toBe(true)
    expect(isGroupChainConflict(null)).toBe(false)
    expect(isGroupChainConflict({ status: 403 })).toBe(false)
  })
  it('reads legacy single and unbound keys without inventing groups', () => {
    expect(normalGroupIds({ group_id: 7 })).toEqual([7])
    expect(normalGroupIds({ group_id: null })).toEqual([])
    expect(normalGroupIds({ group_id: 7, groups: [] })).toEqual([])
  })
  it('uses role-local saved positions rather than array or legacy order', () => {
    expect(normalGroupIds({ group_id: 99, groups: [
      { group_id: 2, role: 'normal', position: 1 },
      { group_id: 8, role: 'ha', position: 0 },
      { group_id: 1, role: 'normal', position: 0 },
    ] })).toEqual([1, 2])
  })
  it('creates contiguous unique normal entries and does not synthesize HA', () => {
    expect(normalGroupEntries([2, 1, 2])).toEqual([
      { group_id: 2, role: 'normal', position: 0 },
      { group_id: 1, role: 'normal', position: 1 },
    ])
  })
  it('preserves each existing HA identity/position but never sends the consent timestamp', () => {
    expect(replaceNormalGroups([2, 1], [{ group_id: 8, role: 'ha', position: 0, enabled_by_user_at: '2026-01-01' }])).toEqual([
      { group_id: 2, role: 'normal', position: 0 },
      { group_id: 1, role: 'normal', position: 1 },
      { group_id: 8, role: 'ha', position: 0 },
    ])
    expect(replaceNormalGroups([])).toEqual([])
  })
})
