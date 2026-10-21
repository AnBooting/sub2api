/**
 * API Keys management endpoints
 * Handles CRUD operations for user API keys
 */

import { apiClient } from './client'
import { replaceNormalGroups } from '@/utils/keyGroupChain'
import type { ApiKey, ApiKeyGroupInput, CreateApiKeyRequest, UpdateApiKeyRequest, PaginatedResponse } from '@/types'

/**
 * List all API keys for current user
 * @param page - Page number (default: 1)
 * @param pageSize - Items per page (default: 10)
 * @param filters - Optional filter parameters
 * @param options - Optional request options
 * @returns Paginated list of API keys
 */
export async function list(
  page: number = 1,
  pageSize: number = 10,
  filters?: {
    search?: string
    status?: string
    group_id?: number | string
    sort_by?: string
    sort_order?: 'asc' | 'desc'
  },
  options?: {
    signal?: AbortSignal
  }
): Promise<PaginatedResponse<ApiKey>> {
  const { data } = await apiClient.get<PaginatedResponse<ApiKey>>('/keys', {
    params: { page, page_size: pageSize, ...filters },
    signal: options?.signal
  })
  return data
}

/**
 * Get API key by ID
 * @param id - API key ID
 * @returns API key details
 */
export async function getById(id: number): Promise<ApiKey> {
  const { data } = await apiClient.get<ApiKey>(`/keys/${id}`)
  return data
}

/**
 * Create new API key
 * @param name - Key name
 * @param groupId - Optional group ID
 * @param customKey - Optional custom key value
 * @param ipWhitelist - Optional IP whitelist
 * @param ipBlacklist - Optional IP blacklist
 * @param quota - Optional quota limit in USD (0 = unlimited)
 * @param expiresInDays - Optional days until expiry (undefined = never expires)
 * @param rateLimitData - Optional rate limit fields
 * @returns Created API key
 */
export async function create(
  name: string,
  groupId?: number | null,
  customKey?: string,
  ipWhitelist?: string[],
  ipBlacklist?: string[],
  quota?: number,
  expiresInDays?: number,
  rateLimitData?: { rate_limit_5h?: number; rate_limit_1d?: number; rate_limit_7d?: number },
  groups?: ApiKeyGroupInput[]
): Promise<ApiKey> {
  const payload: CreateApiKeyRequest = { name }
  if (groups !== undefined) {
    payload.groups = groups
  } else if (groupId !== undefined) {
    payload.group_id = groupId
  }
  if (customKey) {
    payload.custom_key = customKey
  }
  if (ipWhitelist && ipWhitelist.length > 0) {
    payload.ip_whitelist = ipWhitelist
  }
  if (ipBlacklist && ipBlacklist.length > 0) {
    payload.ip_blacklist = ipBlacklist
  }
  if (quota !== undefined && quota > 0) {
    payload.quota = quota
  }
  if (expiresInDays !== undefined && expiresInDays > 0) {
    payload.expires_in_days = expiresInDays
  }
  if (rateLimitData?.rate_limit_5h && rateLimitData.rate_limit_5h > 0) {
    payload.rate_limit_5h = rateLimitData.rate_limit_5h
  }
  if (rateLimitData?.rate_limit_1d && rateLimitData.rate_limit_1d > 0) {
    payload.rate_limit_1d = rateLimitData.rate_limit_1d
  }
  if (rateLimitData?.rate_limit_7d && rateLimitData.rate_limit_7d > 0) {
    payload.rate_limit_7d = rateLimitData.rate_limit_7d
  }

  const { data } = await apiClient.post<ApiKey>('/keys', payload)
  return data
}

/**
 * Update API key
 * @param id - API key ID
 * @param updates - Fields to update
 * @returns Updated API key
 */
export async function update(id: number, updates: UpdateApiKeyRequest): Promise<ApiKey> {
  const { data } = await apiClient.put<ApiKey>(`/keys/${id}`, updates)
  return data
}

export interface BulkUpdateApiKeysResult {
  succeededIds: number[]
  failures: Array<{ id: number; error: unknown }>
}

/** Reuse per-key validation and permissions, with at most five requests in flight. */
export async function bulkUpdate(
  ids: number[],
  updates: UpdateApiKeyRequest
): Promise<BulkUpdateApiKeysResult> {
  const uniqueIds = [...new Set(ids)]
  const result: BulkUpdateApiKeysResult = { succeededIds: [], failures: [] }
  for (let offset = 0; offset < uniqueIds.length; offset += 5) {
    const batch = uniqueIds.slice(offset, offset + 5)
    const responses = await Promise.allSettled(batch.map(async (id) => {
      // The user endpoint replaces the whole chain. Read each key so bulk normal edits
      // preserve that key's own HA consent; a failed read must not result in a write.
      if (updates.groups !== undefined && updates.groups.every((entry) => entry.role === 'normal')) {
        const current = await getById(id)
        return update(id, { ...updates, groups: replaceNormalGroups(updates.groups.map((entry) => entry.group_id), current.groups), expected_group_chain_revision: current.group_chain_revision })
      }
      return update(id, updates)
    }))
    responses.forEach((response, index) => {
      if (response.status === 'fulfilled') {
        result.succeededIds.push(batch[index])
      } else {
        result.failures.push({ id: batch[index], error: response.reason })
      }
    })
  }
  return result
}

/** One admin-designated HA fallback group a user may opt a key into (PRD-04). */
export interface KeyHAGroupCandidate {
  group_id: number
  name: string
  platform: string
  rate_multiplier: number
  enabled: boolean
  enabled_by_user_at?: string
  scope_compatible: boolean
  /**
   * Whether the user currently holds the access this group needs to actually serve
   * (an exclusive group needs an explicit grant; a public group needs none). The
   * server enforces the same condition on write, so this mirrors what will really
   * happen rather than advertising an opt-in the server would reject (PRD-04 FE-02).
   */
  access_granted: boolean
}

/**
 * List the HA fallback groups this key may opt into, with the user's current consent.
 *
 * The list is filtered only by the admin fallback flag and group activity — not by
 * ordinary group bind permission — because PRD-04/K-3 makes the fallback independent
 * of normal group access.
 * @param id - API key ID
 */
export async function listKeyHAGroups(id: number): Promise<KeyHAGroupCandidate[]> {
  const { data } = await apiClient.get<KeyHAGroupCandidate[]>(`/keys/${id}/ha-groups`)
  return data
}

/**
 * Opt this key in or out of an admin-designated high-availability fallback group.
 *
 * PRD-04/TS-04: the admin marks which groups may serve as a fallback; the user
 * consents per key here. The server owns the multiplier, so no rate is sent.
 * @param id - API key ID
 * @param groupID - fallback group ID
 * @param enabled - true to consent, false to withdraw
 */
export async function setKeyHAGroup(
  id: number,
  groupID: number,
  enabled: boolean
): Promise<{ enabled: boolean; rate_multiplier: number; group_chain_revision: number }> {
  const { data } = await apiClient.put<{ enabled: boolean; rate_multiplier: number; group_chain_revision: number }>(
    `/keys/${id}/ha-groups/${groupID}`,
    { enabled }
  )
  return data
}

/**
 * 为**新建**的 Key 补开高可用兜底授权。
 *
 * 授权对象是具体的 Key，因此只能在 Key 建好之后逐个开通。
 * 服务端会重新校验分组标记与模型范围，所以失败项单独返回，由调用方提示用户。
 * @param id - API key ID
 * @param groupIDs - 需要开启兜底的分组 ID
 */
export async function enableKeyHAGroups(
  id: number,
  groupIDs: number[]
): Promise<{ enabled: number[]; failed: number[] }> {
  const enabled: number[] = []
  const failed: number[] = []
  for (const groupID of [...new Set(groupIDs)]) {
    try {
      await setKeyHAGroup(id, groupID, true)
      enabled.push(groupID)
    } catch {
      failed.push(groupID)
    }
  }
  return { enabled, failed }
}

/**
 * Delete API key
 * @param id - API key ID
 * @returns Success confirmation
 */
export async function deleteKey(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(`/keys/${id}`)
  return data
}

/**
 * Toggle API key status (active/inactive)
 * @param id - API key ID
 * @param status - New status
 * @returns Updated API key
 */
export async function toggleStatus(id: number, status: 'active' | 'inactive'): Promise<ApiKey> {
  return update(id, { status })
}

export const keysAPI = {
  list,
  getById,
  create,
  update,
  bulkUpdate,
  enableKeyHAGroups,
  delete: deleteKey,
  toggleStatus
}

export default keysAPI
