<template>
  <span
    v-if="badge"
    class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium"
    :class="badge.className"
    :title="badge.title"
    data-testid="deepseek-pricing-state-badge"
  >
    <span>{{ badge.text }}</span>
    <span v-if="badge.showMultiplier" class="opacity-80">{{ multiplierText }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DeepSeekPricingState } from '@/api/admin/groups'

const props = defineProps<{
  /** 后端下发的峰谷状态；缺失（老数据 / 接口未带）时为 null / undefined。 */
  state?: DeepSeekPricingState | null
}>()

const { t } = useI18n()

type Badge = {
  text: string
  className: string
  title: string
  showMultiplier: boolean
}

// 字段缺失时整体不渲染徽标：后端尚未下发该字段时，界面不应报错或显示空壳。
const badge = computed<Badge | null>(() => {
  const state = props.state
  if (!state || typeof state.state !== 'string' || !state.state) return null

  const multiplier = typeof state.multiplier === 'number' && Number.isFinite(state.multiplier)
    ? state.multiplier
    : null
  const label = typeof state.label === 'string' && state.label.trim() ? state.label.trim() : ''
  const timezone = typeof state.timezone === 'string' && state.timezone.trim() ? state.timezone.trim() : ''
  // 后端下发的时区一并展示，让管理员知道判定基于哪个时区。
  const hint = timezone
    ? `${t('admin.groups.peakState.autoJudged')} (${timezone})`
    : t('admin.groups.peakState.autoJudged')

  if (state.state === 'peak') {
    return {
      text: label || t('admin.groups.peakState.peak'),
      className: 'bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-300',
      title: hint,
      showMultiplier: multiplier !== null && multiplier !== 1
    }
  }
  if (state.state === 'valley') {
    return {
      text: label || t('admin.groups.peakState.valley'),
      className: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400',
      title: hint,
      showMultiplier: multiplier !== null && multiplier !== 1
    }
  }
  // "none" 以及后端将来新增的未知取值都按「未启用」处理，不把未知状态当错误。
  return {
    text: label || t('admin.groups.peakState.none'),
    className: 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300',
    title: hint,
    showMultiplier: false
  }
})

const multiplierText = computed(() => {
  const multiplier = typeof props.state?.multiplier === 'number' ? props.state.multiplier : null
  if (multiplier === null || !Number.isFinite(multiplier)) return ''
  return `${Number(multiplier.toFixed(4))}x`
})
</script>
