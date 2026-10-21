import { describe, expect, it } from 'vitest'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'

/** 这些用户侧标签必须自带单位（Credit），避免与源币种金额混淆。 */
const UNIT_LABEL_PATHS: string[][] = [
  ['dashboard', 'balance'],
  ['dashboard', 'platformQuota', 'title'],
  ['keys', 'quota'],
  ['keys', 'quotaLimit'],
  ['keys', 'quotaAmount'],
  ['keys', 'quotaUsed'],
  ['keys', 'rateLimit5h'],
  ['keys', 'rateLimit1d'],
  ['keys', 'rateLimit7d'],
  ['profile', 'accountBalance'],
  ['redeem', 'currentBalance'],
  ['redeem', 'newBalance'],
  ['payment', 'currentBalance'],
  ['payment', 'creditedBalance'],
  ['payment', 'rechargeBonus', 'amountLabel'],
  ['payment', 'rechargeBonus', 'amountLabelWithPercent'],
  ['payment', 'orders', 'creditedAmount'],
  ['payment', 'orders', 'bonusAmount'],
  ['payment', 'planCard', 'dailyLimit'],
  ['payment', 'planCard', 'weeklyLimit'],
  ['payment', 'planCard', 'monthlyLimit'],
  ['payment', 'planCard', 'quota'],
]

const CASES = UNIT_LABEL_PATHS.map((path) => ({ path, name: path.join('.') }))

function get(source: unknown, path: string[]): string | undefined {
  let current: unknown = source
  for (const key of path) {
    if (current == null || typeof current !== 'object') return undefined
    current = (current as Record<string, unknown>)[key]
  }
  return typeof current === 'string' ? current : undefined
}

describe('user-side balance and quota labels carry the Credit unit', () => {
  it.each(CASES)('zh $name carries the unit', ({ path }) => {
    const value = get(zh, path)
    expect(value, `missing zh label: ${path.join('.')}`).toBeTruthy()
    expect(value).toContain('（Credit）')
  })

  it.each(CASES)('en $name carries the unit', ({ path }) => {
    const value = get(en, path)
    expect(value, `missing en label: ${path.join('.')}`).toBeTruthy()
    expect(value).toContain('(Credit)')
  })

  it('never repeats the unit', () => {
    const walk = (node: unknown, path: string[] = []): void => {
      if (typeof node === 'string') {
        expect(node, `doubled unit at ${path.join('.')}`).not.toMatch(/（Credit）（Credit）/g)
        expect(node, `doubled unit at ${path.join('.')}`).not.toMatch(/\(Credit\)\(Credit\)/g)
        return
      }
      if (node && typeof node === 'object') {
        for (const [key, value] of Object.entries(node as Record<string, unknown>)) {
          walk(value, [...path, key])
        }
      }
    }
    walk(zh)
    walk(en)
  })
})
