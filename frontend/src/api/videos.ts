/**
 * 用户侧视频任务 API。
 *
 * 契约（后端正在改成游标分页）：GET /api/v1/videos/tasks?limit=20&cursor=<cursor>
 *  - 响应 data：{ items: [...], next_cursor: string | null }
 *  - 每项：{ task_no, status, model, platform, billing_mode, created_at, settled_at, last_error, last_error_code }
 *  - last_error 是可公开的失败说明；last_error_code 是稳定错误码（界面按码 i18n）
 *  - status ∈ processing | completed | failed
 *  - billing_mode ∈ per_token（按量）/ per_request（按次）/ per_second（按秒）
 *  - 空列表返回空数组（不是 404）；未登录 401。
 *  - 接口只返回本人可见字段，绝不包含 upstream_task_id / account_id。
 *
 * 兼容：后端改造完成前接口仍可能直接返回数组，因此这里统一归一化成
 * { items, next_cursor }，页面不需要关心后端处于哪个阶段。
 */

import { apiClient } from './client'

export type VideoTaskStatus = 'processing' | 'completed' | 'failed'

export type VideoTaskBillingMode = 'per_token' | 'per_request' | 'per_second'

/**
 * 视频任务失败原因的用户侧错误码（稳定、可枚举、可翻译）。
 *
 * 后端只给「错误码 + 可公开说明」，不把内部诊断（承载账号 ID、仓储错误、SQL 片段…）
 * 下发给用户；界面按码做 i18n，不按文案匹配。
 */
export type VideoTaskErrorCode =
  | 'processing_delayed'
  | 'account_not_bound'
  | 'account_load_failed'
  | 'dependency_unavailable'
  | 'upstream_failed'
  | 'settlement_failed'
  | 'internal_error'

export interface VideoTaskSummary {
  task_no: string
  status: VideoTaskStatus
  model?: string
  platform?: string
  billing_mode?: string
  created_at: string
  /** 未结算时后端不下发该字段。 */
  settled_at?: string
  /** 失败说明或处理状态更新（后端给的是可公开文案，不含内部诊断）。 */
  last_error?: string
  /** 稳定错误码；界面按码做 i18n。 */
  last_error_code?: VideoTaskErrorCode
}

export interface VideoTaskListParams {
  limit?: number
  /** 上一页返回的 next_cursor；首页不传。 */
  cursor?: string | null
}

export interface VideoTaskListResult {
  items: VideoTaskSummary[]
  /** 还有下一页时非空；为 null / 空串表示没有更多了。 */
  next_cursor: string | null
}

export const VIDEO_TASK_LIST_LIMIT = 20

/**
 * 归一化列表响应。
 *
 * 后端游标分页改造前后两种形状都可能出现：
 *  - 旧形状：直接是数组 → 视为没有游标的单页；
 *  - 新形状：{ items, next_cursor }。
 */
export function normalizeVideoTaskList(payload: unknown): VideoTaskListResult {
  if (Array.isArray(payload)) {
    return { items: payload as VideoTaskSummary[], next_cursor: null }
  }
  const record = payload as { items?: unknown; next_cursor?: unknown } | null
  const items = Array.isArray(record?.items) ? (record.items as VideoTaskSummary[]) : []
  const cursor = typeof record?.next_cursor === 'string' && record.next_cursor.trim()
    ? record.next_cursor.trim()
    : null
  return { items, next_cursor: cursor }
}

/** 当前用户的视频任务列表（游标分页）。 */
export async function listVideoTasks(
  params: VideoTaskListParams = {},
): Promise<VideoTaskListResult> {
  const limit = params.limit ?? VIDEO_TASK_LIST_LIMIT
  const { data } = await apiClient.get<VideoTaskListResult | VideoTaskSummary[]>('/videos/tasks', {
    params: {
      limit,
      ...(params.cursor ? { cursor: params.cursor } : {}),
    },
  })
  return normalizeVideoTaskList(data)
}

export const videosAPI = {
  listTasks: listVideoTasks,
}

export default videosAPI
