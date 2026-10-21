/**
 * DeepSeek 峰谷计价状态（用户侧只读展示）。
 *
 * 后端在分组 DTO 上以 deepseek_pricing_state 下发：
 *   { state: "peak" | "valley" | "none", label: string, timezone: string, multiplier: number }
 *
 * 这里**不**依赖 types/index.ts 的 Group 类型（字段缺失时按「没有状态」处理），
 * 读取全程容错：老数据 / 接口没带字段 → 返回 null，界面不显示徽标也不报错。
 */

export type DeepSeekPricingStateKind = 'peak' | 'valley' | 'none'

export interface DeepSeekPricingState {
  state: DeepSeekPricingStateKind
  /** 后端给的状态文案（可能是中文或英文），为空时前端按 state 兜底。 */
  label?: string
  /** 判定所用时区，例如 Asia/Shanghai。 */
  timezone?: string
  /** 生效倍率，例如 1.5。 */
  multiplier?: number
}

const VALID_STATES: DeepSeekPricingStateKind[] = ['peak', 'valley', 'none']

/**
 * 从任意对象上安全地取出峰谷状态。
 *
 * 字段缺失、结构不对、state 非法 → null（调用方据此不渲染徽标）。
 */
export function readDeepSeekPricingState(source: unknown): DeepSeekPricingState | null {
  if (!source || typeof source !== 'object') return null
  const raw = (source as { deepseek_pricing_state?: unknown }).deepseek_pricing_state
  if (!raw || typeof raw !== 'object') return null
  const record = raw as Record<string, unknown>
  const state = String(record.state ?? '') as DeepSeekPricingStateKind
  if (!VALID_STATES.includes(state)) return null
  const label = typeof record.label === 'string' ? record.label.trim() : ''
  const timezone = typeof record.timezone === 'string' ? record.timezone.trim() : ''
  const multiplier = typeof record.multiplier === 'number' && Number.isFinite(record.multiplier)
    ? record.multiplier
    : undefined
  return {
    state,
    label: label || undefined,
    timezone: timezone || undefined,
    multiplier,
  }
}
