/**
 * 支付渠道品牌色 —— 令牌契约第 6 条约定的唯一例外。
 *
 * 这些是第三方商标色（Stripe / Airwallex / 支付宝 / 微信），必须保真：
 * 改色或近似色会破坏品牌识别，属于商标问题而不是设计问题。因此集中在本文件，
 * .vue 里只引用常量、不散落色值；新增渠道时也只改这里。
 */
export const PAYMENT_BRAND_COLOR = {
  stripe: '#635bff',
  airwallex: '#14171a',
  alipay: '#00aeef',
  wxpay: '#2bb741',
} as const

export type PaymentBrandKey = keyof typeof PAYMENT_BRAND_COLOR
