import type { ApiKey, ApiKeyGroup, ApiKeyGroupInput } from '@/types'

// An explicit empty chain is authoritative; legacy group_id is used only if groups is absent.
export function normalGroupIds(key: Pick<ApiKey, 'group_id' | 'groups'>): number[] {
  if (key.groups !== undefined) {
    return key.groups.filter((entry) => entry.role === 'normal')
      .slice().sort((a, b) => a.position - b.position).map((entry) => entry.group_id)
  }
  return key.group_id ? [key.group_id] : []
}

export function isGroupChainConflict(error: unknown): boolean {
  if (!error || typeof error !== 'object') return false
  const value = error as { status?: number; code?: string | number; reason?: string; response?: { status?: number; data?: { code?: string } } }
  return value.status === 409 || value.response?.status === 409 || value.code === 'group_chain_conflict' || value.reason === 'group_chain_conflict' || value.response?.data?.code === 'group_chain_conflict'
}

export function normalGroupEntries(ids: number[]): ApiKeyGroupInput[] {
  return [...new Set(ids)].map((group_id, position) => ({ group_id, role: 'normal', position }))
}

export function replaceNormalGroups(ids: number[], current?: ApiKeyGroup[]): ApiKeyGroupInput[] {
  return [...normalGroupEntries(ids), ...(current ?? []).filter((entry) => entry.role === 'ha').map(({ group_id, role, position }) => ({ group_id, role, position }))]
}
