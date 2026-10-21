import { describe, expect, it } from 'vitest'

import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

/**
 * 管理端价格口径文案契约（上游 0.2.17 同步的 D1 / D3）。
 *
 * D1：上游给部分模型加了**该模型自身的官方内置价**（fallbackPrices，如 gemini-4-argon），
 * 非零价、也不是拿别的模型兜底，与「缺价不得放行」红线不冲突。管理端文案必须把
 * 「官方内置价（可计费）」与「没有任何价格来源（拒绝）」分开说，不能再一刀切。
 *
 * D3：GPT-5.5 只对 ChatGPT 订阅账号（OAuth / 影子账号）下线，API Key 上游仍可用，
 * 任何「已下架」表述都要按渠道区分。
 */
describe('管理端价格口径文案', () => {
  it('分组逐模型定价说明区分官方内置价与缺价拒绝', () => {
    const hint = zh.admin.groups.modelPricing.priceSourceHint
    expect(hint).toContain('官方内置价')
    expect(hint).toContain('pricing_unavailable')
    expect(hint).not.toContain('缺价一律拒绝')

    const enHint = en.admin.groups.modelPricing.priceSourceHint
    expect(enHint).toContain('official built-in price')
    expect(enHint).toContain('pricing_unavailable')
  })

  it('定价状态提示语说明「有官方内置价可计费」与「无价格来源被拒绝」两种结果', () => {
    const incomplete = zh.admin.groups.pricingStatus.tip.incomplete
    expect(incomplete).toContain('官方内置价')
    expect(incomplete).toContain('pricing_unavailable')

    const builtinOnly = zh.admin.groups.pricingStatus.tip.builtinOnly
    expect(builtinOnly).toContain('官方内置价')
    expect(builtinOnly).toContain('被拒绝')

    const enIncomplete = en.admin.groups.pricingStatus.tip.incomplete
    expect(enIncomplete).toContain('official built-in price')
    expect(enIncomplete).toContain('pricing_unavailable')
  })

  it('自动定价缺价提示按官方内置价与拒绝两种口径说明', () => {
    const missing = zh.admin.channels.form.autoPricingMissing
    expect(missing).toContain('官方内置价')
    expect(missing).toContain('pricing_unavailable')

    const enMissing = en.admin.channels.form.autoPricingMissing
    expect(enMissing).toContain('official built-in price')
    expect(enMissing).toContain('pricing_unavailable')
  })

  it('GPT-5.5 的说明按渠道区分（订阅账号下线 / API Key 上游可用）', () => {
    const hint = zh.admin.groups.modelAllowlist.gpt55RetirementHint
    expect(hint).toContain('ChatGPT 订阅账号')
    expect(hint).toContain('API Key')
    expect(hint).not.toContain('GPT-5.5 已下架')

    // 退役时间以代码为准（backend/internal/service/openai_model_retirement.go
    // 与上游 docs/OPENAI_GPT55_RETIREMENT.md 都是 2026-10-14 19:00 UTC），
    // 中英文案必须写同一个日期，且都要写明只影响订阅账号。
    expect(hint).toContain('2026-10-14')
    expect(hint).toContain('SetupToken')

    const enHint = en.admin.groups.modelAllowlist.gpt55RetirementHint
    expect(enHint).toContain('API-key upstreams')
    expect(enHint).toContain('404 model_not_found')
    expect(enHint).toContain('2026-10-14')
    expect(enHint).toContain('SetupToken')
  })

  it('分组高峰时段说明写明判定时区与作用范围', () => {
    const hint = zh.admin.groups.peakWindow.hint
    expect(hint).toContain('Asia/Shanghai')
    expect(hint).toContain('token')

    const enHint = en.admin.groups.peakWindow.hint
    expect(enHint).toContain('Asia/Shanghai')
    expect(enHint).toContain('token')
  })
})
