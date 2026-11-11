/**
 * Centralized platform color definitions.
 *
 * 平台是「归属分类」，不是状态：按令牌契约，分类一律用 accent（azure），
 * 状态（成功/提醒/断线）才用 success / warning / danger。因此本文件不再按平台
 * 铺开十几种色相，所有平台共用一套分类令牌；未登记平台回落中性灰。
 *
 * 唯一例外是 platformAccentColor()：它返回给 CSS color-mix() 用的原始色值，
 * 读的是 design-system.css 里的 --c-azure-500 令牌，不是写死的 hex。
 *
 * 所有组件都应从这里取平台样式，不要各自定义映射。
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

// ── Badge (bg + text + border, for inline badges with border) ───────
// 平台归属 → 分类色 accent；未登记平台 → 中性灰。
const BADGE =
  'border-accent-300 bg-accent-50 text-accent-700 dark:border-accent-800 dark:bg-accent-950/60 dark:text-accent-300'
const BADGE_DEFAULT = 'border-rule-strong bg-paper-inset text-ink-soft'

// ── Light badge (soft bg, no border) ────────────────────────────────
const BADGE_LIGHT = 'bg-accent-50 text-accent-700 dark:bg-accent-950/50 dark:text-accent-300'
const BADGE_LIGHT_DEFAULT = 'bg-paper-inset text-ink-soft'

// ── Border ──────────────────────────────────────────────────────────
const BORDER = 'border-accent-300 dark:border-accent-800'
const BORDER_DEFAULT = 'border-rule'

// ── Border strong (higher-contrast platform tint, e.g. plaza group cards) ──
const BORDER_STRONG = 'border-accent-400 dark:border-accent-700'
const BORDER_STRONG_DEFAULT = 'border-rule-strong'

// ── Accent (single raw color; consumers derive washes/tints from it via
//    CSS color-mix, e.g. plaza paid-price zone). 引用令牌变量，不写死色值。──
const ACCENT = 'rgb(var(--c-azure-500))'
const ACCENT_DEFAULT = 'rgb(var(--c-azure-500))'

// ── Accent bar (平面色条，取代原来的渐变条) ─────────────────────────
const ACCENT_BAR = 'bg-accent-400 dark:bg-accent-500'
const ACCENT_BAR_DEFAULT = 'bg-accent-400 dark:bg-accent-500'

// ── Text (price, icon) ─────────────────────────────────────────────
// 价格是读数，用纸面主墨；平台归属不占用文字颜色。
const TEXT = 'text-ink'
const TEXT_DEFAULT = 'text-ink'

// ── Icon (check mark etc.) ──────────────────────────────────────────
// 勾选 = 通路，用 moss。
const ICON = 'text-success-600 dark:text-success-400'
const ICON_DEFAULT = 'text-success-600 dark:text-success-400'

// ── Button (solid bg) ───────────────────────────────────────────────
// 订阅按钮是主操作 → 唯一信号色 cobalt。
const BUTTON =
  'bg-signal text-on-signal hover:bg-primary-600 active:bg-primary-700 dark:text-dark-950 dark:hover:bg-primary-400 dark:active:bg-primary-300'
const BUTTON_DEFAULT = BUTTON

// ── Discount badge ──────────────────────────────────────────────────
// 折扣是标注/测量值 → ochre。
const DISCOUNT = 'bg-warning-50 text-warning-800 dark:bg-warning-950/60 dark:text-warning-300'
const DISCOUNT_DEFAULT = DISCOUNT

// ── Public API ──────────────────────────────────────────────────────

function isPlatform(p: string): p is Platform {
  return (
    p === 'anthropic' ||
    p === 'openai' ||
    p === 'antigravity' ||
    p === 'gemini' ||
    p === 'grok' ||
    p === 'kimi' ||
    p === 'zhipu' ||
    p === 'deepseek' ||
    p === 'minimax' ||
    p === 'opencode_go' ||
    p === 'typesafe' ||
    p === 'command_code' ||
    p === 'cline' ||
    p === 'composite'
  )
}

export function platformBadgeClass(p: string): string {
  return isPlatform(p) ? BADGE : BADGE_DEFAULT
}

export function platformBadgeLightClass(p: string): string {
  return isPlatform(p) ? BADGE_LIGHT : BADGE_LIGHT_DEFAULT
}

export function platformBorderClass(p: string): string {
  return isPlatform(p) ? BORDER : BORDER_DEFAULT
}

export function platformBorderStrongClass(p: string): string {
  return isPlatform(p) ? BORDER_STRONG : BORDER_STRONG_DEFAULT
}

export function platformAccentColor(p: string): string {
  return isPlatform(p) ? ACCENT : ACCENT_DEFAULT
}

export function platformAccentBarClass(p: string): string {
  return isPlatform(p) ? ACCENT_BAR : ACCENT_BAR_DEFAULT
}

export function platformTextClass(p: string): string {
  return isPlatform(p) ? TEXT : TEXT_DEFAULT
}

export function platformIconClass(p: string): string {
  return isPlatform(p) ? ICON : ICON_DEFAULT
}

export function platformButtonClass(p: string): string {
  return isPlatform(p) ? BUTTON : BUTTON_DEFAULT
}

export function platformDiscountClass(p: string): string {
  return isPlatform(p) ? DISCOUNT : DISCOUNT_DEFAULT
}

/** 平台展示名：来自平台清单（后端 domain/platforms.go），新登记的平台同样适用。 */
export function platformLabel(p: string): string {
  if (p === 'composite') return 'Composite'
  return getPlatformSpec(p)?.display_name ?? (p || 'API')
}
