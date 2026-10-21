import { describe, it, expect, beforeEach } from 'vitest'
import { buildEmbeddedUrl, detectTheme, EMBED_AUTH_TOKEN_MESSAGE_TYPE } from '../embedded-url'

describe('buildEmbeddedUrl', () => {
  beforeEach(() => {
    // jsdom 默认地址，供 src_host/src_url 断言使用
    window.history.replaceState({}, '', '/user/purchase')
  })

  it('appends user, theme, lang, ui mode and source context', () => {
    const result = buildEmbeddedUrl(
      'https://pay.example.com/checkout?plan=pro',
      42,
      'dark',
      'zh-CN',
    )

    const url = new URL(result)
    expect(url.searchParams.get('plan')).toBe('pro')
    expect(url.searchParams.get('user_id')).toBe('42')
    expect(url.searchParams.get('theme')).toBe('dark')
    expect(url.searchParams.get('lang')).toBe('zh-CN')
    expect(url.searchParams.get('ui_mode')).toBe('embedded')
    expect(url.searchParams.get('src_host')).toBe(window.location.origin)
  })

  it('never leaks the auth token into the URL', () => {
    const result = buildEmbeddedUrl(
      'https://pay.example.com/checkout',
      7,
      'light',
      'en',
    )

    const url = new URL(result)
    // 令牌会进入浏览器历史、Referer 与访问日志，任何情况下都不得出现在 URL 中。
    expect(url.searchParams.has('token')).toBe(false)
    expect(url.searchParams.has('access_token')).toBe(false)
    expect(result).not.toContain('token=')
  })

  it('omits optional params when they are empty', () => {
    const result = buildEmbeddedUrl('https://pay.example.com/checkout', undefined, 'light')

    const url = new URL(result)
    expect(url.searchParams.get('theme')).toBe('light')
    expect(url.searchParams.get('ui_mode')).toBe('embedded')
    expect(url.searchParams.has('user_id')).toBe(false)
    expect(url.searchParams.has('lang')).toBe(false)
  })

  it('returns original string for invalid url input', () => {
    expect(buildEmbeddedUrl('not a url', 1)).toBe('not a url')
  })

  it('exposes a stable postMessage type for token delivery', () => {
    expect(EMBED_AUTH_TOKEN_MESSAGE_TYPE).toBe('sub2api:embed-auth-token')
  })

  it('detects dark mode from document root class', () => {
    document.documentElement.classList.add('dark')
    expect(detectTheme()).toBe('dark')
  })
})
