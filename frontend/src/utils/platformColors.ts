/**
 * Centralized platform color definitions.
 *
 * 平台是「归属分类」，不是状态：状态（通路/预警/断线）由 success / warning / danger
 * 承担，平台归属用中性分类色族。**但分类不等于只有一种颜色** ——
 * 审查指出，把 14 个平台收敛成同一色后，账号列表里 openai / anthropic / gemini
 * 徽章看起来完全一样，管理员必须读文字才能区分；色相在分类场景有真实信息价值。
 *
 * 因此本文件把平台按「上游家族」分到 5 个低饱和分类色相（taxonomy）：
 *   cobalt 家族（azure）· tax-blue · tax-plum · tax-sage · tax-clay
 * 这 5 个色相彼此可辨、同明度，且都刻意避开状态色（moss/ochre/oxide）的色相区间，
 * 不会被误读成「健康/告警/失败」。
 *
 * 例外：platformAccentColor() 返回给 CSS color-mix() 用的原始色值，
 * 它读的是 design-system.css 的 CSS 变量，不是写死的 hex。
 */

import { getPlatformSpec } from '@/constants/platformCatalog'

export type Platform =
  | 'anthropic'
  | 'openai'
  | 'antigravity'
  | 'gemini'
  | 'grok'
  | 'kimi'
  | 'zhipu'
  | 'deepseek'
  | 'minimax'
  | 'opencode_go'
  | 'typesafe'
  | 'command_code'
  | 'cline'
  | 'composite'

/** 分类色族：只表达「属于哪一类」，不表达状态。 */
export type PlatformHue = 'azure' | 'taxBlue' | 'taxPlum' | 'taxSage' | 'taxClay'

/**
 * 平台 → 分类色族。
 * 分族依据是「上游提供方家族」，不是随意上色：
 *   - anthropic        : 自有主线，用默认 azure（最中性）
 *   - openai           : 另一条主线，用 tax-blue（与 azure 同系但更深，可辨）
 *   - gemini/antigravity: Google 系，用 tax-sage
 *   - grok             : xAI 系，用 tax-plum
 *   - 国内四家          : kimi/zhipu/deepseek/minimax，用 tax-clay
 *   - 其余（编码/聚合）  : azure，回落到最中性
 */
const HUE_BY_PLATFORM: Record<Platform, PlatformHue> = {
  anthropic: 'azure',
  openai: 'taxBlue',
  gemini: 'taxSage',
  antigravity: 'taxSage',
  grok: 'taxPlum',
  kimi: 'taxClay',
  zhipu: 'taxClay',
  deepseek: 'taxClay',
  minimax: 'taxClay',
  opencode_go: 'azure',
  typesafe: 'azure',
  command_code: 'azure',
  cline: 'azure',
  composite: 'azure',
}

/** 每个色族的三档 Tailwind 名（badge / 实底线 / 原始色值变量） */
const HUE_TOKENS: Record<
  PlatformHue,
  { badge: string; badgeLight: string; border: string; borderStrong: string; accentVar: string; bar: string }
> = {
  azure: {
    badge:
      'border-accent-300 bg-accent-50 text-accent-700 dark:border-accent-800 dark:bg-accent-950/60 dark:text-accent-300',
    badgeLight: 'bg-accent-50 text-accent-700 dark:bg-accent-950/50 dark:text-accent-300',
    border: 'border-accent-300 dark:border-accent-800',
    borderStrong: 'border-accent-400 dark:border-accent-700',
    accentVar: 'rgb(var(--c-azure-500))',
    bar: 'bg-accent-400 dark:bg-accent-500',
  },
  taxBlue: {
    badge:
      'border-tax-blue-300 bg-tax-blue-50 text-tax-blue-700 dark:border-tax-blue-800 dark:bg-tax-blue-950/60 dark:text-tax-blue-300',
    badgeLight: 'bg-tax-blue-50 text-tax-blue-700 dark:bg-tax-blue-950/50 dark:text-tax-blue-300',
    border: 'border-tax-blue-300 dark:border-tax-blue-800',
    borderStrong: 'border-tax-blue-400 dark:border-tax-blue-700',
    accentVar: 'rgb(var(--c-tax-blue-500))',
    bar: 'bg-tax-blue-400 dark:bg-tax-blue-500',
  },
  taxPlum: {
    badge:
      'border-tax-plum-300 bg-tax-plum-50 text-tax-plum-700 dark:border-tax-plum-800 dark:bg-tax-plum-950/60 dark:text-tax-plum-300',
    badgeLight: 'bg-tax-plum-50 text-tax-plum-700 dark:bg-tax-plum-950/50 dark:text-tax-plum-300',
    border: 'border-tax-plum-300 dark:border-tax-plum-800',
    borderStrong: 'border-tax-plum-400 dark:border-tax-plum-700',
    accentVar: 'rgb(var(--c-tax-plum-500))',
    bar: 'bg-tax-plum-400 dark:bg-tax-plum-500',
  },
  taxSage: {
    badge:
      'border-tax-sage-300 bg-tax-sage-50 text-tax-sage-700 dark:border-tax-sage-800 dark:bg-tax-sage-950/60 dark:text-tax-sage-300',
    badgeLight: 'bg-tax-sage-50 text-tax-sage-700 dark:bg-tax-sage-950/50 dark:text-tax-sage-300',
    border: 'border-tax-sage-300 dark:border-tax-sage-800',
    borderStrong: 'border-tax-sage-400 dark:border-tax-sage-700',
    accentVar: 'rgb(var(--c-tax-sage-500))',
    bar: 'bg-tax-sage-400 dark:bg-tax-sage-500',
  },
  taxClay: {
    badge:
      'border-tax-clay-300 bg-tax-clay-50 text-tax-clay-700 dark:border-tax-clay-800 dark:bg-tax-clay-950/60 dark:text-tax-clay-300',
    badgeLight: 'bg-tax-clay-50 text-tax-clay-700 dark:bg-tax-clay-950/50 dark:text-tax-clay-300',
    border: 'border-tax-clay-300 dark:border-tax-clay-800',
    borderStrong: 'border-tax-clay-400 dark:border-tax-clay-700',
    accentVar: 'rgb(var(--c-tax-clay-500))',
    bar: 'bg-tax-clay-400 dark:bg-tax-clay-500',
  },
}

// ── 未登记平台 / 无归属：中性灰 ─────────────────────────────────────
const BADGE_DEFAULT = 'border-rule-strong bg-paper-inset text-ink-soft'
const BADGE_LIGHT_DEFAULT = 'bg-paper-inset text-ink-soft'
const BORDER_DEFAULT = 'border-rule'
const BORDER_STRONG_DEFAULT = 'border-rule-strong'
const ACCENT_DEFAULT = 'rgb(var(--c-azure-500))'
const ACCENT_BAR_DEFAULT = 'bg-accent-400 dark:bg-accent-500'

// ── 与平台无关的语义件 ─────────────────────────────────────────────
// 价格是读数，用纸面主墨；平台归属不占用文字颜色。
const TEXT = 'text-ink'
// 勾选 = 通路，用 moss。
const ICON = 'text-success-600 dark:text-success-400'
// 订阅按钮是主操作 → 唯一信号色 cobalt。
const BUTTON =
  'bg-signal text-on-signal hover:bg-primary-600 active:bg-primary-700 dark:hover:bg-primary-400 dark:active:bg-primary-300'
// 折扣是标注/测量值 → ochre。
const DISCOUNT = 'bg-warning-50 text-warning-800 dark:bg-warning-950/60 dark:text-warning-300'

// ── Public API ──────────────────────────────────────────────────────

function hueOf(p: string): PlatformHue | null {
  return (HUE_BY_PLATFORM as Record<string, PlatformHue | undefined>)[p] ?? null
}

export function platformBadgeClass(p: string): string {
  const hue = hueOf(p)
  return hue ? HUE_TOKENS[hue].badge : BADGE_DEFAULT
}

export function platformBadgeLightClass(p: string): string {
  const hue = hueOf(p)
  return hue ? HUE_TOKENS[hue].badgeLight : BADGE_LIGHT_DEFAULT
}

export function platformBorderClass(p: string): string {
  const hue = hueOf(p)
  return hue ? HUE_TOKENS[hue].border : BORDER_DEFAULT
}

export function platformBorderStrongClass(p: string): string {
  const hue = hueOf(p)
  return hue ? HUE_TOKENS[hue].borderStrong : BORDER_STRONG_DEFAULT
}

export function platformAccentColor(p: string): string {
  const hue = hueOf(p)
  return hue ? HUE_TOKENS[hue].accentVar : ACCENT_DEFAULT
}

export function platformAccentBarClass(p: string): string {
  const hue = hueOf(p)
  return hue ? HUE_TOKENS[hue].bar : ACCENT_BAR_DEFAULT
}

export function platformTextClass(_p: string): string {
  return TEXT
}

export function platformIconClass(_p: string): string {
  return ICON
}

export function platformButtonClass(_p: string): string {
  return BUTTON
}

export function platformDiscountClass(_p: string): string {
  return DISCOUNT
}

/** 平台展示名：来自平台清单（后端 domain/platforms.go），新登记的平台同样适用。 */
export function platformLabel(p: string): string {
  if (p === 'composite') return 'Composite'
  return getPlatformSpec(p)?.display_name ?? (p || 'API')
}
