import { describe, expect, it } from 'vitest'
import { readDeepSeekPricingState } from '../deepseekPricingState'

describe('readDeepSeekPricingState', () => {
  it('reads a valid state', () => {
    expect(
      readDeepSeekPricingState({
        deepseek_pricing_state: {
          state: 'peak',
          label: '峰价时段 09:00-12:00',
          timezone: 'Asia/Shanghai',
          multiplier: 1.5,
        },
      }),
    ).toEqual({
      state: 'peak',
      label: '峰价时段 09:00-12:00',
      timezone: 'Asia/Shanghai',
      multiplier: 1.5,
    })
  })

  it('returns null when the backend does not send the field', () => {
    expect(readDeepSeekPricingState({ id: 1 })).toBeNull()
    expect(readDeepSeekPricingState(null)).toBeNull()
    expect(readDeepSeekPricingState(undefined)).toBeNull()
    expect(readDeepSeekPricingState('nope')).toBeNull()
  })

  it('returns null for an unknown state', () => {
    expect(readDeepSeekPricingState({ deepseek_pricing_state: { state: 'weird' } })).toBeNull()
    expect(readDeepSeekPricingState({ deepseek_pricing_state: { state: 42 } })).toBeNull()
  })

  it('drops unusable label/timezone/multiplier values', () => {
    expect(
      readDeepSeekPricingState({
        deepseek_pricing_state: { state: 'none', label: '  ', timezone: 5, multiplier: 'x' },
      }),
    ).toEqual({ state: 'none', label: undefined, timezone: undefined, multiplier: undefined })
  })
})
