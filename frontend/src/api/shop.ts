/**
 * White-label recharge (shop) API endpoints.
 *
 * 全部请求都走本站 API：前端**不会**直接访问上游域名，也不会拿到上游凭据
 * （PRD-03 FE-01 / FE-04）。
 */

import { apiClient } from './client'

/** 展示状态：本站对上游支付/交付结果的映射，不是独立支付状态机。 */
export type ShopOrderDisplayStatus = 'pending' | 'paid' | 'delivery_pending' | 'delivered'

/** 支付指引类型。'none' 表示当前没有可展示的支付动作。 */
export type ShopPaymentKind = 'none' | 'qr_code' | 'redirect'

export interface ShopProduct {
  id: string
  name: string
  description: string
  price_cny: number
  original_price_cny?: number
  stock?: number
  listed: boolean
  purchasable: boolean
  credit_per_cny: number
  synced_at?: string
}

/**
 * 商品目录状态。
 *
 * configured=false 或 available=false 时前端必须展示「未配置 / 服务不可用」，
 * 并且**不得**渲染任何商品（禁止假数据，PRD-03 §2.8 / FE-05）。
 */
export interface ShopCatalog {
  configured: boolean
  available: boolean
  unavailable_reason?: string
  products: ShopProduct[]
  synced_at?: string
  sync_failed_at?: string
  sync_error_code?: string
}

export interface ShopPayment {
  kind: ShopPaymentKind
  qr_content?: string
  redirect_url?: string
  expires_at?: string
}

export interface ShopOrder {
  order_no: string
  product_id: string
  product_name?: string
  quantity: number
  amount_cny: number
  display_status: ShopOrderDisplayStatus
  delivery_error_code?: string
  card_secret?: string
  card_hint?: string
  redeem_path?: string
  payment?: ShopPayment
  paid_at?: string
  created_at?: string
  updated_at?: string
}

export interface CreateShopOrderRequest {
  product_id: string
  quantity: number
  /** 客户端请求 ID：服务端据此保证重复点击只产生一条订单映射。 */
  idempotency_key: string
}

export const shopAPI = {
  /**
   * 商品目录（同步失败时仍返回最近一次成功商品，但 available=false）。
   *
   * 返回已解包的业务数据而不是 AxiosResponse：调用方只关心目录本身，
   * 与仓库里 subscriptions.ts 等模块的既有约定保持一致。
   */
  async getCatalog(): Promise<ShopCatalog> {
    const response = await apiClient.get<ShopCatalog>('/shop/products')
    return response.data
  },

  /** 下单。 */
  async createOrder(data: CreateShopOrderRequest): Promise<ShopOrder> {
    const response = await apiClient.post<ShopOrder>('/shop/orders', data)
    return response.data
  },

  /** 当前用户的订单列表。 */
  async listOrders(params?: { limit?: number }): Promise<ShopOrder[]> {
    const response = await apiClient.get<ShopOrder[]>('/shop/orders', { params })
    return response.data
  },

  /** 订单详情（含本人可见的卡密）。 */
  async getOrder(orderNo: string): Promise<ShopOrder> {
    const response = await apiClient.get<ShopOrder>(`/shop/orders/${encodeURIComponent(orderNo)}`)
    return response.data
  },

  /** 主动查单：支付结果的唯一可信来源之一（前端按钮不构成支付成功依据）。 */
  async queryOrder(orderNo: string): Promise<ShopOrder> {
    const response = await apiClient.post<ShopOrder>(`/shop/orders/${encodeURIComponent(orderNo)}/query`)
    return response.data
  },
}

/**
 * 生成一次下单的幂等键。
 *
 * 用 crypto.randomUUID 而不是时间戳：每次用户动作都必须是**新**的键（各自成单），
 * 而同一逻辑请求的重试必须复用**同一个**键（避免重复下单）。
 */
export function createIdempotencyKey(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return `req-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`
}

/**
 * 校验服务端下发的支付跳转目标能否安全跳转。
 *
 * 主机白名单由服务端负责：不在允许主机内的跳转目标根本不会进入 DTO
 * （后端 guardPaymentInstruction，fail closed），因此前端**不再自己实现一套
 * 主机白名单**，只做兜底校验：
 *  - 必须是可解析的绝对 URL；
 *  - 协议必须是 http/https；
 *  - 必须带主机名。
 *
 * 任一条不满足就返回 false，由调用方展示既有 redirectBlocked 文案并且不跳转，
 * 也不向界面暴露任何白名单/域名细节。
 */
export function isSafeShopRedirectUrl(url: string | null | undefined): boolean {
  const raw = (url ?? '').trim()
  if (!raw) return false
  let parsed: URL
  try {
    parsed = new URL(raw)
  } catch {
    return false
  }
  const scheme = parsed.protocol.toLowerCase()
  if (scheme !== 'http:' && scheme !== 'https:') return false
  return !!parsed.hostname
}

/** 本地快照键：用于刷新后回查最近一次订单。 */
const SHOP_ORDER_SNAPSHOT_KEY = 'shop_last_order_no'

/**
 * 读取本地保存的最近订单号。
 *
 * 刷新页面后服务端不会主动推送订单号，只能靠 URL 参数或本地快照回查；
 * 快照损坏或不可用（隐私模式）时返回 null，界面退回「从列表进入」。
 */
export function readShopOrderSnapshot(): string | null {
  try {
    const value = window.localStorage.getItem(SHOP_ORDER_SNAPSHOT_KEY)
    return value && value.trim() ? value.trim() : null
  } catch {
    return null
  }
}

/** 记录最近一次订单号（仅保存订单号，不保存卡密等敏感内容）。 */
export function writeShopOrderSnapshot(orderNo: string | undefined | null): void {
  const value = (orderNo ?? '').trim()
  try {
    if (value) {
      window.localStorage.setItem(SHOP_ORDER_SNAPSHOT_KEY, value)
    } else {
      window.localStorage.removeItem(SHOP_ORDER_SNAPSHOT_KEY)
    }
  } catch {
    // 本地存储不可用时忽略：URL 参数仍可回查订单。
  }
}

/**
 * 把服务端稳定错误码映射为 i18n 文案键。
 *
 * 只映射本站错误码；未知错误返回通用文案键，避免把服务端内部信息透到界面。
 */
export function shopErrorKey(reason: string | undefined): string {
  switch (reason) {
    case 'shop_not_configured':
      return 'shop.errors.notConfigured'
    case 'shop_upstream_unavailable':
      return 'shop.errors.upstreamUnavailable'
    case 'shop_upstream_rejected':
      return 'shop.errors.upstreamRejected'
    case 'shop_upstream_response_invalid':
      return 'shop.errors.upstreamInvalid'
    case 'product_sync_unavailable':
      return 'shop.errors.syncUnavailable'
    case 'shop_product_not_found':
      return 'shop.errors.productNotFound'
    case 'shop_order_not_found':
      return 'shop.errors.orderNotFound'
    case 'shop_idempotency_key_required':
      return 'shop.errors.idempotencyKeyRequired'
    case 'shop_idempotency_conflict':
      return 'shop.errors.idempotencyConflict'
    case 'order_delivery_pending':
      return 'shop.errors.deliveryPending'
    case 'shop_delivery_unavailable':
      return 'shop.errors.deliveryUnavailable'
    default:
      return 'shop.errors.generic'
  }
}
