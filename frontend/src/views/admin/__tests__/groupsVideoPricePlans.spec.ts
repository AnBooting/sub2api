import { describe, expect, it } from 'vitest'
import {
  createVideoPricePlansForm,
  serializeVideoPricePlans,
  videoPricePlanFamilyRows
} from '../groupsVideoPricePlans'

describe('groupsVideoPricePlans', () => {
  it('keeps known families and preserves unknown ones from an existing group', () => {
    const form = createVideoPricePlansForm({
      seedance: { mode: 'per_token', per_token_prices: { '720p': { without_input_video: 4 } } },
      'minimax-hailuo': { mode: 'per_request', per_request_price: 2 }
    })
    expect(Object.keys(form).sort()).toEqual(['minimax-hailuo', 'seedance'])
    expect(form.seedance.mode).toBe('per_token')
    expect(form.seedance.perTokenWithoutInput['720p']).toBe(4)
    expect(form['minimax-hailuo'].perRequestPrice).toBe(2)
    expect(videoPricePlanFamilyRows(form).map((row) => row.key)).toContain('minimax-hailuo')
  })

  it('drops a plan without a mode so an unconfigured family is never submitted', () => {
    const form = createVideoPricePlansForm(null)
    form.seedance.perRequestPrice = 3
    expect(serializeVideoPricePlans(form)).toEqual({})
  })

  it('serializes only the tiers relevant to the declared mode', () => {
    const form = createVideoPricePlansForm(null)
    form.seedance.mode = 'per_request'
    form.seedance.perRequestPrice = 3
    // A per_second tier must not leak into a per_request plan.
    form.seedance.perSecondPrices['720p'] = 9
    expect(serializeVideoPricePlans(form)).toEqual({
      seedance: { mode: 'per_request', per_request_price: 3 }
    })
  })

  it('keeps per-token tiers separate for with/without input video', () => {
    const form = createVideoPricePlansForm(null)
    form.seedance.mode = 'per_token'
    form.seedance.perTokenWithoutInput['1080p'] = 46
    form.seedance.perTokenWithInput['1080p'] = 77
    expect(serializeVideoPricePlans(form)).toEqual({
      seedance: {
        mode: 'per_token',
        per_token_prices: { '1080p': { without_input_video: 46, with_input_video: 77 } }
      }
    })
  })

  it('omits empty tiers instead of turning them into a zero price', () => {
    const form = createVideoPricePlansForm(null)
    form.seedance.mode = 'per_second'
    form.seedance.perSecondPrices['480p'] = 0.11
    form.seedance.perSecondPrices['720p'] = ''
    form.seedance.perSecondPrices['1080p'] = null
    expect(serializeVideoPricePlans(form)).toEqual({
      seedance: { mode: 'per_second', per_second_prices: { '480p': 0.11 } }
    })
  })

  it('round-trips a fully configured plan', () => {
    const plans = {
      seedance: {
        mode: 'per_token' as const,
        per_token_prices: {
          '480p': { without_input_video: 70, with_input_video: 42 },
          '1080p': { without_input_video: 77, with_input_video: 46 }
        }
      }
    }
    expect(serializeVideoPricePlans(createVideoPricePlansForm(plans))).toEqual(plans)
  })
})
