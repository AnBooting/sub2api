/**
 * 「用量统计尚未完成对账」（N-02）的统一判定 —— 前端单一事实源。
 *
 * 后端契约：`ErrUsageStatsIncomplete` → **HTTP 503 + reason=usage_stats_incomplete**。
 * 覆盖三处同源接口：
 *   - 用户端 `/usage/dashboard/trend`、`/usage/dashboard/models`
 *   - 管理端 `/admin/dashboard/trend`、`/admin/groups/usage-summary`
 *   - snapshot-v2 的 trend/models 分区（部分可用时走 200 + unavailable_sections，不在此列）
 *
 * 与 snapshot-v2 的 `unavailable_sections` 语义不同，不能混用：
 *   - snapshot 是**部分可用**（同一响应里其它区块仍然可信）；
 *   - 本状态是**整体不可用**（该接口这次没有可信数据），因此用错误响应表达。
 *
 * 前端处置：显示「统计不完整，暂不显示该图表」，**不要**画空图表，**也不要**当红色故障。
 */

export const USAGE_STATS_INCOMPLETE_CODE = 'USAGE_STATS_INCOMPLETE'

export interface UsageStatsIncompleteLike {
  status?: number | null
  code?: number | string | null
  reason?: string | null
}

/**
 * 这次失败是不是「统计未就绪」。
 *
 * 优先认 client.ts 收敛出来的 `USAGE_STATS_INCOMPLETE` code；
 * 同时兼容 reason 原文，避免绕过拦截器的调用点（如测试直连）漏判。
 */
export function isUsageStatsIncomplete(err: unknown): boolean {
  if (!err || typeof err !== 'object') return false
  const e = err as UsageStatsIncompleteLike
  if (e.code === USAGE_STATS_INCOMPLETE_CODE) return true
  return e.reason === 'usage_stats_incomplete'
}
