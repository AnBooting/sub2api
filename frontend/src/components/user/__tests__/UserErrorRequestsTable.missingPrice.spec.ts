import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import UserErrorRequestsTable from '../UserErrorRequestsTable.vue'
import type { UserErrorRequest } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const baseRow: UserErrorRequest = {
  id: 1,
  created_at: '2026-10-21T10:00:00Z',
  model: 'gpt-x',
  inbound_endpoint: '/v1/chat/completions',
  status_code: 503,
  category: 'internal',
  platform: 'openai',
  message: 'no pricing available for model: gpt-x: pricing not found',
  key_name: 'my-key',
  key_deleted: false,
  group_name: 'standard',
}

function mountTable(rows: UserErrorRequest[]) {
  return mount(UserErrorRequestsTable, {
    props: {
      rows,
      total: rows.length,
      loading: false,
      page: 1,
      pageSize: 20,
      visibleColumnKeys: ['message'],
    },
    global: {
      stubs: { IpGeoCell: true, IpGeoBatchToolbar: true, UserErrorDetailModal: true },
    },
  })
}

describe('UserErrorRequestsTable missing price hint', () => {
  it('marks a missing-price failure with a dedicated badge', () => {
    const wrapper = mountTable([baseRow])

    expect(wrapper.find('[data-testid="missing-price-badge"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('does not mark other failures', () => {
    const wrapper = mountTable([
      { ...baseRow, id: 2, status_code: 500, message: 'upstream connection reset' },
    ])

    expect(wrapper.find('[data-testid="missing-price-badge"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
