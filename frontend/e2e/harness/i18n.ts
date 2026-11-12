import { createI18n } from 'vue-i18n'

/**
 * 测试 harness 专用的最小 i18n 实例。
 *
 * 不引入 @/i18n 的原因：应用侧实例会连带拉起 router / pinia store，
 * harness 只挂单个组件，不需要这些副作用。缺键时 t() 返回键名本身，
 * 断言都基于结构与 class，不依赖文案。
 */
export const testI18n = createI18n({
  legacy: false,
  locale: 'en',
  fallbackLocale: 'en',
  missingWarn: false,
  fallbackWarn: false,
  messages: {
    en: {
      common: {
        selectOption: 'Select option',
        searchPlaceholder: 'Search',
        noOptionsFound: 'No options',
        loading: 'Loading',
        search: 'Search'
      },
      dates: {
        today: 'Today',
        yesterday: 'Yesterday',
        last7Days: 'Last 7 days',
        last30Days: 'Last 30 days',
        startDate: 'Start date',
        endDate: 'End date',
        apply: 'Apply'
      }
    }
  }
})
