import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import i18n, { initI18n } from './i18n'
import { useAppStore } from '@/stores/app'
import { updateFavicon } from '@/utils/branding'
import { initTheme, watchSystemTheme } from '@/composables/useTheme'
import './style.css'

async function bootstrap() {
  // 主题在挂载前定下来：class、浏览器栏色、存储三者一次对齐，避免首屏闪一下浅色。
  // 输入控件字号已统一 ≥16px（见 style.css 的 .input），不再需要冻结 maximum-scale，
  // 用户仍可双指放大 —— 这是可访问性要求。
  initTheme()
  watchSystemTheme()

  const app = createApp(App)
  const pinia = createPinia()
  app.use(pinia)

  // Initialize settings from injected config BEFORE mounting (prevents flash)
  // This must happen after pinia is installed but before router and i18n
  const appStore = useAppStore()
  appStore.initFromInjectedConfig()

  // Set document title immediately after config is loaded
  if (appStore.siteName && appStore.siteName !== 'Sub2API') {
    document.title = `${appStore.siteName} - AI API Gateway`
  }
  updateFavicon(appStore.siteLogo)

  await initI18n()

  app.use(router)
  app.use(i18n)

  // 等待路由器完成初始导航后再挂载，避免竞态条件导致的空白渲染
  await router.isReady()
  app.mount('#app')
}

bootstrap()
