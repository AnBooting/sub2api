import { apiClient } from '../client'

/**
 * 管理端「商品同步状态」契约。
 *
 * GET /admin/shop/sync-status → { code, message, data }
 *
 * data 字段（稳定错误码 + 本站时间戳，绝不含凭据、上游域名或上游原始响应）：
 *   - last_success_at：最近一次**成功**同步时间（RFC3339/UTC）；null = 从未成功同步过
 *   - last_attempt_at：最近一次同步尝试（成功或失败）；null = 从未尝试
 *   - error：稳定错误码；空串表示当前无错误
 *   - paused：是否暂停新建订单（未配置 / 从未成功同步 / 最近同步失败 / 当前无商品）
 *   - product_count：当前展示的商品数（同步失败时保留上一次成功的数据）
 *
 * 该接口在未配置/上游不可用时同样返回 200 + 明确状态，不会返回 500。
 */
export interface AdminShopSyncStatus {
  last_success_at: string | null
  last_attempt_at: string | null
  error: string
  paused: boolean
  product_count: number
}

/** 读取当前商品同步状态（只读）。 */
export async function getShopSyncStatus(): Promise<AdminShopSyncStatus> {
  return (await apiClient.get<AdminShopSyncStatus>('/admin/shop/sync-status')).data
}

export default { getShopSyncStatus }
