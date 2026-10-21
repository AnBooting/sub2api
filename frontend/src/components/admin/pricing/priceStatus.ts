/**
 * 管理端「价格状态」判定：把「官方内置价（可计费）」与「缺价拒绝」区分开。
 *
 * 背景（上游 0.2.17 同步的 D1 决策）：
 * - 上游给部分模型加了**该模型自身的官方内置价**（fallbackPrices，如 gemini-4-argon）：
 *   非零价，也不是「拿 GPT 兜底价顶替」，与我们的「缺价不得放行」红线**不冲突**。
 * - 因此界面不能一边写「缺价一律拒绝」，一边让这类模型按官方内置价被直接计费。
 *   管理端要把两种状态分开显示：
 *   - 官方内置价（可计费）：模型有价格来源，规则没填价时按官方内置价计费；
 *   - 缺价拒绝：模型没有任何价格来源，请求在计费前被拒绝（pricing_unavailable）。
 *
 * 本文件的判定完全对齐后端（backend/internal/service）：
 * - model_pricing_resolver.go：分组规则整体优先于渠道规则；命中规则带出价格时
 *   整条生效（applyTokenOverrides 把规则里的值合并到基础价上）。
 * - pricing_manual_validation.go（manualTokenPricingError）：CNY 手工 token 价必须
 *   写全输入/输出/缓存读/缓存写，缺任一项 → ErrModelPricingUnavailable（拒绝）。
 * - 非 token 计费模式（按次/图片/视频）没有官方内置价兜底，缺价即拒绝。
 * - USD token 规则缺字段时后端用基础价（官方目录价 / 官方内置价）补齐；
 *   基础价也没有 → 拒绝。
 */
import type { ChannelModelPricing, ModelDefaultPricing } from '@/api/admin/channels'
import { usablePriceSnapshot } from '@/utils/channelPricingSnapshot'

/** 价格可计费性：可计费 / 会被拒绝。 */
export type PriceAvailability = 'billable' | 'rejected'

/** 价格来源分类：手工规则 / 官方内置价（价卡）/ 没有价格来源。 */
export type PriceSourceKind = 'manual' | 'builtin' | 'none'

export interface ModelPriceStatus {
  /** 能否计费：可计费 = billable，会被拒绝 = rejected。 */
  availability: PriceAvailability
  /** 价格来源分类。 */
  source: PriceSourceKind
  /** 规则里没填的必需价格字段（规则完整或没有规则时为空数组）。 */
  missingFields: string[]
  /** 该模型有没有官方内置价（价卡快照可用）。 */
  builtinAvailable: boolean
  /** 后端给出的定价来源原文（manual / catalog / fallback / 官方价卡来源）。 */
  pricingSource?: string
}

/** 分组维度的定价状态汇总（不依赖探测，用于列表一眼可见）。 */
export type GroupPricingStatus = 'no_rules' | 'builtin_only' | 'incomplete' | 'complete'

export interface GroupPricingSummary {
  status: GroupPricingStatus
  /** 规则覆盖到的模型名。 */
  models: string[]
  /** 规则未填全的模型名。 */
  incompleteModels: string[]
}

/**
 * token 模式规则的必需价格字段。
 * CNY 另需缓存写价与缓存读价（与后端 manualTokenPricingError 一致；
 * USD 缺缓存价时后端用基础价的缓存价补齐，因此不算缺价）。
 */
export function requiredTokenPriceFields(currency?: string): string[] {
  const fields = ['input_price', 'output_price']
  if (currency === 'CNY') fields.push('cache_write_price', 'cache_read_price')
  return fields
}

function rulePriceValue(
  rule: Pick<ChannelModelPricing, 'billing_mode' | 'currency' | 'input_price' | 'output_price' | 'cache_write_price' | 'cache_read_price' | 'per_request_price' | 'image_input_price' | 'image_output_price'>,
  field: string
): number | null | undefined {
  switch (field) {
    case 'input_price': return rule.input_price
    case 'output_price': return rule.output_price
    case 'cache_write_price': return rule.cache_write_price
    case 'cache_read_price': return rule.cache_read_price
    case 'per_request_price': return rule.per_request_price
    default: return undefined
  }
}

/** 规则里缺失的必需价格字段。 */
export function missingRulePriceFields(rule: ChannelModelPricing): string[] {
  const mode = rule.billing_mode || 'token'
  if (mode !== 'token') {
    // 按次/图片/视频：默认单价或计费层级至少要有一个（与渠道表单校验一致）。
    const hasTier = (rule.intervals?.length ?? 0) > 0 || rule.time_pricing != null
    const hasImage = rule.image_input_price != null || rule.image_output_price != null
    if (rule.per_request_price != null || hasTier || hasImage) return []
    return ['per_request_price']
  }
  return requiredTokenPriceFields(rule.currency).filter(field => rulePriceValue(rule, field) == null)
}

/** 规则里是否填了任何价格（用于区分「手工定价」与「只登记模型」）。 */
export function ruleHasAnyPrice(rule: ChannelModelPricing): boolean {
  return [
    rule.input_price,
    rule.output_price,
    rule.cache_write_price,
    rule.cache_read_price,
    rule.image_input_price,
    rule.image_output_price,
    rule.per_request_price
  ].some(value => value != null)
}

/** 该模型有没有官方内置价（价卡快照可用即视为有）。 */
export function builtinPriceAvailable(snapshot?: ModelDefaultPricing): boolean {
  return usablePriceSnapshot(snapshot)
}

/**
 * 价卡快照的价格来源分类。
 * manual = 手工价；catalog/fallback/官方价卡来源 = 官方内置价；其余 = 未知。
 */
export function priceSourceKindOf(snapshot?: ModelDefaultPricing): PriceSourceKind {
  if (!snapshot) return 'none'
  if (snapshot.pricing_source === 'manual') return 'manual'
  if (builtinPriceAvailable(snapshot)) return 'builtin'
  return 'none'
}

/**
 * 判定一个模型在「给定规则 + 给定价卡快照」下的价格状态。
 *
 * @param rule 命中的定价规则（没有则为 undefined，表示完全由渠道价/价卡决定）
 * @param snapshot 该模型的官方价卡快照（GET /admin/channels/model-pricing）
 */
export function priceStatusOf(
  rule: ChannelModelPricing | null | undefined,
  snapshot?: ModelDefaultPricing
): ModelPriceStatus {
  const builtinAvailable = builtinPriceAvailable(snapshot)
  const pricingSource = snapshot?.pricing_source

  // 没有命中任何规则：该模型完全由渠道价 / 官方内置价（价卡）决定。
  if (!rule) {
    return {
      availability: builtinAvailable ? 'billable' : 'rejected',
      source: builtinAvailable ? 'builtin' : 'none',
      missingFields: [],
      builtinAvailable,
      pricingSource
    }
  }

  const missing = missingRulePriceFields(rule)

  // 规则填全了 → 手工价，直接可计费。
  if (missing.length === 0) {
    return {
      availability: 'billable',
      source: 'manual',
      missingFields: [],
      builtinAvailable,
      pricingSource
    }
  }

  // 按次/图片/视频计费没有官方内置价兜底：缺价就是缺价，会被拒绝。
  if ((rule.billing_mode || 'token') !== 'token') {
    return {
      availability: 'rejected',
      source: 'none',
      missingFields: missing,
      builtinAvailable,
      pricingSource
    }
  }

  // CNY 手工 token 价缺字段 → 后端 manualTokenPricingError 直接拒绝，
  // 即使有官方 CNY 价卡也不行（CNY 手工价是运营者售价，不继承官方价）。
  if ((rule.billing_mode || 'token') === 'token' && rule.currency === 'CNY') {
    return {
      availability: 'rejected',
      source: 'none',
      missingFields: missing,
      builtinAvailable,
      pricingSource
    }
  }

  // USD token 规则缺字段：后端会用基础价补齐；基础价（官方目录价/官方内置价）也没有才拒绝。
  if (builtinAvailable) {
    return {
      availability: 'billable',
      source: 'builtin',
      missingFields: missing,
      builtinAvailable,
      pricingSource
    }
  }

  return {
    availability: 'rejected',
    source: 'none',
    missingFields: missing,
    builtinAvailable,
    pricingSource
  }
}

/**
 * 汇总一个分组的定价状态（只读规则，不发请求）。
 *
 * - no_rules：没有逐模型定价规则 → 完全按渠道价/官方内置价（价卡）计费。
 * - builtin_only：规则只登记模型、没填价格 → 按官方内置价（价卡）计费。
 * - incomplete：有规则缺价字段 → 需要管理员补全（是否被拒绝取决于有没有官方内置价）。
 * - complete：规则都填全了。
 */
export function summarizeGroupPricing(rules: ChannelModelPricing[] | null | undefined): GroupPricingSummary {
  const list = rules || []
  if (list.length === 0) {
    return { status: 'no_rules', models: [], incompleteModels: [] }
  }

  const models: string[] = []
  const incompleteModels: string[] = []
  let anyPrice = false

  for (const rule of list) {
    const ruleModels = (rule.models || []).filter(Boolean)
    models.push(...ruleModels)
    if (ruleHasAnyPrice(rule)) {
      anyPrice = true
      if (missingRulePriceFields(rule).length > 0) {
        incompleteModels.push(...ruleModels)
      }
    }
  }

  if (!anyPrice) {
    return { status: 'builtin_only', models: [...new Set(models)], incompleteModels: [] }
  }
  if (incompleteModels.length > 0) {
    return { status: 'incomplete', models: [...new Set(models)], incompleteModels: [...new Set(incompleteModels)] }
  }
  return { status: 'complete', models: [...new Set(models)], incompleteModels: [] }
}
