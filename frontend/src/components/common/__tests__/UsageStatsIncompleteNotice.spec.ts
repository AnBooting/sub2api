import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import UsageStatsIncompleteNotice from '../UsageStatsIncompleteNotice.vue'
import zhDashboard from '@/i18n/locales/zh/dashboard'
import enDashboard from '@/i18n/locales/en/dashboard'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

describe('UsageStatsIncompleteNotice', () => {
  it('renders as a non-error status region with the shared copy', () => {
    const wrapper = mount(UsageStatsIncompleteNotice, { global: { stubs: { Icon: true } } })
    const node = wrapper.get('[data-testid="usage-stats-incomplete"]')
    // 可重试的业务态：必须是 status 而不是 alert，且不是红色。
    expect(node.attributes('role')).toBe('status')
    expect(node.classes().join(' ')).toContain('amber')
    expect(node.classes().join(' ')).not.toContain('rose')
    expect(node.text()).toContain('usageStatsIncomplete.title')
    expect(node.text()).toContain('usageStatsIncomplete.description')
    wrapper.unmount()
  })

  it('allows overriding the copy and hiding the description', () => {
    const wrapper = mount(UsageStatsIncompleteNotice, {
      props: { title: 'custom title', description: null },
      global: { stubs: { Icon: true } },
    })
    const node = wrapper.get('[data-testid="usage-stats-incomplete"]')
    expect(node.text()).toContain('custom title')
    expect(node.text()).not.toContain('usageStatsIncomplete.description')
    wrapper.unmount()
  })

  it('ships both locales with non-empty title/description/short copy', () => {
    for (const locale of [zhDashboard, enDashboard]) {
      const block = locale.usageStatsIncomplete
      expect(block.title.trim().length).toBeGreaterThan(0)
      expect(block.description.trim().length).toBeGreaterThan(0)
      expect(block.short.trim().length).toBeGreaterThan(0)
      // 必须让用户明白这是「暂时/会恢复」，而不是永久失败。
      expect(block.title).toMatch(/暂|暂时|暂不|temporar|hidden for now/i)
    }
  })
})
