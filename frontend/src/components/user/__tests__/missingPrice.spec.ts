import { describe, expect, it } from 'vitest'
import { isMissingPriceError, isModelPricingMissing, MISSING_PRICE_STATUS_CODE } from '../missingPrice'

describe('missing price detection', () => {
  it('recognises the backend missing-price error (503 + pricing message)', () => {
    expect(MISSING_PRICE_STATUS_CODE).toBe(503)
    expect(
      isMissingPriceError({
        status_code: 503,
        message: 'no pricing available for model: gpt-x: pricing not found',
      }),
    ).toBe(true)
    expect(
      isMissingPriceError({ status_code: 503, message: 'Model pricing is unavailable. Please retry later.' }),
    ).toBe(true)
  })

  it('does not treat other failures as missing price', () => {
    expect(isMissingPriceError({ status_code: 500, message: 'pricing not found' })).toBe(false)
    expect(isMissingPriceError({ status_code: 503, message: 'upstream connection reset' })).toBe(false)
    expect(isMissingPriceError({ status_code: 503, message: '' })).toBe(false)
    expect(isMissingPriceError({ status_code: 503 })).toBe(false)
    expect(isMissingPriceError(null)).toBe(false)
    expect(isMissingPriceError(undefined)).toBe(false)
  })

  it('flags a model without any configured price', () => {
    expect(isModelPricingMissing(null)).toBe(true)
    expect(isModelPricingMissing(undefined)).toBe(true)
    expect(
      isModelPricingMissing({
        billing_mode: 'token',
        input_price: null,
        output_price: null,
        cache_write_price: null,
        cache_read_price: null,
        image_input_price: null,
        image_output_price: null,
        per_request_price: null,
        intervals: [],
      }),
    ).toBe(true)
  })

  it('keeps a priced model as priced', () => {
    expect(
      isModelPricingMissing({
        billing_mode: 'token',
        input_price: 1.5,
        output_price: null,
        cache_write_price: null,
        cache_read_price: null,
        image_input_price: null,
        image_output_price: null,
        per_request_price: null,
        intervals: [],
      }),
    ).toBe(false)
  })

  it('treats a tiered interval price as priced', () => {
    expect(
      isModelPricingMissing({
        billing_mode: 'token',
        input_price: null,
        output_price: null,
        intervals: [{ input_price: 2, output_price: 4 }],
      }),
    ).toBe(false)
  })
})
