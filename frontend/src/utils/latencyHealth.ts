import type { UsageLog } from '@/types'

/**
 * 请求延迟健康度分档（用于用量明细"延迟"列的纵向健康扫视）。
 *
 * 首 Token（TTFT）：10s 内正常，10-30s 偏慢，30-60s 缓慢，60s 及以上严重。
 * 总耗时：流式请求整体时长天然更长，阈值放宽为 1min / 3min / 5min。
 */
export type LatencySeverity = 'good' | 'warn' | 'slow' | 'critical'

export const FIRST_TOKEN_THRESHOLDS_MS = {
  warn: 10_000,
  slow: 30_000,
  critical: 60_000,
} as const

export const DURATION_THRESHOLDS_MS = {
  warn: 60_000,
  slow: 180_000,
  critical: 300_000,
} as const

interface Thresholds {
  warn: number
  slow: number
  critical: number
}

const classify = (ms: number, thresholds: Thresholds): LatencySeverity => {
  if (ms >= thresholds.critical) return 'critical'
  if (ms >= thresholds.slow) return 'slow'
  if (ms >= thresholds.warn) return 'warn'
  return 'good'
}

export const firstTokenSeverity = (ms: number): LatencySeverity =>
  classify(ms, FIRST_TOKEN_THRESHOLDS_MS)

export const durationSeverity = (ms: number): LatencySeverity =>
  classify(ms, DURATION_THRESHOLDS_MS)

export const LATENCY_TEXT_CLASSES: Record<LatencySeverity, string> = {
  good: 'text-success-600 dark:text-success-400',
  warn: 'text-warning-600 dark:text-warning-400',
  slow: 'text-warning-600 dark:text-warning-400',
  critical: 'text-danger-600 dark:text-danger-400',
}

/** 无首字数据时的纯色色条（仅按总耗时档着色）。 */
export const LATENCY_BAR_CLASSES: Record<LatencySeverity, string> = {
  good: 'bg-success-500',
  warn: 'bg-warning-400',
  slow: 'bg-warning-500',
  critical: 'bg-danger-500',
}

// 历史上这里还有 LATENCY_BAR_FROM_CLASSES / LATENCY_BAR_TO_CLASSES 两张渐变端点表。
// 渐变在新设计里被禁用（令牌契约第 2 条），色条改为单色平面 + 1px 痕量线，
// 两张表已无消费者，随渐变一起删除。

// 与运维 Token 请求统计一致：使用完整耗时，输出可能包含推理 Token。
export const formatUsageOutputRate = (row: Pick<UsageLog,
  'output_tokens' | 'duration_ms' | 'image_count' | 'image_output_tokens' | 'billing_mode' | 'request_type'
>): string => {
  const { output_tokens: outputTokens, duration_ms: durationMs } = row
  if (row.image_count > 0 || row.image_output_tokens > 0 || row.billing_mode === 'image'
    || (row.request_type && !['sync', 'stream', 'ws_v2', 'cyber'].includes(row.request_type))
    || !Number.isFinite(outputTokens) || outputTokens <= 0
    || durationMs == null || !Number.isFinite(durationMs) || durationMs <= 0) {
    return '—'
  }
  return `${(outputTokens * 1000 / durationMs).toFixed(1)} tok/s`
}