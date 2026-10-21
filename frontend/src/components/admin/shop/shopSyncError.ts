/**
 * 管理端「商品同步状态」错误码 → 可读文案的映射。
 *
 * 稳定错误码来自后端商品同步服务（与用户侧同一套取值）。界面只展示**本站语义**的
 * 可读文案：未知码一律回落到通用「商品同步失败」，绝不把原始码当作正文糊到界面上；
 * 原始码只保留在错误行的 title 属性里，方便运维排查。
 */

/** 已知稳定错误码 → i18n 文案 key。 */
export const SHOP_SYNC_ERROR_I18N_KEYS: Record<string, string> = {
  shop_not_configured: 'admin.shopSync.errors.notConfigured',
  product_sync_unavailable: 'admin.shopSync.errors.productSyncUnavailable',
  shop_upstream_unavailable: 'admin.shopSync.errors.serviceUnavailable',
  shop_upstream_rejected: 'admin.shopSync.errors.serviceRejected',
  shop_upstream_response_invalid: 'admin.shopSync.errors.serviceResponseInvalid',
}

/** 未知错误码的通用回落文案 key。 */
export const SHOP_SYNC_FALLBACK_ERROR_I18N_KEY = 'admin.shopSync.errors.unknown'

/**
 * 把后端错误码翻译成 i18n key。
 *
 * - 空码（当前无错误）→ null，调用方不渲染错误行；
 * - 已知码 → 对应的可读文案 key；
 * - 未知码 → 通用回落文案 key（不暴露原始码）。
 */
export function shopSyncErrorI18nKey(code: string | null | undefined): string | null {
  const value = typeof code === 'string' ? code.trim() : ''
  if (!value) return null
  return SHOP_SYNC_ERROR_I18N_KEYS[value] ?? SHOP_SYNC_FALLBACK_ERROR_I18N_KEY
}
