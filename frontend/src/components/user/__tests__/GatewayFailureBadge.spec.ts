import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import GatewayFailureBadge from '../GatewayFailureBadge.vue'
import zhDashboard from '@/i18n/locales/zh/dashboard'
import enDashboard from '@/i18n/locales/en/dashboard'

// 与仓库既有组件测试一致：把 useI18n 换成立即回显 key 的桩，
// 这样组件测试不依赖 vue-i18n 的消息编译器；文案完整性在下面的 locale 断言里单独验证。
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

function mountBadge(kind: 'pricing_unavailable' | 'model_not_found') {
  return mount(GatewayFailureBadge, { props: { kind } })
}

describe('GatewayFailureBadge', () => {
  it('renders a distinct badge and tooltip key per failure mode', () => {
    const pricing = mountBadge('pricing_unavailable')
    const notFound = mountBadge('model_not_found')

    expect(pricing.get('span').text()).toBe('gatewayFailure.pricingUnavailable.badge')
    expect(pricing.get('span').attributes('title')).toBe('gatewayFailure.pricingUnavailable.tooltip')
    expect(notFound.get('span').text()).toBe('gatewayFailure.modelNotFound.badge')
    expect(notFound.get('span').attributes('title')).toBe('gatewayFailure.modelNotFound.tooltip')

    pricing.unmount()
    notFound.unmount()
  })

  it('exposes a stable data-testid per mode', () => {
    const pricing = mountBadge('pricing_unavailable')
    const notFound = mountBadge('model_not_found')
    expect(pricing.find('[data-testid="gateway-failure-pricing-unavailable"]').exists()).toBe(true)
    expect(notFound.find('[data-testid="gateway-failure-model-not-found"]').exists()).toBe(true)
    pricing.unmount()
    notFound.unmount()
  })

  it('uses warning colour for pricing (platform to-do) and neutral for model_not_found (user picked wrong)', () => {
    const pricing = mountBadge('pricing_unavailable')
    const notFound = mountBadge('model_not_found')
    expect(pricing.get('span').classes().join(' ')).toContain('amber')
    expect(notFound.get('span').classes().join(' ')).not.toContain('amber')
    pricing.unmount()
    notFound.unmount()
  })

  it('ships both locales with non-empty, distinct copy', () => {
    for (const locale of [zhDashboard, enDashboard]) {
      const block = locale.gatewayFailure
      expect(block.pricingUnavailable.badge.trim().length).toBeGreaterThan(0)
      expect(block.pricingUnavailable.tooltip.trim().length).toBeGreaterThan(0)
      expect(block.modelNotFound.badge.trim().length).toBeGreaterThan(0)
      expect(block.modelNotFound.tooltip.trim().length).toBeGreaterThan(0)
      // 两种失败必须给不同文案，否则用户分不清「换模型」和「等补价」。
      expect(block.pricingUnavailable.badge).not.toBe(block.modelNotFound.badge)
      expect(block.pricingUnavailable.tooltip).not.toBe(block.modelNotFound.tooltip)
    }
  })
})
