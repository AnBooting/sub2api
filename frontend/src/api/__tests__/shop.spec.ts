import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, post },
}))

import { createIdempotencyKey, shopAPI, shopErrorKey } from '@/api/shop'

describe('shop api', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    get.mockResolvedValue({ data: {} })
    post.mockResolvedValue({ data: {} })
  })

  it('reads the catalog from the site API and unwraps the data', async () => {
    get.mockResolvedValue({ data: { configured: false, available: false, products: [] } })
    const catalog = await shopAPI.getCatalog()

    expect(get).toHaveBeenCalledWith('/shop/products')
    expect(catalog.configured).toBe(false)
  })

  it('creates orders through the site API with the idempotency key', async () => {
    post.mockResolvedValue({ data: { order_no: 'RC1' } })
    await shopAPI.createOrder({ product_id: 'sku-1', quantity: 1, idempotency_key: 'key-1' })

    expect(post).toHaveBeenCalledWith('/shop/orders', {
      product_id: 'sku-1',
      quantity: 1,
      idempotency_key: 'key-1',
    })
  })

  it('encodes the order number in detail and query paths', async () => {
    await shopAPI.getOrder('RC/1 2')
    await shopAPI.queryOrder('RC/1 2')

    expect(get).toHaveBeenCalledWith('/shop/orders/RC%2F1%202')
    expect(post).toHaveBeenCalledWith('/shop/orders/RC%2F1%202/query')
  })

  it('generates a fresh idempotency key per call', () => {
    const first = createIdempotencyKey()
    const second = createIdempotencyKey()
    expect(first).not.toBe(second)
    expect(first.length).toBeGreaterThan(8)
  })

  it('maps stable backend reasons to locale keys and falls back for unknown ones', () => {
    expect(shopErrorKey('shop_not_configured')).toBe('shop.errors.notConfigured')
    expect(shopErrorKey('product_sync_unavailable')).toBe('shop.errors.syncUnavailable')
    expect(shopErrorKey('order_delivery_pending')).toBe('shop.errors.deliveryPending')
    expect(shopErrorKey('order_not_paid')).toBe('shop.errors.notPaid')
    expect(shopErrorKey(undefined)).toBe('shop.errors.generic')
    expect(shopErrorKey('some_unknown_reason')).toBe('shop.errors.generic')
  })

  it('never builds a URL pointing outside the site API', async () => {
    await shopAPI.getCatalog()
    for (const call of [...get.mock.calls, ...post.mock.calls]) {
      const url = String(call[0])
      expect(url.startsWith('/shop/')).toBe(true)
      expect(url.includes('://')).toBe(false)
    }
  })
})
