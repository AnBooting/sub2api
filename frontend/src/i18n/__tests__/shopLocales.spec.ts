import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

// 白标红线（PRD-03 FE-04 / AC-F1）：用户可见文案、错误文案与响应字段里
// **不得**出现上游品牌。这里把该约束固化为可执行断言，防止后续填充接口时把
// 供应商名顺手写进文案。
const FORBIDDEN_BRAND_TOKENS = ['yunmao', '云猫', 'ovo1']

function collectStrings(value: unknown, prefix = ''): { path: string; text: string }[] {
  if (typeof value === 'string') {
    return [{ path: prefix, text: value }]
  }
  if (value === null || typeof value !== 'object') {
    return []
  }
  return Object.entries(value as Record<string, unknown>).flatMap(([key, child]) =>
    collectStrings(child, prefix ? prefix + '.' + key : key),
  )
}

describe('shop locale', () => {
  it('defines the same shop keys in zh and en', () => {
    const zhShop = (zh as Record<string, any>).shop
    const enShop = (en as Record<string, any>).shop
    expect(zhShop).toBeTruthy()
    expect(enShop).toBeTruthy()

    const zhKeys = collectStrings(zhShop).map((entry) => entry.path).sort()
    const enKeys = collectStrings(enShop).map((entry) => entry.path).sort()
    expect(zhKeys).toEqual(enKeys)
  })

  it('has a non-empty message for every shop leaf', () => {
    for (const [locale, messages] of Object.entries({ zh, en })) {
      const shop = (messages as Record<string, any>).shop as Record<string, unknown>
      const leaves = collectStrings(shop)
      expect(leaves.length).toBeGreaterThan(0)
      const empty = leaves.filter((entry) => entry.text.trim() === '')
      expect(empty, locale + ' has empty shop messages').toEqual([])
    }
  })

  it('never mentions the upstream brand in user-visible copy', () => {
    for (const [locale, messages] of Object.entries({ zh, en })) {
      const shop = (messages as Record<string, any>).shop as Record<string, unknown>
      for (const entry of collectStrings(shop)) {
        const lowered = entry.text.toLowerCase()
        for (const token of FORBIDDEN_BRAND_TOKENS) {
          expect(
            lowered.includes(token),
            locale + '.' + entry.path + ' leaks brand token ' + token + ': ' + entry.text,
          ).toBe(false)
        }
      }
    }
  })

  it('covers every stable backend error reason', () => {
    const reasons = [
      'notConfigured',
      'upstreamUnavailable',
      'upstreamRejected',
      'upstreamInvalid',
      'syncUnavailable',
      'productNotFound',
      'orderNotFound',
      'idempotencyKeyRequired',
      'idempotencyConflict',
      'deliveryPending',
      'deliveryUnavailable',
      'generic',
    ]
    for (const locale of ['zh', 'en'] as const) {
      const errors = ((locale === 'zh' ? zh : en) as Record<string, any>).shop.errors
      for (const reason of reasons) {
        expect(typeof errors[reason], locale + ' missing shop.errors.' + reason).toBe('string')
      }
    }
  })
})
