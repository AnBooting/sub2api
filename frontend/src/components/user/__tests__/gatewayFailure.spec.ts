import { describe, expect, it } from 'vitest'
import {
  classifyGatewayFailure,
  gatewayFailureI18nKey,
  MODEL_NOT_FOUND_STATUS_CODE,
  PRICING_UNAVAILABLE_STATUS_CODE,
} from '../gatewayFailure'

describe('gateway failure classification (F-06 口径)', () => {
  it('pins the HTTP codes the backend uses', () => {
    expect(PRICING_UNAVAILABLE_STATUS_CODE).toBe(503)
    expect(MODEL_NOT_FOUND_STATUS_CODE).toBe(404)
  })

  it('prefers a structured reason over the message', () => {
    expect(classifyGatewayFailure({ status: 404, reason: 'model_not_found', message: 'whatever' })).toBe('model_not_found')
    expect(classifyGatewayFailure({ status: 503, reason: 'pricing_unavailable', message: 'whatever' })).toBe('pricing_unavailable')
    expect(classifyGatewayFailure({ reason: 'MODEL_NOT_FOUND' })).toBe('model_not_found')
    expect(classifyGatewayFailure({ reason: 'PRICING_UNAVAILABLE' })).toBe('pricing_unavailable')
  })

  it('classifies by message when no structured code is present', () => {
    expect(classifyGatewayFailure({ status_code: 503, message: 'no pricing available for model: x: pricing not found' })).toBe('pricing_unavailable')
    expect(classifyGatewayFailure({ status_code: 503, message: 'Model pricing is unavailable. Please retry later.' })).toBe('pricing_unavailable')
    expect(classifyGatewayFailure({ status: 404, message: 'model_not_found: gpt-9' })).toBe('model_not_found')
    expect(classifyGatewayFailure({ message: 'no available channel for model gpt-9' })).toBe('model_not_found')
  })

  it('keeps the two modes distinct even when both are 404/503-ish', () => {
    // 关键回归点：不能把「换模型」和「等补价」混成同一个提示。
    const pricing = classifyGatewayFailure({ status: 503, message: 'pricing not found' })
    const notFound = classifyGatewayFailure({ status: 404, message: 'model not found' })
    expect(pricing).toBe('pricing_unavailable')
    expect(notFound).toBe('model_not_found')
    expect(pricing).not.toBe(notFound)
  })

  it('does not misclassify ordinary failures', () => {
    expect(classifyGatewayFailure(null)).toBeNull()
    expect(classifyGatewayFailure(undefined)).toBeNull()
    expect(classifyGatewayFailure({ status: 500, message: 'upstream connection reset' })).toBeNull()
    expect(classifyGatewayFailure({ status: 429, message: 'rate limited' })).toBeNull()
    expect(classifyGatewayFailure({ status_code: 503, message: 'upstream connection reset' })).toBeNull()
    // 404 且没有 message：不足以定性，不猜。
    expect(classifyGatewayFailure({ status: 404 })).toBeNull()
  })

  it('falls back to 503-only when the message is missing, because pricing failures are always 503', () => {
    expect(classifyGatewayFailure({ status_code: 503 })).toBe('pricing_unavailable')
    expect(classifyGatewayFailure({ status: 503, message: '' })).toBe('pricing_unavailable')
  })

  it('ignores a numeric code (it is the HTTP status, not a business code)', () => {
    expect(classifyGatewayFailure({ code: 404, message: '' })).toBeNull()
  })

  it('maps each kind to its own i18n namespace', () => {
    expect(gatewayFailureI18nKey('pricing_unavailable')).toBe('gatewayFailure.pricingUnavailable')
    expect(gatewayFailureI18nKey('model_not_found')).toBe('gatewayFailure.modelNotFound')
  })
})
