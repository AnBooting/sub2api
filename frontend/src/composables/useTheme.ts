import { ref } from 'vue'

/**
 * 主题状态的唯一入口。
 *
 * 为什么不让各组件自己 toggle：
 * 主题影响 documentElement 的 class、浏览器栏的 theme-color、以及 localStorage，
 * 三处必须同时改变才不出现「深色纸配浅色系统栏」这种破裂感。
 */

const STORAGE_KEY = 'theme'
const PAPER_LIGHT = '#fafbfc'
const PAPER_DARK = '#0d1218'

export const isDarkTheme = ref(false)

function prefersDark(): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false
  return window.matchMedia('(prefers-color-scheme: dark)').matches
}

function syncMeta(dark: boolean) {
  if (typeof document === 'undefined') return
  const content = dark ? PAPER_DARK : PAPER_LIGHT
  document
    .querySelectorAll('meta[name="theme-color"]')
    .forEach((meta) => meta.setAttribute('content', content))
}

export function applyTheme(dark: boolean, persist = true) {
  if (typeof document === 'undefined') return
  isDarkTheme.value = dark
  document.documentElement.classList.toggle('dark', dark)
  syncMeta(dark)
  if (persist) {
    try {
      localStorage.setItem(STORAGE_KEY, dark ? 'dark' : 'light')
    } catch {
      // 隐私模式下 localStorage 可能抛错，主题仍然生效
    }
  }
}

/** 启动时调用：读存储 → 回落到系统偏好。只读不写，避免首屏覆盖用户系统设置。 */
export function initTheme(): boolean {
  if (typeof window === 'undefined') return false
  let saved: string | null = null
  try {
    saved = localStorage.getItem(STORAGE_KEY)
  } catch {
    saved = null
  }
  const dark = saved === 'dark' || (saved !== 'light' && prefersDark())
  applyTheme(dark, false)
  return dark
}

export function toggleTheme() {
  applyTheme(!isDarkTheme.value, true)
  return isDarkTheme.value
}

/** 跟随系统：用户没有显式选过主题时才生效 */
export function watchSystemTheme() {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return () => {}
  const query = window.matchMedia('(prefers-color-scheme: dark)')
  const handler = (event: MediaQueryListEvent) => {
    let saved: string | null = null
    try {
      saved = localStorage.getItem(STORAGE_KEY)
    } catch {
      saved = null
    }
    if (saved === 'dark' || saved === 'light') return
    applyTheme(event.matches, false)
  }
  query.addEventListener('change', handler)
  return () => query.removeEventListener('change', handler)
}
