/**
 * 网关请求失败的「非普通错误」分类 —— 用户侧。
 *
 * 本轮安全整改把两类失败的口径分开了（F-06）：
 *  - 模型不属于该分组可路由集合 → **404 model_not_found**（"这个分组没有这个模型"）
 *  - 模型属于集合但没有可用价格 → **503 pricing_unavailable**（红线：缺价不得放行）
 *
 * 两者对用户是**完全不同的处置**：
 *  - model_not_found = 选错了模型/分组，换一个就行；
 *  - pricing_unavailable = 平台还没配价，等运维补价，重试无用。
 *
 * 所以 UI 不能都丢一串后端原文。这里只做「分类 + 给文案 key」，不重写后端判断：
 * 后端若是回了结构化 code/reason，一律以它为准；只有拿不到结构时才退到
 * 「HTTP 状态码 + message 特征」的启发式。
 */

export const PRICING_UNAVAILABLE_STATUS_CODE = 503
export const MODEL_NOT_FOUND_STATUS_CODE = 404

export type GatewayFailureKind = 'pricing_unavailable' | 'model_not_found'

/** 后端可能下发结构化错误码的几个字段（信封解包后可能是 code / reason / error）。 */
export interface GatewayFailureLike {
  status?: number | null
  status_code?: number | null
  code?: number | string | null
  reason?: string | null
  message?: string | null
}

const PRICING_CODES = ['pricing_unavailable', 'PRICING_UNAVAILABLE', 'MODEL_PRICING_UNAVAILABLE']
const MODEL_NOT_FOUND_CODES = ['model_not_found', 'MODEL_NOT_FOUND']

/** message 特征：只认后端实际会下发的那几句（见 backend gateway_handler 的 billingErrorDetails）。 */
const PRICING_MESSAGE_PATTERNS: RegExp[] = [
  /pricing not found/i,
  /no pricing available/i,
  /pricing is unavailable/i,
  /pricing_unavailable/i,
]
const MODEL_NOT_FOUND_MESSAGE_PATTERNS: RegExp[] = [
  /model_not_found/i,
  /model not found/i,
  /unknown model/i,
  /no available channel for model/i,
]

function readStatus(f: GatewayFailureLike): number | null {
  const raw = f.status ?? f.status_code ?? f.code
  const n = Number(raw)
  return Number.isFinite(n) ? n : null
}

function readCodes(f: GatewayFailureLike): string[] {
  const out: string[] = []
  // 只把**字符串** code/reason 当成结构化错误码：数字 code 是 HTTP 状态码，不是业务码。
  if (typeof f.reason === 'string') out.push(f.reason)
  if (typeof f.code === 'string') out.push(f.code)
  return out
}

/**
 * 分类一次网关失败。
 *
 * @returns 'pricing_unavailable' | 'model_not_found' | null（null = 普通错误，按原样展示）
 */
export function classifyGatewayFailure(failure: GatewayFailureLike | null | undefined): GatewayFailureKind | null {
  if (!failure) return null

  const codes = readCodes(failure)
  if (codes.some((c) => PRICING_CODES.some((p) => p.toLowerCase() === c.toLowerCase()))) return 'pricing_unavailable'
  if (codes.some((c) => MODEL_NOT_FOUND_CODES.some((p) => p.toLowerCase() === c.toLowerCase()))) return 'model_not_found'

  const message = (failure.message ?? '').trim()
  const status = readStatus(failure)

  // 结构化码缺失时才看 message；message 也缺失就只剩状态码，那不足以定性。
  if (message) {
    if (PRICING_MESSAGE_PATTERNS.some((p) => p.test(message))) return 'pricing_unavailable'
    if (MODEL_NOT_FOUND_MESSAGE_PATTERNS.some((p) => p.test(message))) return 'model_not_found'
    return null
  }

  // 无 message：503 仍然按缺价（后端缺价固定 503，且此时不重试才有意义），404 不猜。
  if (status === PRICING_UNAVAILABLE_STATUS_CODE) return 'pricing_unavailable'
  return null
}

/** 分类结果 → i18n key 命名空间后缀（gwFailure.<kind>.*）。 */
export function gatewayFailureI18nKey(kind: GatewayFailureKind): string {
  return kind === 'pricing_unavailable'
    ? 'gatewayFailure.pricingUnavailable'
    : 'gatewayFailure.modelNotFound'
}
