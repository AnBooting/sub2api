<template>
  <div
    class="flex h-full min-h-[200px] flex-col items-center justify-center gap-2 rounded-lg border border-amber-200 bg-amber-50 px-4 py-6 text-center dark:border-amber-900/40 dark:bg-amber-900/10"
    data-testid="usage-stats-incomplete"
    role="status"
  >
    <Icon name="exclamationTriangle" size="md" class="text-amber-500 dark:text-amber-400" />
    <p class="text-sm font-medium text-amber-800 dark:text-amber-300">
      {{ title || t('usageStatsIncomplete.title') }}
    </p>
    <p v-if="description !== null" class="max-w-md text-xs text-amber-700 dark:text-amber-400">
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
