import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import VideoTasksView from '../VideoTasksView.vue'
import zhDashboard from '@/i18n/locales/zh/dashboard'
import enDashboard from '@/i18n/locales/en/dashboard'

const { listVideoTasks } = vi.hoisted(() => ({ listVideoTasks: vi.fn() }))

vi.mock('@/api/videos', () => ({
  listVideoTasks,
  VIDEO_TASK_LIST_LIMIT: 20,
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  // 真实 i18n 会翻译；这里只为错误码那组键提供译文，其余键按键名回显，
  // 便于断言「界面用的是稳定错误码的译文，而不是后端文案」。
  const translations: Record<string, string> = {
    'videoTasks.errors.account_load_failed': '任务读取上游账号失败',
    'videoTasks.errors.processing_delayed': '任务仍在处理中，请稍后查看状态或联系客服。',
  }
  return { ...actual, useI18n: () => ({ t: (key: string) => translations[key] ?? key }) }
})

vi.mock('@/utils/platformColors', () => ({
  platformLabel: (platform: string) => platform,
}))

const baseTask = {
  task_no: 'VT-1',
  status: 'completed' as const,
  model: 'veo-3',
  platform: 'gemini',
  billing_mode: 'per_request',
  created_at: '2026-10-20T10:00:00Z',
  settled_at: '2026-10-20T10:05:00Z',
}

function mountView() {
  return mount(VideoTasksView, {
    global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } },
  })
}

describe('VideoTasksView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listVideoTasks.mockResolvedValue({ items: [baseTask], next_cursor: null })
  })

  it('renders the task list with model, status and timestamps', async () => {
    const wrapper = mountView()
    await flushPromises()

    const row = wrapper.get('[data-testid="video-task-row"]')
    expect(row.text()).toContain('VT-1')
    expect(row.text()).toContain('veo-3')
    expect(row.get('[data-testid="video-task-status"]').text()).toBe('videoTasks.status.completed')
    expect(row.text()).toContain('videoTasks.columns.createdAt')
    expect(row.text()).toContain('videoTasks.columns.settledAt')
    expect(wrapper.find('[data-testid="video-tasks-empty"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it.each([
    ['processing', 'videoTasks.status.processing'],
    ['completed', 'videoTasks.status.completed'],
    ['failed', 'videoTasks.status.failed'],
  ])('maps the %s status', async (status, expected) => {
    listVideoTasks.mockResolvedValue({ items: [{ ...baseTask, status }], next_cursor: null })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="video-task-status"]').text()).toBe(expected)
    wrapper.unmount()
  })

  it.each([
    ['per_token', 'videoTasks.billingMode.per_token'],
    ['per_request', 'videoTasks.billingMode.per_request'],
    ['per_second', 'videoTasks.billingMode.per_second'],
  ])('shows the %s billing mode', async (billing_mode, expected) => {
    listVideoTasks.mockResolvedValue({
      items: [{ ...baseTask, billing_mode }],
      next_cursor: null,
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="video-task-row"]').text()).toContain(expected)
    wrapper.unmount()
  })

  it('does not render an empty billing mode label when the field is missing', async () => {
    listVideoTasks.mockResolvedValue({
      items: [{ ...baseTask, billing_mode: undefined }],
      next_cursor: null,
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="video-task-row"]').text()).not.toContain('videoTasks.billingMode')
    wrapper.unmount()
  })

  it('shows the failure reason when the task carries one', async () => {
    listVideoTasks.mockResolvedValue({
      items: [{ ...baseTask, status: 'failed', last_error: 'upstream timeout' }],
      next_cursor: null,
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="video-task-error"]').text()).toContain('upstream timeout')
    wrapper.unmount()
  })

  it('does not promise automatic checks when the worker may be disabled', () => {
    expect(zhDashboard.videoTasks.errors.processing_delayed).toBe('任务仍在处理中，请稍后查看状态或联系客服。')
    expect(zhDashboard.videoTasks.errors.settlement_failed).toBe('视频已生成但结算尚未完成，请稍后查看状态或联系客服。')
    expect(enDashboard.videoTasks.errors.processing_delayed).toBe('The task is still processing. Check its status later or contact support.')
    expect(enDashboard.videoTasks.errors.settlement_failed).toBe('The video was generated, but settlement is not complete. Check its status later or contact support.')
  })

  it('labels a processing diagnostic as an update rather than a failure', async () => {
    listVideoTasks.mockResolvedValue({
      items: [{
        ...baseTask,
        status: 'processing',
        last_error: 'The task is still processing. Its status will be checked again.',
        last_error_code: 'processing_delayed',
      }],
      next_cursor: null,
    })

    const wrapper = mountView()
    await flushPromises()

    const text = wrapper.get('[data-testid="video-task-error"]').text()
    expect(text).toContain('videoTasks.processingUpdate')
    expect(text).toContain('任务仍在处理中，请稍后查看状态或联系客服。')
    expect(text).not.toContain('The task is still processing. Its status will be checked again.')
    expect(text).not.toContain('videoTasks.lastError')
    wrapper.unmount()
  })

  it('shows the failure reason by the stable error code and never renders the backend text', async () => {
    listVideoTasks.mockResolvedValue({
      items: [{
        ...baseTask,
        status: 'failed',
        last_error: 'load carrying account 99 failed: database temporarily unavailable',
        last_error_code: 'account_load_failed',
      }],
      next_cursor: null,
    })

    const wrapper = mountView()
    await flushPromises()

    const text = wrapper.get('[data-testid="video-task-error"]').text()
    expect(text).toContain('任务读取上游账号失败')
    expect(text).not.toContain('load carrying account')
    expect(text).not.toContain('99')
    wrapper.unmount()
  })

  it('renders only the fields the user-side contract exposes', async () => {
    listVideoTasks.mockResolvedValue({
      items: [{
        ...baseTask,
        upstream_task_id: 'secret-upstream-id',
        account_id: 42,
        billing_snapshot: 'secret-snapshot',
      }],
      next_cursor: null,
    })

    const wrapper = mountView()
    await flushPromises()

    const text = wrapper.text()
    expect(text).not.toContain('secret-upstream-id')
    expect(text).not.toContain('secret-snapshot')
    expect(text).not.toContain('42')
    wrapper.unmount()
  })

  it('shows an empty state when there is no task', async () => {
    listVideoTasks.mockResolvedValue({ items: [], next_cursor: null })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="video-tasks-empty"]').text()).toContain('videoTasks.empty')
    expect(wrapper.find('[data-testid="video-tasks-list"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="video-tasks-load-more"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows the failure state when the request fails', async () => {
    listVideoTasks.mockRejectedValue({ status: 500 })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="video-tasks-error"]').text()).toContain('videoTasks.loadFailed')
    expect(wrapper.find('[data-testid="video-tasks-list"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('loads the next page by cursor and appends it to the list', async () => {
    listVideoTasks.mockResolvedValueOnce({ items: [baseTask], next_cursor: 'cursor-2' })
    listVideoTasks.mockResolvedValueOnce({
      items: [{ ...baseTask, task_no: 'VT-2' }],
      next_cursor: null,
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="video-tasks-load-more"]').exists()).toBe(true)
    await wrapper.get('[data-testid="video-tasks-load-more"]').trigger('click')
    await flushPromises()

    expect(listVideoTasks).toHaveBeenLastCalledWith({ limit: 20, cursor: 'cursor-2' })
    expect(wrapper.findAll('[data-testid="video-task-row"]')).toHaveLength(2)
    expect(wrapper.text()).toContain('VT-2')
    // 没有下一页时隐藏按钮
    expect(wrapper.find('[data-testid="video-tasks-load-more"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps the loaded list when loading the next page fails', async () => {
    listVideoTasks.mockResolvedValueOnce({ items: [baseTask], next_cursor: 'cursor-2' })
    listVideoTasks.mockRejectedValueOnce({ status: 500 })

    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="video-tasks-load-more"]').trigger('click')
    await flushPromises()

    expect(wrapper.findAll('[data-testid="video-task-row"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="video-tasks-load-more-error"]').text()).toContain('videoTasks.loadMoreFailed')
    wrapper.unmount()
  })
})
