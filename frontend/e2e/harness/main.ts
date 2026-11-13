import { createApp } from 'vue'
import { createPinia } from 'pinia'
import DialogHarness from './DialogHarness.vue'
import { testI18n } from './i18n'

createApp(DialogHarness).use(createPinia()).use(testI18n).mount('#app')
