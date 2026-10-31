import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import SmtpNotConfiguredNotice from '../SmtpNotConfiguredNotice.vue'
import zhSettings from '@/i18n/locales/zh/admin/settings'
import enSettings from '@/i18n/locales/en/admin/settings'

// 与仓库既有组件测试一致：useI18n 换成立即回显 key 的桩，
// 标题/正文是否真有文案在下面的 locale 断言里单独验证。
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

describe('SmtpNotConfiguredNotice', () => {
  it('renders a blocking, screen-reader-visible notice with both copy keys', () => {
    const wrapper = mount(SmtpNotConfiguredNotice, { global: { stubs: { Icon: true } } })

    const notice = wrapper.get('[data-testid="smtp-not-configured"]')
    expect(notice.attributes('role')).toBe('alert')
    expect(notice.text()).toContain('admin.settings.smtp.notConfiguredTitle')
    expect(notice.text()).toContain('admin.settings.smtp.notConfiguredHint')
    wrapper.unmount()
  })

  it('ships both locales with non-empty copy that is distinct from the panel description', () => {
    for (const locale of [zhSettings, enSettings]) {
      const smtp = locale.settings.smtp
      expect(smtp.notConfiguredTitle.trim().length).toBeGreaterThan(0)
      expect(smtp.notConfiguredHint.trim().length).toBeGreaterThan(0)
      // 阻断文案不能和面板本身的 description 重复，否则用户看不出这是「出问题了」。
      expect(smtp.notConfiguredHint).not.toBe(smtp.description)
      expect(smtp.notConfiguredTitle).not.toBe(smtp.title)
    }
  })
})
