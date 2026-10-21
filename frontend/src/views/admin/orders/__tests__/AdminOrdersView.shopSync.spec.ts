import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AdminOrdersView from '../AdminOrdersView.vue'

const { getOrders, getShopSyncStatus } = vi.hoisted(() => ({
  getOrders: vi.fn(),
  getShopSyncStatus: vi.fn(),
}))

vi.mock('@/api/admin/payment', () => {
  const adminPaymentAPI = { getOrders }
  return { adminPaymentAPI, default: { adminPaymentAPI } }
})

vi.mock('@/api/admin/shop', () => ({
  getShopSyncStatus,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

// 状态条内部有 30s 轮询定时器，卸载时清理，避免用例结束后继续触发。
enableAutoUnmount(afterEach)

const mountView = async () => {
  const wrapper = mount(AdminOrdersView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        Pagination: true,
        BaseDialog: true,
        Select: true,
        Icon: true,
        AdminRefundDialog: true,
        OrderStatusBadge: true,
        OrderTable: { template: '<div data-testid="order-table" />' },
      },
    },
  })
  await flushPromises()
  return wrapper
}

describe('AdminOrdersView product sync status', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    getOrders.mockResolvedValue({ data: { items: [], total: 0 } })
    getShopSyncStatus.mockResolvedValue({
      last_success_at: '2026-10-20T08:00:00Z',
      last_attempt_at: '2026-10-21T08:00:00Z',
      error: 'shop_upstream_unavailable',
      paused: true,
      product_count: 3,
    })
  })

  it('shows the product sync status card at the top of the orders page', async () => {
    const wrapper = await mountView()

    const card = wrapper.get('[data-testid="shop-sync-status"]')
    expect(card.exists()).toBe(true)
    expect(getShopSyncStatus).toHaveBeenCalledTimes(1)
    expect(card.get('[data-testid="shop-sync-error"]').text()).toBe('admin.shopSync.errors.serviceUnavailable')
    expect(card.get('[data-testid="shop-sync-paused"]').text()).toBe('admin.shopSync.paused')
    expect(card.get('[data-testid="shop-sync-product-count"]').text()).toBe('3')
  })

  it('keeps the orders page usable when the sync status request fails', async () => {
    getShopSyncStatus.mockRejectedValue(new Error('boom'))
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

    const wrapper = await mountView()

    expect(wrapper.get('[data-testid="shop-sync-status"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="shop-sync-error"]').text()).toBe('admin.shopSync.loadFailed')
    expect(wrapper.get('[data-testid="order-table"]').exists()).toBe(true)
    errorSpy.mockRestore()
  })
})
