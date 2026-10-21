/**
 * DeepSeek 峰谷计价状态（用户侧只读展示）。
 *
 * 后端在分组 DTO 上以 deepseek_pricing_state 下发（PR #8 契约）：
 *   {
 *     state: "peak" | "valley" | "none",
 *     label: string,
 *     timezone: string,
 *     multiplier: number,
 *     next_switch_at: string | null   // RFC3339；state=none 时为 null
 *   }
 *
 * 前端**不自己判定**峰谷、也不自己推算切换时间：state/label/倍率/切换时间
 * 一律按服务端下发的值显示；
 * 字段缺失（老数据 / 接口没带 / 后端还没上线该字段）时按「没有状态」处理，
 * 不显示徽标也不报错。
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
  /**
   * 下一档切换时间（RFC3339 原文，例如 "2026-10-22T09:00:00+08:00"）。
   *
   * 字段缺失 / null / 空串 / 非法时间 → undefined（不显示切换时间）。
   */
  nextSwitchAt?: string
}

const VALID_STATES: DeepSeekPricingStateKind[] = ['peak', 'valley', 'none']

/**
 * 容错读取「下一档切换时间」。
 *
 * 只接受**后端给的时间**；缺失 / null / 空串 / 非法时间串一律当作没有，
 * 前端不自己推算、不报错。
 */
function readNextSwitchAt(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined
  const raw = value.trim()
  if (!raw) return undefined
  const parsed = new Date(raw)
  if (Number.isNaN(parsed.getTime())) return undefined
  return raw
}

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
  const multiplier =
    typeof record.multiplier === 'number' && Number.isFinite(record.multiplier)
      ? record.multiplier
      : undefined
  return {
    state,
    label: label || undefined,
    timezone: timezone || undefined,
    multiplier,
    nextSwitchAt: readNextSwitchAt(record.next_switch_at),
  }
}
