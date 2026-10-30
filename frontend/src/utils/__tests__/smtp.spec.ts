import { describe, expect, it } from 'vitest'
import { isSmtpConfigured } from '../smtp'

describe('isSmtpConfigured (I-01 契约)', () => {
  it('trusts the server-declared flag', () => {
    expect(isSmtpConfigured({ smtp_configured: true })).toBe(true)
    expect(isSmtpConfigured({ smtp_configured: false })).toBe(false)
  })

  it('degrades a missing field to "not configured" instead of assuming it works', () => {
    // 后端尚未上线该字段时，发码会以 EMAIL_NOT_CONFIGURED 失败；
    // 前端必须按「不可用」阻断，而不是乐观放行让管理员点一次才知道。
    expect(isSmtpConfigured({})).toBe(false)
    expect(isSmtpConfigured(null)).toBe(false)
    expect(isSmtpConfigured(undefined)).toBe(false)
  })

  it('does not treat a non-boolean truthy value as configured', () => {
    expect(isSmtpConfigured({ smtp_configured: 'true' as unknown as boolean })).toBe(false)
    expect(isSmtpConfigured({ smtp_configured: 1 as unknown as boolean })).toBe(false)
    expect(isSmtpConfigured({ smtp_configured: null })).toBe(false)
  })

  it('does not infer configuration from form drafts (host/from email present)', () => {
    // 关键回归点：管理员刚填了 host 但还没保存，判定必须仍然是「未配置」。
    const draft = { smtp_host: 'smtp.example.com', smtp_from_email: 'a@b.c' } as Record<string, unknown>
    expect(isSmtpConfigured(draft)).toBe(false)
  })
})
