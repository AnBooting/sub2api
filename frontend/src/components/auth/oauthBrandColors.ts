/**
 * OAuth 提供方品牌色 —— 第三方商标色，与支付渠道品牌色同属令牌契约第 6 条例外。
 *
 * 这些是 Google 四色 G、LinuxDo 橙、DingTalk 蓝等官方标识色，改色或近似色会破坏
 * 品牌识别（商标问题）。因此集中在本文件，组件里只引用常量；新增 OAuth 提供方
 * 时也只改这里。
 */
export const OAUTH_BRAND_COLOR = {
  googleBlue: '#4285F4',
  googleGreen: '#34A853',
  googleYellow: '#FBBC05',
  googleRed: '#EA4335',
  linuxdoOrange: 'rgb(233, 84, 32)',
  linuxdoShell: '#EFEFEF',
  linuxdoBeak: '#FEB005',
  linuxdoEye: '#1D1D1F',
  dingtalkBlue: '#1677FF',
} as const
