/**
 * 管理端「余额 / 额度」标签的站点账单位（Credit）约束。
 *
 * 判定规则（与用户端 i18n/__tests__/userUnitLabels.spec.ts 对齐）：
 * - 站内账（用户余额、站内额度、限额、阈值、返利转入后的余额）在管理端标签上
 *   必须自带单位：zh 用「（Credit）」，en 用「(Credit)」。
 * - 上游供应商侧的账号余额 / 账号额度不是站内账，禁止标成 Credit
 *   （否则会把上游美元额度说成站内 Credit，属于口径错误）。
 */
import { describe, expect, it } from 'vitest'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'

/** 管理端必须带站内账单位的标签路径。 */
const UNIT_LABEL_PATHS: string[][] = [
  // 用户余额（管理端用户表 / 用户表单 / 余额弹窗）
  ['admin', 'users', 'columns', 'balance'],
  ['admin', 'users', 'columns', 'balancePlatformQuota'],
  ['admin', 'users', 'form', 'balanceLabel'],
  ['admin', 'users', 'adjustBalance'],
  ['admin', 'users', 'adjustmentAmount'],
  ['admin', 'users', 'depositAmount'],
  ['admin', 'users', 'withdrawAmount'],
  ['admin', 'users', 'currentBalance'],
  ['admin', 'users', 'newBalance'],
  ['admin', 'users', 'totalRecharged'],
  // 平台配额（与分组订阅限额同口径：站内账单位）
  ['admin', 'users', 'platformQuota', 'columns', 'daily'],
  ['admin', 'users', 'platformQuota', 'columns', 'weekly'],
  ['admin', 'users', 'platformQuota', 'columns', 'monthly'],
  // 返利转入后的余额（1:1 进入站内余额）
  ['admin', 'affiliates', 'records', 'balanceAfter'],
  // 兑换码类型 / 筛选器
  ['admin', 'redeem', 'types', 'balance'],
  ['admin', 'redeem', 'balance'],
  // 公告定向条件（按站内余额命中用户）
  ['admin', 'announcements', 'form', 'conditionBalance'],
  ['admin', 'announcements', 'form', 'balanceValue'],
  // 用量记录的计费类型
  ['admin', 'usage', 'billingTypeBalance'],
  // 系统设置：新用户默认余额 / 余额不足提醒阈值
  ['admin', 'settings', 'defaults', 'defaultBalance'],
  ['admin', 'settings', 'balanceNotify', 'threshold'],
  // 系统设置：注册时分配的默认平台限额（与用户表平台配额同口径）
  ['admin', 'settings', 'platformQuota', 'daily'],
  ['admin', 'settings', 'platformQuota', 'weekly'],
  ['admin', 'settings', 'platformQuota', 'monthly']
]

/** 明确属于上游供应商侧的标签：禁止标成站内 Credit。 */
const UPSTREAM_LABEL_PATHS: string[][] = [
  ['admin', 'accounts', 'ollamaCloud', 'balance'],
  ['admin', 'accounts', 'quotaLimitHint']
]

const CASES = UNIT_LABEL_PATHS.map(path => ({ path, name: path.join('.') }))
const UPSTREAM_CASES = UPSTREAM_LABEL_PATHS.map(path => ({ path, name: path.join('.') }))

function get(source: unknown, path: string[]): string | undefined {
  let current: unknown = source
  for (const key of path) {
    if (current == null || typeof current !== 'object') return undefined
    current = (current as Record<string, unknown>)[key]
  }
  return typeof current === 'string' ? current : undefined
}

describe('admin balance and quota labels carry the Credit unit', () => {
  it.each(CASES)('zh $name carries the unit', ({ path }) => {
    const value = get(zh, path)
    expect(value, `missing zh label: ${path.join('.')}`).toBeTruthy()
    // 单位写在括号里（可能是「（Credit）」也可能是「（平台配额，Credit）」）
    expect(value).toMatch(/（[^）]*Credit[^）]*）/)
  })

  it.each(CASES)('en $name carries the unit', ({ path }) => {
    const value = get(en, path)
    expect(value, `missing en label: ${path.join('.')}`).toBeTruthy()
    expect(value).toMatch(/\([^)]*Credit[^)]*\)/)
  })

  it('names the credit unit itself in the pricing settings', () => {
    expect(get(zh, ['admin', 'settings', 'pricing', 'creditUnit'])).toBe('Credit')
    expect(get(en, ['admin', 'settings', 'pricing', 'creditUnit'])).toBe('Credit')
  })

  it.each(UPSTREAM_CASES)('zh $name stays upstream-scoped and is not labelled Credit', ({ path }) => {
    const value = get(zh, path)
    expect(value, `missing zh label: ${path.join('.')}`).toBeTruthy()
    expect(value).not.toContain('Credit')
  })

  it.each(UPSTREAM_CASES)('en $name stays upstream-scoped and is not labelled Credit', ({ path }) => {
    const value = get(en, path)
    expect(value, `missing en label: ${path.join('.')}`).toBeTruthy()
    expect(value).not.toContain('Credit')
  })

  it('never repeats the unit inside the admin namespace', () => {
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
    walk(get(zh, ['admin']))
    walk(get(en, ['admin']))
  })
})
