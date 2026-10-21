<template>
  <span
    v-if="state"
    class="inline-flex items-center gap-1 text-[11px]"
    data-testid="group-pricing-state"
  >
    <span
      class="rounded px-1.5 py-0.5 font-medium"
      :class="badgeClass"
      :title="tooltip"
      data-testid="group-pricing-state-badge"
    >
      {{ labelText }}
      <span v-if="multiplierText" class="ml-0.5 font-mono">{{ multiplierText }}</span>
    </span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { readDeepSeekPricingState, type DeepSeekPricingState } from './deepseekPricingState'

/** 判定时区兜底：官方峰谷时段按 Asia/Shanghai 判定。 */
const DEFAULT_TIMEZONE = 'Asia/Shanghai'

const props = defineProps<{ group?: unknown }>()
const { t } = useI18n()

/** 字段缺失（老数据/接口没带）时为 null → 不渲染徽标，也不报错。 */
const state = computed<DeepSeekPricingState | null>(() => readDeepSeekPricingState(props.group))

/** 文案优先用后端给的 label；缺失时按 state 用本地文案兜底。 */
const labelText = computed(() => {
  const current = state.value
  if (!current) return ''
  if (current.label) return current.label
  switch (current.state) {
    case 'peak':
      return t('groupPricingState.badgePeak')
    case 'valley':
      return t('groupPricingState.badgeValley')
    default:
      return t('groupPricingState.badgeNone')
  }
})

const multiplierText = computed(() => {
  const multiplier = state.value?.multiplier
  if (multiplier == null || !Number.isFinite(multiplier)) return ''
  return `×${Number(multiplier.toPrecision(6))}`
})

/** 峰=警示色，谷=正常色，未启用=灰色。 */
const badgeClass = computed(() => {
  switch (state.value?.state) {
    case 'peak':
      return 'bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-200'
    case 'valley':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
    default:
      return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
  }
})

const tooltip = computed(() =>
  t('groupPricingState.tooltip', { timezone: state.value?.timezone || DEFAULT_TIMEZONE }),
)
</script>
