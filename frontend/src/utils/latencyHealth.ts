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
  good: 'text-emerald-600 dark:text-emerald-400',
  warn: 'text-amber-600 dark:text-amber-400',
  slow: 'text-orange-600 dark:text-orange-400',
  critical: 'text-red-600 dark:text-red-400',
}

/** 无首字数据时的纯色色条（仅按总耗时档着色）。 */
export const LATENCY_BAR_CLASSES: Record<LatencySeverity, string> = {
  good: 'bg-emerald-500',
  warn: 'bg-amber-400',
  slow: 'bg-orange-500',
  critical: 'bg-red-500',
}

/**
 * 首字档 + 总耗时档同时存在时的色条。
 * 原实现依赖 bg-gradient-to-b 搭配 from 与 to 色标；渐变类已按设计体系移除，
 * 仅保留色标会让色条完全没有背景。这里直接给出实色，按更严重的一档取色，
 * 既不依赖渐变，也保持「先看首字、再看总耗时」的告警语义。
 */
export const LATENCY_BAR_FROM_CLASSES: Record<LatencySeverity, string> = {
  good: 'bg-emerald-500',
  warn: 'bg-amber-400',
  slow: 'bg-orange-500',
  critical: 'bg-red-500',
}

/** 兼容保留：调用方与既有测试仍引用该常量，取值与首字档实色一致。 */
export const LATENCY_BAR_TO_CLASSES: Record<LatencySeverity, string> = {
  good: 'bg-emerald-500',
  warn: 'bg-amber-400',
  slow: 'bg-orange-500',
  critical: 'bg-red-500',
}

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
