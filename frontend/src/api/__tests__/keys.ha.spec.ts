import { beforeEach, describe, expect, it, vi } from 'vitest'

const { put } = vi.hoisted(() => ({ put: vi.fn() }))

vi.mock('@/api/client', () => ({
  apiClient: { put },
}))

import { enableKeyHAGroups } from '@/api/keys'

describe('enableKeyHAGroups', () => {
  beforeEach(() => {
    put.mockReset()
    put.mockResolvedValue({ data: { enabled: true, rate_multiplier: 1, group_chain_revision: 2 } })
  })

  it('enables every picked fallback group once', async () => {
    const result = await enableKeyHAGroups(42, [7, 9, 7])

    expect(put).toHaveBeenCalledTimes(2)
    expect(put).toHaveBeenNthCalledWith(1, '/keys/42/ha-groups/7', { enabled: true })
    expect(put).toHaveBeenNthCalledWith(2, '/keys/42/ha-groups/9', { enabled: true })
    expect(result).toEqual({ enabled: [7, 9], failed: [] })
  })

  it('reports the groups the server refused without failing the whole call', async () => {
    put.mockImplementation((url: string) => {
      if (url.endsWith('/ha-groups/9')) return Promise.reject(new Error('forbidden'))
      return Promise.resolve({ data: { enabled: true, rate_multiplier: 1, group_chain_revision: 2 } })
    })

    const result = await enableKeyHAGroups(42, [7, 9])

    expect(result).toEqual({ enabled: [7], failed: [9] })
  })

  it('does nothing when no fallback group was picked', async () => {
    const result = await enableKeyHAGroups(42, [])

    expect(put).not.toHaveBeenCalled()
    expect(result).toEqual({ enabled: [], failed: [] })
  })
})
