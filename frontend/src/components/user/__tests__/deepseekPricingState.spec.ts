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
      nextSwitchAt: undefined,
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
    ).toEqual({
      state: 'none',
      label: undefined,
      timezone: undefined,
      multiplier: undefined,
      nextSwitchAt: undefined,
    })
  })

  it('reads the next switch time when the backend sends it', () => {
    const state = readDeepSeekPricingState({
      deepseek_pricing_state: {
        state: 'peak',
        label: '峰价时段 09:00-12:00',
        timezone: 'Asia/Shanghai',
        multiplier: 2,
        next_switch_at: '2026-10-22T09:00:00+08:00',
      },
    })

    expect(state?.nextSwitchAt).toBe('2026-10-22T09:00:00+08:00')
  })

  it('treats a missing / null / empty next switch time as absent', () => {
    expect(
      readDeepSeekPricingState({ deepseek_pricing_state: { state: 'peak' } })?.nextSwitchAt,
    ).toBeUndefined()
    expect(
      readDeepSeekPricingState({
        deepseek_pricing_state: { state: 'peak', next_switch_at: null },
      })?.nextSwitchAt,
    ).toBeUndefined()
    expect(
      readDeepSeekPricingState({
        deepseek_pricing_state: { state: 'peak', next_switch_at: '   ' },
      })?.nextSwitchAt,
    ).toBeUndefined()
    expect(
      readDeepSeekPricingState({
        deepseek_pricing_state: { state: 'none', next_switch_at: null },
      })?.nextSwitchAt,
    ).toBeUndefined()
  })

  it('drops an invalid next switch time instead of guessing one', () => {
    expect(
      readDeepSeekPricingState({
        deepseek_pricing_state: { state: 'peak', next_switch_at: 'not-a-date' },
      })?.nextSwitchAt,
    ).toBeUndefined()
    expect(
      readDeepSeekPricingState({
        deepseek_pricing_state: { state: 'peak', next_switch_at: 12345 },
      })?.nextSwitchAt,
    ).toBeUndefined()
  })
})
