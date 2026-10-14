import type { VideoPricePlan, VideoPricePlans, VideoBillingMode } from '@/types'

export const videoPlanModes = [
  { key: 'per_request', label: 'per_request' },
  { key: 'per_second', label: 'per_second' },
  { key: 'per_token', label: 'per_token' }
] as const

export const videoPlanResolutions = [
  { key: '480p', label: '480p' },
  { key: '720p', label: '720p' },
  { key: '1080p', label: '1080p' }
] as const

/** Known families offered by default; operators may still add others. */
export const videoPlanFamilies = [{ key: 'seedance', label: 'seedance' }] as const

export type VideoPlanForm = {
  mode: VideoBillingMode | ''
  perRequestPrice: number | string | null
  perSecondPrices: Record<string, number | string | null>
  perTokenWithoutInput: Record<string, number | string | null>
  perTokenWithInput: Record<string, number | string | null>
}
export type VideoPricePlansForm = Record<string, VideoPlanForm>

function normalizeFamily(value: string): string {
  return value.trim().toLowerCase()
}

function normalizePrice(value: unknown): number | null {
  if (value === null || value === undefined || value === '') return null
  const price = Number(value)
  return Number.isFinite(price) && price >= 0 ? price : null
}

function emptyTiers(): Record<string, number | string | null> {
  return Object.fromEntries(videoPlanResolutions.map(({ key }) => [key, null]))
}

function emptyPlan(): VideoPlanForm {
  return {
    mode: '',
    perRequestPrice: null,
    perSecondPrices: emptyTiers(),
    perTokenWithoutInput: emptyTiers(),
    perTokenWithInput: emptyTiers()
  }
}

// Keep unknown families from an existing group so a future backend catalog is not
// silently discarded when an operator edits another group setting.
export function createVideoPricePlansForm(
  plans?: VideoPricePlans | null
): VideoPricePlansForm {
  const form: VideoPricePlansForm = {}
  for (const [rawFamily, rawPlan] of Object.entries(plans ?? {})) {
    const family = normalizeFamily(rawFamily)
    if (!family || !rawPlan || typeof rawPlan !== 'object') continue
    const plan = emptyPlan()
    plan.mode = (rawPlan.mode ?? '') as VideoBillingMode | ''
    plan.perRequestPrice = normalizePrice(rawPlan.per_request_price)
    for (const [rawResolution, rawPrice] of Object.entries(rawPlan.per_second_prices ?? {})) {
      const resolution = rawResolution.trim().toLowerCase()
      const price = normalizePrice(rawPrice)
      if (price !== null) plan.perSecondPrices[resolution] = price
    }
    for (const [rawResolution, tier] of Object.entries(rawPlan.per_token_prices ?? {})) {
      const resolution = rawResolution.trim().toLowerCase()
      const withoutInput = normalizePrice(tier?.without_input_video)
      const withInput = normalizePrice(tier?.with_input_video)
      if (withoutInput !== null) plan.perTokenWithoutInput[resolution] = withoutInput
      if (withInput !== null) plan.perTokenWithInput[resolution] = withInput
    }
    form[family] = plan
  }
  for (const { key } of videoPlanFamilies) {
    form[key] ??= emptyPlan()
  }
  return form
}

// A plan is only submitted when it declares a mode; prices are dropped when they
// are empty so an unconfigured tier stays absent instead of becoming a zero price.
export function serializeVideoPricePlans(form: VideoPricePlansForm): VideoPricePlans {
  const result: VideoPricePlans = {}
  for (const [rawFamily, plan] of Object.entries(form)) {
    const family = normalizeFamily(rawFamily)
    if (!family || !plan || typeof plan !== 'object') continue
    const mode = plan.mode
    if (!mode) continue

    const out: VideoPricePlan = { mode }
    if (mode === 'per_request') {
      const price = normalizePrice(plan.perRequestPrice)
      if (price !== null) out.per_request_price = price
    }
    if (mode === 'per_second') {
      const prices: Record<string, number> = {}
      for (const [rawResolution, rawPrice] of Object.entries(plan.perSecondPrices ?? {})) {
        const resolution = rawResolution.trim().toLowerCase()
        const price = normalizePrice(rawPrice)
        if (resolution && price !== null) prices[resolution] = price
      }
      if (Object.keys(prices).length > 0) out.per_second_prices = prices
    }
    if (mode === 'per_token') {
      const prices: Record<string, { without_input_video?: number; with_input_video?: number }> = {}
      for (const { key: resolution } of videoPlanResolutions) {
        const withoutInput = normalizePrice(plan.perTokenWithoutInput?.[resolution])
        const withInput = normalizePrice(plan.perTokenWithInput?.[resolution])
        if (withoutInput === null && withInput === null) continue
        const tier: { without_input_video?: number; with_input_video?: number } = {}
        if (withoutInput !== null) tier.without_input_video = withoutInput
        if (withInput !== null) tier.with_input_video = withInput
        prices[resolution] = tier
      }
      if (Object.keys(prices).length > 0) out.per_token_prices = prices
    }
    result[family] = out
  }
  return result
}

export function videoPricePlanFamilyRows(form: VideoPricePlansForm) {
  const known = new Set<string>(videoPlanFamilies.map(({ key }) => key))
  const extra = Object.keys(form)
    .map(normalizeFamily)
    .filter((family) => family && !known.has(family))
    .sort()
    .map((key) => ({ key, label: key }))
  return [...videoPlanFamilies, ...extra]
}
