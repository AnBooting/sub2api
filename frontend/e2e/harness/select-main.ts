import { createApp } from 'vue'
import { testI18n } from './i18n'
import SelectHarness from './SelectHarness.vue'

createApp(SelectHarness).use(testI18n).mount('#app')
