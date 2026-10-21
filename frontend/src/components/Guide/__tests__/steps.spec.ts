import { describe, expect, it } from 'vitest'
import { getUserSteps } from '../steps'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'

/** 用 key 本身当文案，方便断言步骤引用了哪些 i18n key。 */
const identity = (key: string) => key

const steps = getUserSteps(identity)

function resolve(source: unknown, key: string): unknown {
  return key.split('.').reduce<unknown>((acc, part) => {
    if (acc == null || typeof acc !== 'object') return undefined
    return (acc as Record<string, unknown>)[part]
  }, source)
}

describe('onboarding user tour', () => {
  it('keeps the original steps and appends the four new feature steps', () => {
    expect(steps).toHaveLength(10)
    expect(steps[0].popover?.title).toBe('onboarding.user.welcome.title')
  })

  it('appends the new steps in a fixed order', () => {
    const titles = steps.map((step) => step.popover?.title)

    expect(titles.slice(-4)).toEqual([
      'onboarding.user.recharge.title',
      'onboarding.user.keyChain.title',
      'onboarding.user.usage.title',
      'onboarding.user.help.title',
    ])
  })

  it('resolves every onboarding key in both locales', () => {
    const keys = new Set<string>()
    for (const step of steps) {
      const popover = step.popover as Record<string, unknown> | undefined
      for (const value of Object.values(popover ?? {})) {
        if (typeof value === 'string' && value.startsWith('onboarding.')) keys.add(value)
      }
    }

    expect(keys.size).toBeGreaterThan(0)
    for (const key of keys) {
      expect(resolve(zh, key), `missing zh key: ${key}`).toBeTruthy()
      expect(resolve(en, key), `missing en key: ${key}`).toBeTruthy()
    }
  })

  it('describes the real top-up and redemption routes', () => {
    const text = zh.onboarding.user.recharge.description

    expect(text).toContain('/purchase')
    expect(text).toContain('/redeem')
    expect(text).toContain('/shop')
    // 白标商品页尚未开通：文案必须如实说明
    expect(text).toContain('尚未开通')
    expect(en.onboarding.user.recharge.description).toContain('not open yet')
  })

  it('describes the group order and the high-availability fallback', () => {
    expect(zh.onboarding.user.keyChain.description).toContain('普通分组')
    expect(zh.onboarding.user.keyChain.description).toContain('高可用兜底')
    expect(en.onboarding.user.keyChain.description).toContain('high-availability fallback')
  })

  it('describes the usage record fields', () => {
    expect(zh.onboarding.user.usage.description).toContain('实际服务的分组')
    expect(zh.onboarding.user.usage.description).toContain('锁价快照')
    expect(en.onboarding.user.usage.description).toContain('pricing snapshot')
  })

  it('points at the in-site help page', () => {
    expect(zh.onboarding.user.help.description).toContain('/help')
    expect(en.onboarding.user.help.description).toContain('/help')
  })
})
