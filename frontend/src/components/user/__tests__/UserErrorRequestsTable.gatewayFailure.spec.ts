import { describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

import UserErrorRequestsTable from '../UserErrorRequestsTable.vue'

function mountTable(rows: Array<Record<string, unknown>>) {
  return mount(UserErrorRequestsTable, {
    props: { rows: rows as never, total: rows.length, loading: false, page: 1, pageSize: 20 },
    global: {
      stubs: {
        DataTable: {
          props: ['columns', 'data', 'loading'],
          template: `<div><div v-for="row in data" :key="row.id">
            <slot name="cell-message" :row="row" />
          </div></div>`,
        },
        IpGeoBatchToolbar: true,
        Pagination: true,
        UserErrorDetailModal: true,
        IpGeoCell: true,
        Icon: true,
      },
    },
  })
}

describe('UserErrorRequestsTable — F-06 两套口径的接线', () => {
  it('renders exactly one badge for a missing-price failure (legacy pricing badge owns this case)', async () => {
    const wrapper = mountTable([
      { id: 1, status_code: 503, message: 'Model pricing is unavailable. Please retry later.', model: 'gpt-x' },
    ])
    await flushPromises()
    // 缺价徽标是既有用户可见契约（有独立测试锁定），必须继续存在；
    // 且不能与新的 gateway 徽标重复展示同一条信息。
    expect(wrapper.find('[data-testid="missing-price-badge"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="gateway-failure-pricing-unavailable"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('renders the NEW model_not_found badge for a 404 model_not_found failure', async () => {
    // 这是本轮 F-06 新增的用户可见信息：此前它与缺价混在一起，用户不知道要换模型。
    const wrapper = mountTable([
      { id: 2, status_code: 404, message: 'model_not_found: gpt-9', model: 'gpt-9' },
    ])
    await flushPromises()
    expect(wrapper.find('[data-testid="gateway-failure-model-not-found"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="missing-price-badge"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('leaves ordinary failures without any badge', async () => {
    const wrapper = mountTable([
      { id: 3, status_code: 500, message: 'upstream connection reset', model: 'gpt-x' },
    ])
    await flushPromises()
    expect(wrapper.find('[data-testid="gateway-failure-pricing-unavailable"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="gateway-failure-model-not-found"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="missing-price-badge"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('never shows two badges for one row', async () => {
    for (const row of [
      { id: 4, status_code: 503, message: 'no pricing available for model: x: pricing not found' },
      { id: 5, status_code: 404, message: 'model_not_found: x' },
      { id: 6, status_code: 429, message: 'rate limited' },
    ]) {
      const wrapper = mountTable([row])
      await flushPromises()
      const total =
        wrapper.findAll('[data-testid="missing-price-badge"]').length +
        wrapper.findAll('[data-testid="gateway-failure-pricing-unavailable"]').length +
        wrapper.findAll('[data-testid="gateway-failure-model-not-found"]').length
      expect(total).toBeLessThanOrEqual(1)
      wrapper.unmount()
    }
  })
})
