import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import RedeemView from '../RedeemView.vue'

const { redeem, getHistory, refreshUser, fetchActiveSubscriptions } = vi.hoisted(() => ({
  redeem: vi.fn(),
  getHistory: vi.fn(),
  refreshUser: vi.fn(),
  fetchActiveSubscriptions: vi.fn(),
}))

vi.mock('@/api', () => ({
  redeemAPI: { redeem, getHistory },
  authAPI: { getPublicSettings: vi.fn().mockResolvedValue({}) },
}))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ user: { balance: 10, concurrency: 2 }, refreshUser }),
}))
vi.mock('@/stores/subscriptions', () => ({
  useSubscriptionStore: () => ({ fetchActiveSubscriptions }),
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn(), showWarning: vi.fn(), showSuccess: vi.fn() }),
}))

// 模拟 vue-i18n：命中的键返回固定文案，未命中的键原样返回（与 vue-i18n 行为一致）。
const MESSAGES: Record<string, string> = {
  'redeem.errors.REDEEM_CODE_USED': 'USED-MESSAGE',
  'redeem.errors.REDEEM_CODE_NOT_FOUND': 'NOT-FOUND-MESSAGE',
  'redeem.errors.REDEEM_CODE_EXPIRED': 'EXPIRED-MESSAGE',
  'redeem.failedToRedeem': 'GENERIC-MESSAGE',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => MESSAGES[key] ?? key,
    }),
  }
})

async function submitCode() {
  const wrapper = mount(RedeemView, {
    global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } },
  })
  await flushPromises()
  await wrapper.get('input#code').setValue('REDEEM-CODE')
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  return wrapper
}

describe('RedeemView failure reasons', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getHistory.mockResolvedValue({ items: [], total: 0 })
    refreshUser.mockResolvedValue({ balance: 30, concurrency: 2 })
    fetchActiveSubscriptions.mockResolvedValue([])
  })

  it.each([
    ['REDEEM_CODE_USED', 'USED-MESSAGE'],
    ['REDEEM_CODE_NOT_FOUND', 'NOT-FOUND-MESSAGE'],
    ['REDEEM_CODE_EXPIRED', 'EXPIRED-MESSAGE'],
  ])('shows the dedicated message for %s', async (reason, expected) => {
    // 后端错误信封里没有 detail，稳定错误码在 reason 上。
    redeem.mockRejectedValue({ status: 409, reason, message: 'raw server message' })

    const wrapper = await submitCode()

    expect(wrapper.text()).toContain(expected)
    expect(wrapper.text()).not.toContain('raw server message')
    wrapper.unmount()
  })

  it('falls back to the server message for an unknown reason', async () => {
    redeem.mockRejectedValue({ status: 500, reason: 'SOMETHING_ELSE', message: 'boom' })

    const wrapper = await submitCode()

    // 未命中 i18n 映射时按仓库既有约定回落到服务端消息（不展示不存在的 detail 字段）。
    expect(wrapper.text()).toContain('boom')
    wrapper.unmount()
  })

  it('shows the credit-rate hints on the redeem page', async () => {
    const wrapper = mount(RedeemView, {
      global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } },
    })
    await flushPromises()

    const hints = wrapper.get('[data-testid="redeem-credit-hints"]').text()
    expect(hints).toContain('redeem.creditRateHint')
    expect(hints).toContain('redeem.manualCreditHint')
    wrapper.unmount()
  })
})
