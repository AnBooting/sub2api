import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ShopView from '../ShopView.vue'

const { getCatalog, createOrder, queryOrder, listOrders, getOrder, navigateToPaymentPage, replace, routeState } = vi.hoisted(() => ({
  getCatalog: vi.fn(),
  createOrder: vi.fn(),
  queryOrder: vi.fn(),
  listOrders: vi.fn(),
  getOrder: vi.fn(),
  navigateToPaymentPage: vi.fn(),
  replace: vi.fn(),
  routeState: { path: '/shop', query: {} as Record<string, string> },
}))

vi.mock('@/api/shop', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/shop')>()
  return {
    ...actual,
    shopAPI: { getCatalog, createOrder, queryOrder, listOrders, getOrder },
  }
})

vi.mock('@/components/user/shopNavigation', () => ({ navigateToPaymentPage }))

vi.mock('vue-router', () => ({
  useRoute: () => routeState,
  useRouter: () => ({ replace, push: vi.fn() }),
}))

vi.mock('qrcode', () => ({
  default: { toDataURL: vi.fn().mockResolvedValue('data:image/png;base64,stub') },
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ user: { username: 'tester', balance: 12.5 } }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const catalog = {
  configured: true,
  available: true,
  products: [{
    id: 'p1',
    name: 'Gift card',
    description: '',
    price_cny: 10,
    listed: true,
    purchasable: true,
    credit_per_cny: 1,
  }],
}

function mountShop() {
  return mount(ShopView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        Icon: true,
        ShopOrdersPanel: {
          props: ['initialOrderNo'],
          template: '<div data-testid="stub-orders-panel">{{ initialOrderNo }}</div>',
        },
      },
    },
  })
}

describe('ShopView orders tab and redirect payment', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    window.localStorage.clear()
    routeState.path = '/shop'
    routeState.query = {}
    getCatalog.mockResolvedValue(catalog)
    listOrders.mockResolvedValue([])
    getOrder.mockResolvedValue({})
    queryOrder.mockResolvedValue({})
  })

  it('shows the credit-rate and manual-redeem hints', async () => {
    const wrapper = mountShop()
    await flushPromises()

    expect(wrapper.get('[data-testid="shop-credit-rate-hint"]').text()).toContain('shop.creditRateHint')
    expect(wrapper.find('[data-testid="shop-tab-orders"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('redirects to the payment page returned by the server', async () => {
    createOrder.mockResolvedValue({
      order_no: 'SNAP-1',
      product_id: 'p1',
      quantity: 1,
      amount_cny: 10,
      display_status: 'pending',
      payment: { kind: 'redirect', redirect_url: 'https://pay.example.com/checkout' },
    })
    const wrapper = mountShop()
    await flushPromises()

    await wrapper.get('button.btn-primary').trigger('click')
    await flushPromises()

    expect(createOrder).toHaveBeenCalled()
    // 进入「支付中」阶段就自动跳转到服务端下发的支付页
    expect(navigateToPaymentPage).toHaveBeenCalledWith('https://pay.example.com/checkout')
    // 同时保留手动重试入口
    const goButton = wrapper.find('[data-testid="shop-redirect-go"]')
    expect(goButton.exists()).toBe(true)
    await goButton.trigger('click')

    expect(navigateToPaymentPage).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('blocks the redirect and explains it when the server returns no target', async () => {
    createOrder.mockResolvedValue({
      order_no: 'SNAP-2',
      product_id: 'p1',
      quantity: 1,
      amount_cny: 10,
      display_status: 'pending',
      payment: { kind: 'redirect' },
    })
    const wrapper = mountShop()
    await flushPromises()

    await wrapper.get('button.btn-primary').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="shop-redirect-blocked"]').text()).toContain('shop.payment.redirectBlocked')
    expect(navigateToPaymentPage).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('remembers the created order so a refresh can find it again', async () => {
    createOrder.mockResolvedValue({
      order_no: 'SNAP-3',
      product_id: 'p1',
      quantity: 1,
      amount_cny: 10,
      display_status: 'pending',
      payment: { kind: 'qr_code', qr_content: 'qr' },
    })
    const wrapper = mountShop()
    await flushPromises()

    await wrapper.get('button.btn-primary').trigger('click')
    await flushPromises()

    expect(window.localStorage.getItem('shop_last_order_no')).toBe('SNAP-3')
    expect(replace).toHaveBeenCalledWith({ path: '/shop', query: { order: 'SNAP-3' } })
    wrapper.unmount()
  })

  it('opens the orders tab with the order given by the URL', async () => {
    routeState.query = { order: 'SNAP-9' }
    const wrapper = mountShop()
    await flushPromises()

    expect(wrapper.get('[data-testid="stub-orders-panel"]').text()).toContain('SNAP-9')
    wrapper.unmount()
  })
})
