import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import ShopSyncStatusCard from '../ShopSyncStatusCard.vue'
import { shopSyncErrorI18nKey } from '../shopSyncError'

const api = vi.hoisted(() => ({ getShopSyncStatus: vi.fn() }))
vi.mock('@/api/admin/shop', () => ({ getShopSyncStatus: api.getShopSyncStatus }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))

enableAutoUnmount(afterEach)

const okStatus = {
  last_success_at: '2026-10-20T08:00:00Z',
  last_attempt_at: '2026-10-21T08:00:00Z',
  error: '',
  paused: false,
  product_count: 7
}

beforeEach(() => {
  vi.resetAllMocks()
  api.getShopSyncStatus.mockResolvedValue({ ...okStatus })
})

const mountCard = async () => {
  const wrapper = mount(ShopSyncStatusCard)
  await flushPromises()
  return wrapper
}

describe('admin shop sync status card', () => {
  it('renders a healthy sync state with the last success time and product count', async () => {
    const wrapper = await mountCard()

    expect(api.getShopSyncStatus).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-testid="shop-sync-state"]').text()).toBe('admin.shopSync.stateOk')
    expect(wrapper.get('[data-testid="shop-sync-state"]').classes()).toContain('badge-success')
    expect(wrapper.get('[data-testid="shop-sync-last-success"]').text()).toBe('2026-10-20T08:00:00Z')
    expect(wrapper.get('[data-testid="shop-sync-last-attempt"]').text()).toBe('2026-10-21T08:00:00Z')
    expect(wrapper.get('[data-testid="shop-sync-product-count"]').text()).toBe('7')
    expect(wrapper.find('[data-testid="shop-sync-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="shop-sync-paused"]').exists()).toBe(false)
  })

  it('maps a known error code to readable copy and flags that purchasing is paused', async () => {
    api.getShopSyncStatus.mockResolvedValue({
      ...okStatus,
      error: 'shop_upstream_rejected',
      paused: true
    })

    const wrapper = await mountCard()

    const error = wrapper.get('[data-testid="shop-sync-error"]')
    expect(error.text()).toBe('admin.shopSync.errors.serviceRejected')
    expect(error.classes()).toContain('text-amber-600')
    // 原始码只留在 title 属性里，不作为正文展示。
    expect(error.attributes('title')).toBe('shop_upstream_rejected')
    expect(wrapper.get('[data-testid="shop-sync-state"]').text()).toBe('admin.shopSync.stateFailed')
    expect(wrapper.get('[data-testid="shop-sync-state"]').classes()).toContain('badge-warning')
    expect(wrapper.get('[data-testid="shop-sync-paused"]').text()).toBe('admin.shopSync.paused')
  })

  it('renders the not-configured state distinctly from other failures', async () => {
    api.getShopSyncStatus.mockResolvedValue({
      ...okStatus,
      last_success_at: null,
      last_attempt_at: null,
      error: 'shop_not_configured',
      paused: true,
      product_count: 0
    })

    const wrapper = await mountCard()

    expect(wrapper.get('[data-testid="shop-sync-state"]').text()).toBe('admin.shopSync.stateNotConfigured')
    expect(wrapper.get('[data-testid="shop-sync-state"]').classes()).toContain('badge-gray')
    expect(wrapper.get('[data-testid="shop-sync-error"]').text()).toBe('admin.shopSync.errors.notConfigured')
    expect(wrapper.get('[data-testid="shop-sync-last-success"]').text()).toBe('admin.shopSync.neverSynced')
    expect(wrapper.get('[data-testid="shop-sync-last-attempt"]').text()).toBe('admin.shopSync.neverAttempted')
    expect(wrapper.get('[data-testid="shop-sync-product-count"]').text()).toBe('0')
  })

  it('falls back to generic copy for an unknown error code without printing the raw code', async () => {
    api.getShopSyncStatus.mockResolvedValue({
      ...okStatus,
      error: 'shop_brand_new_upstream_code',
      paused: true
    })

    const wrapper = await mountCard()

    const error = wrapper.get('[data-testid="shop-sync-error"]')
    expect(error.text()).toBe('admin.shopSync.errors.unknown')
    expect(error.text()).not.toContain('shop_brand_new_upstream_code')
  })

  it('stays usable when the sync status endpoint fails', async () => {
    api.getShopSyncStatus.mockRejectedValue(new Error('network down'))
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

    const wrapper = await mountCard()

    expect(wrapper.get('[data-testid="shop-sync-state"]').text()).toBe('admin.shopSync.stateUnknown')
    expect(wrapper.get('[data-testid="shop-sync-error"]').text()).toBe('admin.shopSync.loadFailed')
    expect(wrapper.get('[data-testid="shop-sync-product-count"]').text()).toBe('0')
    expect(wrapper.find('[data-testid="shop-sync-paused"]').exists()).toBe(false)
    errorSpy.mockRestore()
  })
})

describe('shop sync error code mapping', () => {
  it('maps every documented stable code to its own copy', () => {
    expect(shopSyncErrorI18nKey('shop_not_configured')).toBe('admin.shopSync.errors.notConfigured')
    expect(shopSyncErrorI18nKey('product_sync_unavailable')).toBe('admin.shopSync.errors.productSyncUnavailable')
    expect(shopSyncErrorI18nKey('shop_upstream_unavailable')).toBe('admin.shopSync.errors.serviceUnavailable')
    expect(shopSyncErrorI18nKey('shop_upstream_rejected')).toBe('admin.shopSync.errors.serviceRejected')
    expect(shopSyncErrorI18nKey('shop_upstream_response_invalid')).toBe('admin.shopSync.errors.serviceResponseInvalid')
  })

  it('treats an empty code as no error and unknown codes as a generic failure', () => {
    expect(shopSyncErrorI18nKey('')).toBeNull()
    expect(shopSyncErrorI18nKey('   ')).toBeNull()
    expect(shopSyncErrorI18nKey(null)).toBeNull()
    expect(shopSyncErrorI18nKey('shop_something_else')).toBe('admin.shopSync.errors.unknown')
  })
})
