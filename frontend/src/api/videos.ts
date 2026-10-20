/**
 * 用户侧视频任务 API。
 *
 * 契约（后端已就绪）：GET /api/v1/videos/tasks?limit=20
 *  - data 是数组，每项：{ task_no, status, model, platform, billing_mode, created_at, settled_at, last_error }
 *  - status ∈ processing | completed | failed
 *  - 空列表返回空数组（不是 404）；未登录 401。
 *  - 接口只返回本人可见字段，绝不包含 upstream_task_id / account_id。
 */

import { apiClient } from './client'

export type VideoTaskStatus = 'processing' | 'completed' | 'failed'

export interface VideoTaskSummary {
  task_no: string
  status: VideoTaskStatus
  model?: string
  platform?: string
  billing_mode?: string
  created_at: string
  /** 未结算时后端不下发该字段。 */
  settled_at?: string
  /** 失败原因；成功或处理中为空。 */
  last_error?: string
}

export const VIDEO_TASK_LIST_LIMIT = 20

export const videosAPI = {
  /** 当前用户的视频任务列表。 */
  async listTasks(limit: number = VIDEO_TASK_LIST_LIMIT): Promise<VideoTaskSummary[]> {
    const { data } = await apiClient.get<VideoTaskSummary[]>('/videos/tasks', { params: { limit } })
    return data
  },
}

export default videosAPI
