/**
 * 管理端读取 DeepSeek **官方**峰谷的当前档位与下一档切换时间。
 *
 * 口径：档位与切换时间都由后端判定（后端复用官方时段日历），前端只负责显示，
 * **不在前端复制官方时段规则**去推算当前是峰还是谷。字段缺失（后端还没提供）
 * 时整块不显示，不猜、不报错。
 */
import type { DeepSeekCurrentPeriod, PricingStatus } from '@/api/admin/pricing'

/** DeepSeek 官方时段使用的时区（后端未给 timezone 时的兜底展示值）。 */
export const DEFAULT_DEEPSEEK_TIMEZONE = 'Asia/Shanghai'

export interface DeepSeekPeriodView {
  /** 后端判定的档位：peak / valley；其它值原样保留。 */
  state: string
  /** 服务端给的权威文案。 */
  label: string
  /** 官方时段时区。 */
  timezone: string
  /** 官方时段描述（例：09:00-12:00）。 */
  window: string
  /** 高峰倍率。 */
  multiplier: number | null
  /** 下一档切换时间（原始 RFC3339）。 */
  nextSwitchAt: string | null
  /** 下一档切换时间（按官方时区格式化后的墙上时间）。 */
  nextSwitchText: string | null
  /** 当年假日日历是否缺失（缺失时该年不启用官方高峰加价）。 */
  calendarYearMissing: boolean
}

function asString(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

/**
 * 把 RFC3339 时间按指定时区格式化成 YYYY-MM-DD HH:mm（不随浏览器语言变化）。
 * 时区非法或时间不可解析时返回空串。
 */
export function formatPeriodTime(value: string, timeZone: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  try {
    const parts = new Intl.DateTimeFormat('en-CA', {
      timeZone,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false
    }).formatToParts(date)
    const pick = (type: string) => parts.find(part => part.type === type)?.value ?? ''
    const hour = pick('hour') === '24' ? '00' : pick('hour')
    return `${pick('year')}-${pick('month')}-${pick('day')} ${hour}:${pick('minute')}`
  } catch {
    return ''
  }
}

/**
 * 读取当前档位。字段缺失或没有可显示内容时返回 null（界面不显示这一块）。
 */
export function readDeepSeekPeriod(
  status: Pick<PricingStatus, 'deepseek_current_period' | 'deepseek_calendar_year_missing'> | null | undefined
): DeepSeekPeriodView | null {
  const raw = status?.deepseek_current_period as DeepSeekCurrentPeriod | undefined
  if (!raw || typeof raw !== 'object') return null

  const state = asString(raw.state)
  const label = asString(raw.label)
  const timezone = asString(raw.timezone) || DEFAULT_DEEPSEEK_TIMEZONE
  const window = asString(raw.window)
  const multiplier =
    typeof raw.multiplier === 'number' && Number.isFinite(raw.multiplier) ? raw.multiplier : null
  const nextSwitchAt = asString(raw.next_switch_at)
  // 时间不可解析（或时区非法）时视为没有可显示的切换时间。
  const nextSwitchText = nextSwitchAt ? formatPeriodTime(nextSwitchAt, timezone) || null : null

  // 没有任何可显示内容就不要渲染这一块。
  if (!state && !label && !nextSwitchAt) return null

  return {
    state,
    label,
    timezone,
    window,
    multiplier,
    nextSwitchAt,
    nextSwitchText,
    calendarYearMissing: status?.deepseek_calendar_year_missing === true
  }
}

/**
 * 档位文案：优先用服务端给的权威 label；没有 label 时按 state 兜底。
 * 两者都没有就返回空串（不显示档位行）。
 */
export function resolvePeriodLabel(
  period: DeepSeekPeriodView,
  t: (key: string) => string
): string {
  if (period.label) return period.label
  if (period.state === 'peak') return t('admin.settings.pricing.deepseekPeriod.peak')
  if (period.state === 'valley') return t('admin.settings.pricing.deepseekPeriod.valley')
  return ''
}
