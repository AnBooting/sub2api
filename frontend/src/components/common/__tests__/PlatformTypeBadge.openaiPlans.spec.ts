import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import type { AccountPlatform } from '@/types'
import PlatformTypeBadge from '../PlatformTypeBadge.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

function mountPlan(platform: AccountPlatform, planType: string) {
  return mount(PlatformTypeBadge, {
    props: { platform, type: 'oauth', planType },
  })
}

describe('PlatformTypeBadge ChatGPT plan tiers', () => {
  it('labels pro / chatgptpro as Pro 200 with the Pro color', () => {
    for (const planType of ['pro', 'chatgptpro', 'PRO']) {
      const wrapper = mountPlan('openai', planType)

      expect(wrapper.text()).toContain('Pro 200')
      // 付费档位 = 信号色 primary（钴蓝）。旧的装饰性色族已从调色板删除。
      expect(wrapper.html()).toContain('bg-primary-100')
    }
  })

  it('labels prolite as Pro 100 sharing the Pro color', () => {
    for (const planType of ['prolite', 'PROLITE', 'pro_lite']) {
      const wrapper = mountPlan('openai', planType)

      expect(wrapper.text()).toContain('Pro 100')
      expect(wrapper.html()).toContain('bg-primary-100')
      expect(wrapper.text()).not.toContain('Pro 200')
    }
  })

  it('labels team as Business with the Team color', () => {
    const wrapper = mountPlan('openai', 'team')

    expect(wrapper.text()).toContain('Business')
    expect(wrapper.html()).toContain('bg-primary-100')
  })

  it('labels self_serve_business_prolite as Business Premium sharing the Team color', () => {
    for (const planType of ['self_serve_business_prolite', 'selfservebusinessprolite']) {
      const wrapper = mountPlan('openai', planType)

      expect(wrapper.text()).toContain('Business Premium')
      expect(wrapper.html()).toContain('bg-primary-100')
      expect(wrapper.text()).not.toContain('self_serve_business_prolite')
    }
  })

  it('keeps plus, free and abnormal labels unchanged', () => {
    const plus = mountPlan('openai', 'plus')
    expect(plus.text()).toContain('Plus')
    expect(plus.html()).toContain('bg-primary-100')

    const free = mountPlan('openai', 'free')
    expect(free.text()).toContain('Free')
    expect(free.html()).toContain('bg-gray-100')

    const abnormal = mountPlan('openai', 'abnormal')
    expect(abnormal.text()).toContain('admin.accounts.subscriptionAbnormal')
    // 异常/断档 = danger（氧化红）。
    expect(abnormal.html()).toContain('bg-danger-100')
  })

  it('falls back to the raw value for unknown plans', () => {
    const wrapper = mountPlan('openai', 'future_sku')

    expect(wrapper.text()).toContain('future_sku')
    // 未知档位不得套用付费档位信号色，回落成平台/认证方式色。
    expect(wrapper.html()).not.toContain('bg-primary-100')
  })

  it('does not apply the ChatGPT tier naming to other platforms', () => {
    // Antigravity 的 Pro 与 Grok 的 pro 是各自产品线的档位，不能显示成 Pro 200。
    for (const platform of ['antigravity', 'grok'] as AccountPlatform[]) {
      const wrapper = mountPlan(platform, 'pro')

      expect(wrapper.text()).toContain('Pro')
      expect(wrapper.text()).not.toContain('Pro 200')
    }

    const antigravityTeam = mountPlan('antigravity', 'team')
    expect(antigravityTeam.text()).toContain('Team')
    expect(antigravityTeam.text()).not.toContain('Business')
  })
})

describe('new Codex subscription labels', () => {
  it.each([
    ['promax', 'Pro 500', 'bg-primary-100'],
    ['go', 'Go', ''],
    ['ent26', 'Enterprise', 'bg-primary-100'],
    ['enterprise_cbp_automation', 'Enterprise (Automation)', 'bg-primary-100'],
    ['self_serve_business_usage_based', 'Business', 'bg-primary-100'],
    ['business', 'Enterprise', 'bg-primary-100'],
    ['edu_plus', 'Edu Plus', 'bg-primary-100'],
    ['edu_pro', 'Edu Pro', 'bg-primary-100']
  ])('displays %s', (sku, label, color) => {
    const wrapper = mountPlan('openai', sku)
    expect(wrapper.text()).toContain(label)
    if (color) expect(wrapper.html()).toContain(color)
  })
})
