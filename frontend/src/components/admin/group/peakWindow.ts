/**
 * 分组级高峰时段（peak_rate_*）的当前状态判定。
 *
 * 与后端 Group.PeakMultiplierAt（backend/internal/service/group.go）保持一致：
 * - 时段是左闭右开 [peak_start, peak_end)，且**不支持跨天**（end 必须晚于 start）；
 * - 判定用的是服务器本地时区（部署 compose 默认 TZ=Asia/Shanghai），
 *   所以这里按 Asia/Shanghai 计算墙上时间；
 * - 命中高峰时 token 计费倍率乘以 peak_rate_multiplier，谷时为 1 倍。
 *
 * 注意：这是**分组自己的高峰倍率**，与 DeepSeek 官方峰谷（按官方时段自动判定）
 * 是两套机制，不要混为一谈。
 */
export type PeakWindowState = 'disabled' | 'peak' | 'valley' | 'invalid'

export interface PeakWindowConfig {
  enabled: boolean
  /** HH:MM */
  start: string
  /** HH:MM */
  end: string
  multiplier: number
}

export interface PeakWindowStatus {
  state: PeakWindowState
  /** 当前生效的 token 倍率：谷时为 1。 */
  multiplier: number
  /** 下次切换的墙上时间（HH:MM）。 */
  nextSwitchAt: string | null
  /** 下次切换发生在今天还是明天。 */
  nextSwitchDay: 'today' | 'tomorrow' | null
  /** 下次切换后进入的状态。 */
  nextState: 'peak' | 'valley' | null
}

/** 解析 HH:MM → 当天分钟数；格式非法返回 null。 */
export function parseHhMm(value: string): number | null {
  const match = /^(\d{1,2}):(\d{2})$/.exec((value || '').trim())
  if (!match) return null
  const hour = Number(match[1])
  const minute = Number(match[2])
  if (!Number.isInteger(hour) || !Number.isInteger(minute)) return null
  if (hour < 0 || hour > 23 || minute < 0 || minute > 59) return null
  return hour * 60 + minute
}

/** 当天在 Asia/Shanghai 的分钟数（与部署时区一致）。 */
export function shanghaiMinutesOfDay(now: Date): number {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone: 'Asia/Shanghai',
    hourCycle: 'h23',
    hour: '2-digit',
    minute: '2-digit'
  }).formatToParts(now)
  const hour = Number(parts.find(part => part.type === 'hour')?.value ?? '0')
  const minute = Number(parts.find(part => part.type === 'minute')?.value ?? '0')
  return (Number.isFinite(hour) ? hour : 0) * 60 + (Number.isFinite(minute) ? minute : 0)
}

function formatHhMm(minutes: number): string {
  const hour = Math.floor(minutes / 60)
  const minute = minutes % 60
  return `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`
}

/**
 * 判定分组高峰时段的当前状态与下次切换时间。
 */
export function peakWindowStatus(
  config: PeakWindowConfig,
  now: Date = new Date()
): PeakWindowStatus {
  if (!config.enabled) {
    return { state: 'disabled', multiplier: 1, nextSwitchAt: null, nextSwitchDay: null, nextState: null }
  }

  const start = parseHhMm(config.start)
  const end = parseHhMm(config.end)
  // 后端要求 start/end 合法且 end 晚于 start（不支持跨天区间）。
  if (start == null || end == null || end <= start) {
    return { state: 'invalid', multiplier: 1, nextSwitchAt: null, nextSwitchDay: null, nextState: null }
  }

  const nowMinutes = shanghaiMinutesOfDay(now)
  const isPeak = nowMinutes >= start && nowMinutes < end
  const multiplier =
    isPeak && Number.isFinite(config.multiplier) ? config.multiplier : 1

  if (isPeak) {
    return {
      state: 'peak',
      multiplier,
      nextSwitchAt: formatHhMm(end),
      nextSwitchDay: 'today',
      nextState: 'valley'
    }
  }

  // 谷时段：下一个切换点是今天的 start（已过则顺延到明天）。
  return {
    state: 'valley',
    multiplier: 1,
    nextSwitchAt: formatHhMm(start),
    nextSwitchDay: nowMinutes < start ? 'today' : 'tomorrow',
    nextState: 'peak'
  }
}
