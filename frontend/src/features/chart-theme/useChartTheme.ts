/**
 * 图表主题桥：把「纸面明暗」变成一次可响应信号。
 *
 * 颜色仍然只来自 @/styles/chartPalette（唯一序列色来源），本文件不写任何色值。
 * 之所以需要它：主题切换改的是 <html class="dark">，没有任何响应式来源，
 * 单纯 computed(() => document.documentElement.classList...) 在切主题后不会重算，
 * 图表会停留在上一个纸面的配色。
 */
import { getCurrentScope, onScopeDispose, readonly, ref } from 'vue'
import { chartPalette, type ChartPalette } from '@/styles/chartPalette'

function currentIsDark(): boolean {
  return typeof document !== 'undefined' && document.documentElement.classList.contains('dark')
}

export function useChartTheme() {
  const isDark = ref(currentIsDark())
  const palette = ref<ChartPalette>(chartPalette())
  let observer: MutationObserver | null = null

  if (typeof document !== 'undefined' && typeof MutationObserver !== 'undefined') {
    observer = new MutationObserver(() => {
      const next = currentIsDark()
      if (next === isDark.value) return
      isDark.value = next
      palette.value = chartPalette()
    })
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    if (getCurrentScope()) {
      onScopeDispose(() => observer?.disconnect())
    }
  }

  return { isDark: readonly(isDark), palette: readonly(palette) }
}
