/**
 * 管理端「价格审计面板」的判定逻辑（纯函数层）。
 *
 * 这一层只做「数据 → 判定/展示值」的计算，不发请求、不依赖 UI，
 * 面板与测试共用同一套判定，避免测试里重写一份「看起来一样」的逻辑：
 *  - 缺价判定复用生产函数 usablePriceSnapshot（默认价卡快照是否构成完整价格）
 *  - 展示金额复用生产函数 perTokenToMTok（per-token → 每百万 token）
 *
 * ── 取价优先级（与后端 model_pricing_resolver.go 对齐）────────────────────
 *   分组 model_pricing > 渠道 pricing > 内置默认价卡（LiteLLM 目录价）。
 *
 * ── 判定语义（写清楚理由，避免被改错）────────────────────────────────────
 *   1. 规则「命中」模型的方式与后端一致：先精确名，再通配符前缀
 *      （后端 matchGroupModelPricing / lookupPricingAcrossPlatforms）。
 *      一个作用域（分组/渠道）内只有一条规则对某模型生效。
 *   2. 分组规则命中 → 整条分组规则生效，渠道规则被遮蔽
 *      （后端命中分组定价即返回，不再看渠道定价）。
 *      渠道规则只在该渠道绑定的分组「没有自己的分组规则」时生效。
 *   3. 规则「整条生效」：命中后由这条规则决定定价，缺哪个必需字段就是缺什么；
 *      默认价卡不会把它补上。因此
 *      **分组规则不完整 + 默认价卡价格完整，也必须判为缺价**。
 *   4. 例外：USD token 规则一个价格都没填时，后端 hasChannelTokenOverrides
 *      判定它「不是价格覆盖、只是登记模型」，运行时定价仍由默认价卡兜底
 *      → 这时缺不缺价看价卡快照（usablePriceSnapshot）。
 *   5. 价卡快照本身不可用时：found===false → 缺「全部必需价格字段」；
 *      snapshot_ready===false → 文案表达「价卡尚未就绪」。
 *
 *   必需价格字段按计费模式判定：
 *   - token 模式：input_price、output_price；币种 CNY 时另需
 *     cache_write_price、cache_read_price（与 usablePriceSnapshot 的 CNY 判定一致）
 *   - per_request / image / video 模式：per_request_price
 *     （默认价卡快照没有这个字段，所以这三种计费模式只有手工规则能兜住）
 */
import type { Channel, ChannelModelPricing, ModelDefaultPricing } from '@/api/admin/channels'
import type { AdminGroup } from '@/types'
import type { BillingMode } from '@/constants/channel'
import { perTokenToMTok } from '@/components/admin/channel/types'
import { usablePriceSnapshot } from '@/utils/channelPricingSnapshot'

export type PricingScopeKind = 'channel' | 'group'

/** 一个定价作用域（一个分组或一个渠道）及其定价规则。 */
export interface PricingScope {
  scope: PricingScopeKind
  scopeId: number
  scopeName: string
  /** 渠道作用域：该渠道关联的分组 id（用于判断渠道规则是否被分组规则遮蔽）。 */
  boundGroupIds: number[]
  rules: ChannelModelPricing[]
}

/** 定价数据快照：作用域 + 候选模型（显式模型名并集，跳过通配符）。 */
export interface PricingAuditData {
  scopes: PricingScope[]
  /** 候选模型：渠道规则 + 分组规则里出现过的模型名并集，跳过含 '*' 的通配符名。 */
  models: string[]
}

/** 命中某个模型的一条定价规则。 */
export interface PricingRuleRef {
  scope: PricingScopeKind
  scopeId: number
  scopeName: string
  platform: string
  rule: ChannelModelPricing
}

export type MissingPriceKind = 'rule' | 'card_not_found' | 'card_not_ready' | 'probe_failed'

/** 缺价清单的一行。 */
export interface MissingPriceRow {
  model: string
  /** 缺价发生在哪个作用域：channel/group=该处规则缺价；card=完全由默认价卡决定。 */
  scope: PricingScopeKind | 'card'
  /** scope=channel/group 时的 id（card 时为 undefined）。 */
  scopeId?: number
  scopeName: string
  platform: string
  /** 缺失的必需价格字段（字段名）；规则来源时来自规则本身，价卡来源时来自快照。 */
  missingFields: string[]
  /** 定价来源：rule=手工规则缺字段；card=定价权在默认价卡，但价卡没给出完整价。 */
  priceSource: 'rule' | 'card'
  kind: MissingPriceKind
  probeFailed: boolean
}

/** 「去定价」跳转目标（由面板 emit，交给 ChannelsView 处理，组件自己不改路由）。 */
export interface PricingEditTarget {
  channelId?: number
  groupId?: number
  platform?: string
  model?: string
}

/** 价格来源：手工=规则里填了价；缺省=规则存在但没填价；价卡=由默认价卡兜底。 */
export type PriceSourceKind = 'manual' | 'default' | 'card'

/** 价格总表的一行（每行对应一条规则 × 规则里的一个模型名）。 */
export interface PriceTableRow {
  model: string
  currency: string
  billingMode: BillingMode
  unit: BillingMode
  inputPrice: number | null
  outputPrice: number | null
  cacheReadPrice: number | null
  cacheWritePrice: number | null
  imagePrice: number | null
  perRequestPrice: number | null
  source: PriceSourceKind
  scope: PricingScopeKind
  scopeId: number
  scopeName: string
  platform: string
}

/** 探测结果（价格总表与缺价清单共用）。 */
export interface ProbeResults {
  results: Record<string, ModelDefaultPricing>
  failures: Record<string, string>
}

export const EMPTY_PROBE_RESULTS: ProbeResults = { results: {}, failures: {} }

/** 通配符模型名无法用单模型接口探测，跳过逐模型检测。 */
export function isWildcardModel(model: string): boolean {
  return model.includes('*')
}

/**
 * 组装审计数据：作用域（分组/渠道）+ 候选模型名并集。
 * 候选模型跳过通配符名（单模型探测接口无法探测通配符）。
 */
export function collectPricingAuditData(
  channels: Channel[],
  groups: AdminGroup[]
): PricingAuditData {
  const scopes: PricingScope[] = []
  const models = new Set<string>()

  for (const group of groups || []) {
    const rules = group.model_pricing || []
    scopes.push({
      scope: 'group',
      scopeId: group.id,
      scopeName: group.name,
      boundGroupIds: [],
      rules
    })
    for (const rule of rules) {
      for (const model of rule.models || []) {
        if (!model || isWildcardModel(model)) continue
        models.add(model)
      }
    }
  }

  for (const channel of channels || []) {
    const rules = channel.model_pricing || []
    scopes.push({
      scope: 'channel',
      scopeId: channel.id,
      scopeName: channel.name,
      boundGroupIds: channel.group_ids || [],
      rules
    })
    for (const rule of rules) {
      for (const model of rule.models || []) {
        if (!model || isWildcardModel(model)) continue
        models.add(model)
      }
    }
  }

  return { scopes, models: [...models].sort((a, b) => a.localeCompare(b)) }
}

/**
 * 找出作用域内对某模型生效的规则：先精确名，再通配符前缀（与后端一致）。
 * 同一作用域内只有一条规则生效。
 */
export function matchScopeRule(scope: PricingScope, model: string): ChannelModelPricing | null {
  const lower = model.toLowerCase()
  let wildcard: ChannelModelPricing | null = null
  for (const rule of scope.rules || []) {
    for (const pattern of rule.models || []) {
      const normalized = pattern.toLowerCase()
      if (normalized === lower) return rule
      if (normalized.endsWith('*') && lower.startsWith(normalized.slice(0, -1)) && !wildcard) {
        wildcard = rule
      }
    }
  }
  return wildcard
}

/**
 * 某条规则在其计费模式下的必需价格字段。
 *  - token：input_price / output_price；币种为 CNY 时还需要缓存写价与缓存读价
 *    （与 usablePriceSnapshot 的 CNY 判定保持一致）
 *  - per_request / image / video：per_request_price（默认价卡快照没有这个字段，
 *    所以这三种计费模式只有手工规则能兜住）
 */
export function requiredPriceFields(
  rule: Pick<ChannelModelPricing, 'billing_mode' | 'currency'>
): string[] {
  if (rule.billing_mode === 'token') {
    const fields = ['input_price', 'output_price']
    if (rule.currency === 'CNY') fields.push('cache_write_price', 'cache_read_price')
    return fields
  }
  return ['per_request_price']
}

/** 该规则缺失的必需价格字段。 */
export function missingPriceFields(rule: ChannelModelPricing): string[] {
  return requiredPriceFields(rule).filter((field) => {
    const value =
      field === 'input_price'
        ? rule.input_price
        : field === 'output_price'
          ? rule.output_price
          : field === 'cache_write_price'
            ? rule.cache_write_price
            : field === 'cache_read_price'
              ? rule.cache_read_price
              : rule.per_request_price
    return value == null
  })
}

/** 规则里是否填了任何价格（用于区分「手工」与「缺省」）。 */
export function ruleHasPrices(rule: ChannelModelPricing): boolean {
  return [
    rule.input_price,
    rule.output_price,
    rule.cache_write_price,
    rule.cache_read_price,
    rule.image_input_price,
    rule.image_output_price,
    rule.per_request_price
  ].some((value) => value != null)
}

/**
 * 规则是否带出了「价格意图」：
 *  - 非 token 计费模式 → 有（按次/图片/视频规则必须自带单次价）
 *  - CNY 币种 → 有（后端 manualTokenPricingError：CNY 手工价必须写全输入/输出/缓存读写）
 *  - 任一价格字段非空 → 有
 *  - 否则（USD token 规则一个价格都没填）→ 无：后端把它当「只登记模型」的允许项，
 *    定价仍由默认价卡兜底。
 */
export function ruleHasPricingIntent(rule: ChannelModelPricing): boolean {
  if (rule.billing_mode !== 'token') return true
  if (rule.currency === 'CNY') return true
  return ruleHasPrices(rule)
}

/** 规则是否构成一份完整的手工定价（仅在规则带出价格意图时才有意义）。 */
export function isRuleComplete(rule: ChannelModelPricing): boolean {
  if (!ruleHasPricingIntent(rule)) return false
  return missingPriceFields(rule).length === 0
}

/** 默认价卡快照缺失时的必需字段（价卡只有 token 口径的价格）。 */
export function cardRequiredFields(snapshot?: ModelDefaultPricing): string[] {
  const fields = ['input_price', 'output_price']
  if (snapshot?.currency === 'CNY') fields.push('cache_write_price', 'cache_read_price')
  return fields
}

/**
 * 找出对某模型生效的作用域与规则（后端优先级：分组规则整体优先于渠道规则）。
 *
 * - 命中分组规则的分组：分组规则生效，该渠道规则对它的绑定分组被遮蔽。
 * - 渠道规则：只在该渠道绑定的分组「没有自己的分组规则」时生效。
 */
export function effectiveRuleRefs(data: PricingAuditData, model: string): PricingRuleRef[] {
  const groupScopes = data.scopes.filter(scope => scope.scope === 'group')
  const channelScopes = data.scopes.filter(scope => scope.scope === 'channel')

  const refs: PricingRuleRef[] = []
  const groupsWithRule = new Set<number>()

  for (const scope of groupScopes) {
    const rule = matchScopeRule(scope, model)
    if (!rule) continue
    groupsWithRule.add(scope.scopeId)
    refs.push({
      scope: 'group',
      scopeId: scope.scopeId,
      scopeName: scope.scopeName,
      platform: rule.platform || '',
      rule
    })
  }

  for (const scope of channelScopes) {
    const rule = matchScopeRule(scope, model)
    if (!rule) continue
    // 该渠道绑定的分组若都有自己的分组规则，则渠道规则被遮蔽。
    const shadowed =
      scope.boundGroupIds.length > 0 &&
      scope.boundGroupIds.every(groupId => groupsWithRule.has(groupId))
    if (shadowed) continue
    refs.push({
      scope: 'channel',
      scopeId: scope.scopeId,
      scopeName: scope.scopeName,
      platform: rule.platform || '',
      rule
    })
  }

  return refs
}

/** 需要逐模型探测默认价卡的模型（候选模型全集；通配符已在收集阶段排除）。 */
export function probeTargetModels(data: PricingAuditData): string[] {
  return [...data.models]
}

/**
 * 价卡兜底判定：某个模型在「没有带价格的规则生效」时由默认价卡决定定价。
 * 返回 null 表示不缺价。
 */
function cardMissingRow(
  model: string,
  scope: PricingScopeKind | 'card',
  scopeId: number | undefined,
  scopeName: string,
  platform: string,
  probe: ProbeResults
): MissingPriceRow | null {
  if (probe.failures[model]) {
    return {
      model,
      scope,
      scopeId,
      scopeName,
      platform,
      missingFields: [],
      priceSource: 'card',
      kind: 'probe_failed',
      probeFailed: true
    }
  }
  const snapshot = probe.results[model]
  if (!snapshot) return null
  if (!snapshot.found) {
    return {
      model,
      scope,
      scopeId,
      scopeName,
      platform,
      missingFields: cardRequiredFields(snapshot),
      priceSource: 'card',
      kind: 'card_not_found',
      probeFailed: false
    }
  }
  if (snapshot.snapshot_ready === false) {
    return {
      model,
      scope,
      scopeId,
      scopeName,
      platform,
      missingFields: [],
      priceSource: 'card',
      kind: 'card_not_ready',
      probeFailed: false
    }
  }
  if (!usablePriceSnapshot(snapshot)) {
    return {
      model,
      scope,
      scopeId,
      scopeName,
      platform,
      missingFields: cardRequiredFields(snapshot),
      priceSource: 'card',
      kind: 'card_not_found',
      probeFailed: false
    }
  }
  return null
}

/**
 * 计算缺价清单。
 *
 * 逐「作用域」判定：
 *  - 生效规则带出价格 → 该规则整条生效，缺哪个必需字段就是缺什么（价卡不兜底）
 *  - 生效规则没带出价格 → 由默认价卡兜底，缺不缺价看价卡快照
 */
export function resolveMissingPriceRows(
  data: PricingAuditData,
  probe: ProbeResults = EMPTY_PROBE_RESULTS
): MissingPriceRow[] {
  const rows: MissingPriceRow[] = []

  for (const model of data.models) {
    const refs = effectiveRuleRefs(data, model)
    if (refs.length === 0) {
      // 没有任何作用域的规则命中该模型 → 定价完全由默认价卡决定。
      const cardRow = cardMissingRow(model, 'card', undefined, '', '', probe)
      if (cardRow) rows.push(cardRow)
      continue
    }

    for (const ref of refs) {
      if (ruleHasPricingIntent(ref.rule)) {
        const missing = missingPriceFields(ref.rule)
        if (missing.length === 0) continue
        rows.push({
          model,
          scope: ref.scope,
          scopeId: ref.scopeId,
          scopeName: ref.scopeName,
          platform: ref.platform,
          missingFields: missing,
          priceSource: 'rule',
          kind: 'rule',
          probeFailed: Boolean(probe.failures[model])
        })
        continue
      }

      const cardRow = cardMissingRow(
        model,
        ref.scope,
        ref.scopeId,
        ref.scopeName,
        ref.platform,
        probe
      )
      if (cardRow) rows.push(cardRow)
    }
  }

  return rows
}

function ruleDisplayPrices(rule: ChannelModelPricing) {
  return {
    inputPrice: rule.billing_mode === 'token' ? perTokenToMTok(rule.input_price) : null,
    outputPrice: rule.billing_mode === 'token' ? perTokenToMTok(rule.output_price) : null,
    cacheReadPrice: rule.billing_mode === 'token' ? perTokenToMTok(rule.cache_read_price) : null,
    cacheWritePrice: rule.billing_mode === 'token' ? perTokenToMTok(rule.cache_write_price) : null,
    imagePrice: perTokenToMTok(rule.image_output_price ?? rule.image_input_price),
    perRequestPrice: rule.billing_mode === 'token' ? null : rule.per_request_price
  }
}

/**
 * 构建价格总表数据：每条规则 × 规则里的一个模型名一行。
 *
 * 「价格来源」描述该行规则给出的价格来源：
 *  - 手工：规则里填了价
 *  - 缺省：规则存在但没填价
 *  - 价卡：规则没填价，而默认价卡探测到可用价（运行时由价卡兜底）
 */
export function buildPriceTableRows(
  data: PricingAuditData,
  probe: ProbeResults = EMPTY_PROBE_RESULTS
): PriceTableRow[] {
  const rows: PriceTableRow[] = []

  for (const scope of data.scopes) {
    for (const rule of scope.rules || []) {
      for (const model of rule.models || []) {
        if (!model) continue
        const prices = ruleDisplayPrices(rule)
        const cardUsable = cardStatusOf(model, probe) === 'usable'
        rows.push({
          model,
          currency: rule.currency || 'USD',
          billingMode: rule.billing_mode,
          unit: rule.billing_mode,
          ...prices,
          source: ruleHasPrices(rule) ? 'manual' : cardUsable ? 'card' : 'default',
          scope: scope.scope,
          scopeId: scope.scopeId,
          scopeName: scope.scopeName,
          platform: rule.platform || ''
        })
      }
    }
  }

  return rows
}

/** 价格总表里某模型在默认价卡中的状态。 */
export type CardStatus = 'not_probed' | 'usable' | 'unusable' | 'failed'

export function cardStatusOf(model: string, probe: ProbeResults = EMPTY_PROBE_RESULTS): CardStatus {
  if (probe.failures[model]) return 'failed'
  const snapshot = probe.results[model]
  if (!snapshot) return 'not_probed'
  return usablePriceSnapshot(snapshot) ? 'usable' : 'unusable'
}
