import { createApp } from 'vue'
import { testI18n } from './i18n'
import DateRangeHarness from './DateRangeHarness.vue'

createApp(DateRangeHarness).use(testI18n).mount('#app')
