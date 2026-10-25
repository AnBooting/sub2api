import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ShopOrdersPanel from '../ShopOrdersPanel.vue'

const { listOrders, getOrder, queryOrder } = vi.hoisted(() => ({
  listOrders: vi.fn(),
  getOrder: vi.fn(),
  queryOrder: vi.fn(),
}))

vi.mock('@/api/shop', () => ({
  shopAPI: { listOrders, getOrder, queryOrder },
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
}))

const pendingOrder = {
  order_no: 'SNAP-1',
  product_id: 'p1',
  product_name: 'Gift card',
  quantity: 1,
  amount_cny: 10,
  display_status: 'pending' as const,
}
const deliveredOrder = {
  ...pendingOrder,
  display_status: 'delivered' as const,
  card_secret: 'CARD-SECRET-1',
  card_hint: 'CARD-****1',
  redeem_path: '/redeem',
}

function mountPanel(props: Record<string, unknown> = {}) {
  return mount(ShopOrdersPanel, { props })
}

describe('ShopOrdersPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listOrders.mockResolvedValue([pendingOrder])
    getOrder.mockResolvedValue(deliveredOrder)
    queryOrder.mockResolvedValue({ ...deliveredOrder, display_status: 'delivery_pending' })
  })

  it('lists the orders with their status', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(listOrders).toHaveBeenCalled()
    expect(wrapper.get('[data-testid="shop-orders-list"]').text()).toContain('SNAP-1')
    expect(wrapper.text()).toContain('shop.status.pending')
    expect(wrapper.text()).toContain('¥10.00')
    wrapper.unmount()
  })

  it('opens the order detail when a row is clicked', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.get('[data-testid="shop-order-row-SNAP-1"]').trigger('click')
    await flushPromises()

    expect(getOrder).toHaveBeenCalledWith('SNAP-1')
    expect(wrapper.get('[data-testid="shop-order-detail"]').text()).toContain('shop.status.delivered')
    expect(wrapper.get('[data-testid="shop-order-card"]').text()).toContain('CARD-SECRET-1')
    wrapper.unmount()
  })

  it('re-queries the order status from the server', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.get('[data-testid="shop-order-row-SNAP-1"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="shop-order-query"]').trigger('click')
    await flushPromises()

    expect(queryOrder).toHaveBeenCalledWith('SNAP-1')
    expect(wrapper.get('[data-testid="shop-order-status"]').text()).toContain('shop.status.delivery_pending')
    wrapper.unmount()
  })

  it('restores the order given by the URL or the local snapshot', async () => {
    const wrapper = mountPanel({ initialOrderNo: 'SNAP-9' })
    await flushPromises()

    expect(getOrder).toHaveBeenCalledWith('SNAP-9')
    expect(wrapper.get('[data-testid="shop-order-detail"]').text()).toContain('SNAP-1')
    wrapper.unmount()
  })

  it('shows an empty state when there is no order', async () => {
    listOrders.mockResolvedValue([])
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.get('[data-testid="shop-orders-empty"]').text()).toContain('shop.orders.empty')
    wrapper.unmount()
  })
})
