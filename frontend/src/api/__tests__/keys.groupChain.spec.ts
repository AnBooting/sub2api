import { beforeEach, describe, expect, it, vi } from 'vitest'
import { create, update, bulkUpdate } from '../keys'
import { updateApiKeyGroup, updateApiKeyGroups } from '../admin/apiKeys'
const { get, put, post } = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), post: vi.fn() }))
vi.mock('../client', () => ({ apiClient: { get, put, post } }))
beforeEach(() => {
  vi.resetAllMocks()
  put.mockResolvedValue({ data: {} })
  post.mockResolvedValue({ data: {} })
})
const groups = [{ group_id: 2, role: 'normal' as const, position: 0 }, { group_id: 1, role: 'normal' as const, position: 1 }]
describe('key chain wire compatibility', () => {
  it('creates ordered groups without also sending conflicting group_id', async () => {
    await create('chain', 99, undefined, undefined, undefined, undefined, undefined, undefined, groups)
    expect(post).toHaveBeenCalledWith('/keys', { name: 'chain', groups })
  })
  it('keeps legacy single, NULL and omitted group payloads unchanged', async () => {
    await create('single', 7)
    await create('unbound', null)
    await create('default')
    expect(post.mock.calls.map((call) => call[1])).toEqual([{ name: 'single', group_id: 7 }, { name: 'unbound', group_id: null }, { name: 'default' }])
    await update(1, { group_id: null })
    expect(put).toHaveBeenCalledWith('/keys/1', { group_id: null })
    await updateApiKeyGroup(1, null)
    expect(put).toHaveBeenCalledWith('/admin/api-keys/1', { group_id: 0 })
  })
  it('bulk reads each HA chain and never writes on a failed read', async () => {
    const failure = new Error('failed read')
    get.mockImplementation((url: string) => url === '/keys/3' ? Promise.reject(failure) : Promise.resolve({ data: { group_chain_revision: url === '/keys/1' ? 0 : 4, groups: [{ group_id: url === '/keys/1' ? 8 : 9, role: 'ha', position: 0, enabled_by_user_at: 'read-only' }] } }))
    const result = await bulkUpdate([1, 2, 3], { groups })
    expect(put.mock.calls).toEqual([
      ['/keys/1', { groups: [...groups, { group_id: 8, role: 'ha', position: 0 }], expected_group_chain_revision: 0 }],
      ['/keys/2', { groups: [...groups, { group_id: 9, role: 'ha', position: 0 }], expected_group_chain_revision: 4 }],
    ])
    expect(result).toEqual({ succeededIds: [1, 2], failures: [{ id: 3, error: failure }] })
  })
  it('fails a racing bulk key once without rereading or signing a newer revision', async () => {
    const conflict = { status: 409, code: 'group_chain_conflict' }
    get.mockResolvedValue({ data: { group_chain_revision: 3, groups: [] } })
    put.mockRejectedValue(conflict)
    expect(await bulkUpdate([1], { groups })).toEqual({ succeededIds: [], failures: [{ id: 1, error: conflict }] })
    expect(get).toHaveBeenCalledTimes(1)
    expect(put).toHaveBeenCalledTimes(1)
    expect(put).toHaveBeenCalledWith('/keys/1', { groups, expected_group_chain_revision: 3 })
  })
  it('admin ordinary chain sends no HA or consent time, including unbind', async () => {
    await updateApiKeyGroups(4, groups, 0)
    await updateApiKeyGroups(4, [], 5)
    expect(put.mock.calls).toEqual([['/admin/api-keys/4', { groups, expected_group_chain_revision: 0 }], ['/admin/api-keys/4', { groups: [], expected_group_chain_revision: 5 }]])
  })
})
