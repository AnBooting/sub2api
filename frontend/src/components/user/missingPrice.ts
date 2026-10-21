/**
 * 缺价（模型未配置价格）判定 —— 用户侧。
 *
 * 后端口径（红线）：**缺价不得放行**。模型没有可用价格时，网关在转发上游
 * 之前就拒绝请求，返回 HTTP 503 + code=pricing_unavailable
 * （backend/internal/handler/gateway_handler.go: billingErrorDetails）。
 *
 * 用户能在前端看到这条信息的两处数据源：
 *  - 错误请求列表（GET /usage/errors）：行上有 status_code 与 message；
 *  - 可用渠道（GET /channels/available）：模型没有 pricing 字段或价格全为空。
 *
 * 这里只做「判定 + 文案指引」，不自己算价格、不自己造时间。
 */

/** 后端缺价错误固定返回 503。 */
export const MISSING_PRICE_STATUS_CODE = 503

/**
 * 后端缺价错误的 message 特征。
 *
 * 后端这几处文案都是英文（billingErrorDetails → pkgerrors.Message(err)）：
 *   - "no pricing available for model: X: pricing not found"
 *   - "Model pricing is unavailable. Please retry later."
 * 命中失败时退化为普通错误展示，不误报缺价。
 */
const MISSING_PRICE_MESSAGE_PATTERNS: RegExp[] = [
  /pricing not found/i,
  /no pricing available/i,
  /pricing is unavailable/i,
  /pricing_unavailable/i,
]

/** 错误请求行：是不是「缺价」导致的失败。 */
export function isMissingPriceError(
  row: { status_code?: number | null; message?: string | null } | null | undefined,
): boolean {
  if (!row) return false
  if (Number(row.status_code) !== MISSING_PRICE_STATUS_CODE) return false
  const message = (row.message ?? '').trim()
  if (!message) return false
  return MISSING_PRICE_MESSAGE_PATTERNS.some((pattern) => pattern.test(message))
}

/** 可用渠道里模型的价格字段（白名单字段名）。 */
const PRICING_PRICE_FIELDS = [
  'input_price',
  'output_price',
  'cache_write_price',
  'cache_write_1h_price',
  'cache_read_price',
  'image_input_price',
  'image_output_price',
  'per_request_price',
] as const

/**
 * 可用渠道里的模型是否「缺价」。
 *
 * - pricing 整个字段缺失 → 缺价；
 * - pricing 存在但所有单价都是 null → 缺价（后台配了计费模式却没配价）。
 */
export function isModelPricingMissing(pricing: unknown): boolean {
  if (pricing == null) return true
  if (typeof pricing !== 'object') return true
  const record = pricing as Record<string, unknown>
  const hasPrice = PRICING_PRICE_FIELDS.some((field) => {
    const value = record[field]
    return typeof value === 'number' && Number.isFinite(value)
  })
  if (hasPrice) return false
  const intervals = record.intervals
  if (Array.isArray(intervals)) {
    return !intervals.some((interval) => {
      if (!interval || typeof interval !== 'object') return false
      const item = interval as Record<string, unknown>
      return PRICING_PRICE_FIELDS.some((field) => {
        const value = item[field]
        return typeof value === 'number' && Number.isFinite(value)
      })
    })
  }
  return true
}
