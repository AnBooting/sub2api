import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))

vi.mock('@/api/client', () => ({
  apiClient: { get },
}))

import { listVideoTasks, normalizeVideoTaskList, VIDEO_TASK_LIST_LIMIT } from '@/api/videos'

describe('normalizeVideoTaskList', () => {
  it('keeps the cursor of the new payload', () => {
    const result = normalizeVideoTaskList({ items: [{ task_no: 'A' }], next_cursor: 'cursor-2' })

    expect(result.items).toHaveLength(1)
    expect(result.next_cursor).toBe('cursor-2')
  })

  it('treats a legacy array payload as a single page', () => {
    const result = normalizeVideoTaskList([{ task_no: 'A' }])

    expect(result.items).toHaveLength(1)
    expect(result.next_cursor).toBeNull()
  })

  it('returns an empty list for an unexpected payload', () => {
    expect(normalizeVideoTaskList(null)).toEqual({ items: [], next_cursor: null })
    expect(normalizeVideoTaskList({ items: 'oops' })).toEqual({ items: [], next_cursor: null })
    expect(normalizeVideoTaskList({ next_cursor: 42 }).next_cursor).toBeNull()
  })
})

describe('listVideoTasks', () => {
  beforeEach(() => {
    get.mockReset()
  })

  it('requests the first page without a cursor', async () => {
    get.mockResolvedValue({ data: { items: [{ task_no: 'A' }], next_cursor: 'cursor-2' } })

    const result = await listVideoTasks()

    expect(get).toHaveBeenCalledWith('/videos/tasks', { params: { limit: VIDEO_TASK_LIST_LIMIT } })
    expect(result.next_cursor).toBe('cursor-2')
  })

  it('passes the cursor when loading the next page', async () => {
    get.mockResolvedValue({ data: { items: [{ task_no: 'B' }], next_cursor: null } })

    const result = await listVideoTasks({ cursor: 'cursor-2' })

    expect(get).toHaveBeenCalledWith('/videos/tasks', {
      params: { limit: VIDEO_TASK_LIST_LIMIT, cursor: 'cursor-2' },
    })
    expect(result.items).toHaveLength(1)
  })
})
