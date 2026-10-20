import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import VideoTasksView from '../VideoTasksView.vue'

const { listTasks } = vi.hoisted(() => ({ listTasks: vi.fn() }))

vi.mock('@/api/videos', () => ({
  videosAPI: { listTasks },
  VIDEO_TASK_LIST_LIMIT: 20,
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
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
    listTasks.mockResolvedValue([baseTask])
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
    listTasks.mockResolvedValue([{ ...baseTask, status }])

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="video-task-status"]').text()).toBe(expected)
    wrapper.unmount()
  })

  it('shows the failure reason when the task carries one', async () => {
    listTasks.mockResolvedValue([{ ...baseTask, status: 'failed', last_error: 'upstream timeout' }])

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="video-task-error"]').text()).toContain('upstream timeout')
    wrapper.unmount()
  })

  it('renders only the fields the user-side contract exposes', async () => {
    listTasks.mockResolvedValue([{
      ...baseTask,
      upstream_task_id: 'secret-upstream-id',
      account_id: 42,
      billing_snapshot: 'secret-snapshot',
    }])

    const wrapper = mountView()
    await flushPromises()

    const text = wrapper.text()
    expect(text).not.toContain('secret-upstream-id')
    expect(text).not.toContain('secret-snapshot')
    expect(text).not.toContain('42')
    wrapper.unmount()
  })

  it('shows an empty state when there is no task', async () => {
    listTasks.mockResolvedValue([])

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="video-tasks-empty"]').text()).toContain('videoTasks.empty')
    expect(wrapper.find('[data-testid="video-tasks-list"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows the failure state when the request fails', async () => {
    listTasks.mockRejectedValue({ status: 500 })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="video-tasks-error"]').text()).toContain('videoTasks.loadFailed')
    expect(wrapper.find('[data-testid="video-tasks-list"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
