<template>
  <div
    class="flex h-full min-h-[200px] flex-col items-center justify-center gap-2 border border-warning-300 bg-warning-50 px-4 py-6 text-center dark:border-warning-800 dark:bg-warning-950/40"
    data-testid="usage-stats-incomplete"
    role="status"
  >
    <Icon name="exclamationTriangle" size="md" class="text-warning-500 dark:text-warning-400" />
    <p class="text-sm font-medium text-warning-800 dark:text-warning-300">
      {{ title || t('usageStatsIncomplete.title') }}
    </p>
    <p v-if="description !== null" class="max-w-md text-xs text-warning-700 dark:text-warning-400">
      {{ description || t('usageStatsIncomplete.description') }}
    </p>
  </div>
</template>

<script setup lang="ts">
/**
 * 「统计不完整，暂不显示该图表」提示（N-02 契约）。
 *
 * 用在**整块图表**因 `503 + usage_stats_incomplete` 而不可用的位置。
 * 关键设计：这是 `role="status"` + 琥珀色，不是红色错误 —— 该状态可重试，
 * 后台补算完成后自然恢复；把它画成红色故障会让用户以为平台坏了。
 *
 * 只要接了 `isUsageStatsIncomplete` 的页面都用这一个组件，避免多套文案。
 */
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

/**
 * `description` 语义：
 *   undefined → 用默认文案（正常情况）
 *   null      → 显式隐藏描述（表格式紧凑位置）
 *   字符串     → 覆盖默认文案
 */
withDefaults(defineProps<{ title?: string; description?: string | null }>(), {
  title: '',
  description: undefined,
})

const { t } = useI18n()
</script>
