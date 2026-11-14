// 图表数据序列色板 —— 唯一来源：frontend/src/styles/design-system.css
// 规则：第 1 序列永远是信号色（cobalt），其后是测量色与中性灰。
// lilac 只在这里出现，界面元素不得引用。
import { computed } from 'vue'

function readVar(name: string, fallback: string): string {
  if (typeof window === 'undefined' || typeof document === 'undefined') return fallback
  const raw = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  if (!raw) return fallback
  return `rgb(${raw.split(/\s+/).join(' ')})`
}

export interface ChartPalette {
  series: string[]
  grid: string
  axis: string
  ink: string
  inkSoft: string
  surface: string
}

const SERIES_VARS = [
  '--c-cobalt-500',
  '--c-ochre-500',
  '--c-moss-500',
  '--c-oxide-500',
  '--c-lilac-500',
  '--c-graphite-500',
]

const SERIES_FALLBACK = [
  'rgb(58 99 240)',
  'rgb(188 131 20)',
  'rgb(84 132 67)',
  'rgb(192 91 69)',
  'rgb(120 103 170)',
  'rgb(102 113 125)',
]

export function chartPalette(): ChartPalette {
  return {
    series: SERIES_VARS.map((name, index) => readVar(name, SERIES_FALLBACK[index])),
    grid: readVar('--rule', 'rgb(229 233 237)'),
    axis: readVar('--ink-faint', 'rgb(120 130 142)'),
    ink: readVar('--ink', 'rgb(15 20 26)'),
    inkSoft: readVar('--ink-soft', 'rgb(90 99 110)'),
    surface: readVar('--surface', 'rgb(255 255 255)'),
  }
}

/** 响应式：主题切换后重新解析（dark class 变化由调用方的 watch 触发） */
export function useChartPalette() {
  return computed(() => chartPalette())
}

/** 给 Chart.js 用：半透明版本 */
export function withAlpha(color: string, alpha: number): string {
  const match = color.match(/rgb\((\d+)[\s,]+(\d+)[\s,]+(\d+)/)
  if (!match) return color
  return `rgba(${match[1]}, ${match[2]}, ${match[3]}, ${alpha})`
}
