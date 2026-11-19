import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import PaymentMethodSelector from '@/components/payment/PaymentMethodSelector.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, fallback?: string) => fallback ?? key,
  }),
}))

describe('PaymentMethodSelector', () => {
  it('wraps large custom method collections without letting labels widen the selector', () => {
    const methods = Array.from({ length: 12 }, (_, index) => ({
      type: `custom_${index}`,
      display_name: `CUSTOM_PAYMENT_METHOD_${index}`,
      fee_rate: 0,
      available: true,
    }))

    const wrapper = mount(PaymentMethodSelector, {
      props: {
        selected: 'custom_0',
        methods,
      },
    })

    const grid = wrapper.get('[data-testid="payment-method-grid"]')
    expect(grid.classes()).toEqual(expect.arrayContaining(['grid', 'sm:grid-cols-3', 'lg:grid-cols-4']))
    expect(grid.classes()).not.toContain('sm:flex')

    const buttons = wrapper.findAll('button')
    expect(buttons).toHaveLength(methods.length)
    expect(buttons.every(button => button.classes().includes('min-w-0'))).toBe(true)
    expect(buttons.every((button, index) => button.attributes('title') === methods[index].display_name)).toBe(true)
    expect(wrapper.findAll('[data-testid="payment-method-label"]').every(label => label.classes().includes('truncate'))).toBe(true)
  })

  it('shows the configured display name for custom EasyPay methods', () => {
    const wrapper = mount(PaymentMethodSelector, {
      props: {
        selected: 'ldc',
        methods: [{ type: 'ldc', display_name: 'LDC Pay', fee_rate: 0, available: true }],
      },
    })

    expect(wrapper.text()).toContain('LDC Pay')
    expect(wrapper.text()).not.toContain('ldc')
    expect(wrapper.text()).not.toContain('payment.methods.ldc')
  })

  it('uses the generic signal border for custom methods that contain built-in names', () => {
    const wrapper = mount(PaymentMethodSelector, {
      props: {
        selected: 'card_alipay',
        methods: [{ type: 'card_alipay', display_name: 'Card Pay', fee_rate: 0, available: true }],
      },
    })

    const button = wrapper.get('button')
    // 通配渠道（类型名里含内置名但并非内置渠道）走通用选中态：信号色边框，不刷品牌色。
    expect(button.classes()).toContain('border-signal')
    // 品牌色例外只给内置渠道（见 brandColors.ts）：通配渠道的按钮上不得出现任何品牌色 borderColor。
    // 注意 jsdom 会把内联 hex 规范成 rgb(...)，所以否定期望必须同时覆盖 hex 与 rgb 两种写法，
    // 否则断言会因 jsdom 规范化而恒真（只测 hex 永远匹配不到）。
    const style = (button.attributes('style') ?? '').toLowerCase()
    expect(style).not.toMatch(BRAND_COLOR_ANY_FORM)
  })
})

/**
 * 品牌色（brandColors.ts 的四个渠道）的两种书写形态：源码 hex 与 jsdom 规范化后的 rgb()。
 * 两条都写，避免只比 hex 时被 jsdom 规范化成 rgb 而恒真。
 */
const BRAND_COLOR_ANY_FORM =
  /#00aeef|rgb\(\s*0,\s*174,\s*239\s*\)|#635bff|rgb\(\s*99,\s*91,\s*255\s*\)|#2bb741|rgb\(\s*43,\s*183,\s*65\s*\)|#14171a|rgb\(\s*20,\s*23,\s*26\s*\)/
