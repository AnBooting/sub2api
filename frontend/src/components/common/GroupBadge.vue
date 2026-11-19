<template>
  <span
    :class="[
      'inline-flex min-w-0 items-center gap-1.5 border border-rule px-2 py-0.5 text-xs font-medium transition-colors',
      badgeClass
    ]"
  >
    <!-- Platform logo -->
    <PlatformIcon v-if="platform" :platform="platform" size="sm" />
    <!-- Group name -->
    <span :class="truncate ? 'truncate' : 'min-w-0 whitespace-normal [overflow-wrap:anywhere]'">{{ name }}</span>
    <!-- Right side label -->
    <span v-if="showLabel" :class="labelClass">
      <template v-if="hasCustomRate">
        <!-- 原倍率删除线 + 专属倍率高亮 -->
        <span class="line-through opacity-50 mr-0.5">{{ rateMultiplier }}x</span>
        <span class="font-bold">{{ userRateMultiplier }}x</span>
      </template>
      <template v-else>
        {{ labelText }}
      </template>
    </span>
    <span v-if="hasPeakRate" :class="peakRateClass" :title="peakRateTitle">
      {{ peakRateText }}
    </span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SubscriptionType, GroupPlatform } from '@/types'
import { useAppStore } from '@/stores/app'
import { formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'
import PlatformIcon from './PlatformIcon.vue'

interface Props {
  name: string
  platform?: GroupPlatform
  subscriptionType?: SubscriptionType
  rateMultiplier?: number
  userRateMultiplier?: number | null // 用户专属倍率
  peakRateEnabled?: boolean
  peakStart?: string
  peakEnd?: string
  peakRateMultiplier?: number
  showRate?: boolean
  /** Allow full group names to wrap in layouts that have room for multiple lines. */
  truncate?: boolean
  daysRemaining?: number | null // 剩余天数（订阅类型时使用）
  /**
   * 订阅分组默认在右侧 label 展示"订阅"或剩余天数；
   * 开启后订阅分组也改为显示倍率（保留订阅主题色 label，配合可用渠道这类
   * 只关心费率、不关心有效期的场景）。
   */
  alwaysShowRate?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  subscriptionType: 'standard',
  showRate: true,
  truncate: true,
  daysRemaining: null,
  userRateMultiplier: null,
  peakRateEnabled: false,
  alwaysShowRate: false
})

const { t } = useI18n()

const isSubscription = computed(() => props.subscriptionType === 'subscription')

// 是否有专属倍率（且与默认倍率不同）
const hasCustomRate = computed(() => {
  return (
    props.userRateMultiplier !== null &&
    props.userRateMultiplier !== undefined &&
    props.rateMultiplier !== undefined &&
    props.userRateMultiplier !== props.rateMultiplier
  )
})

const appStore = useAppStore()

const hasPeakRate = computed(() => {
  return Boolean(props.showRate && props.peakRateEnabled && props.peakStart && props.peakEnd)
})

const peakRateText = computed(() => {
  return formatPeakRateWindow(
    {
      peak_rate_enabled: props.peakRateEnabled,
      peak_start: props.peakStart,
      peak_end: props.peakEnd,
      peak_rate_multiplier: props.peakRateMultiplier
    },
    serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset)
  )
})

const peakRateTitle = computed(() => {
  return t('common.peakRateTooltip', { window: peakRateText.value })
})

// 是否显示右侧标签
const showLabel = computed(() => {
  if (!props.showRate) return false
  // 订阅类型：显示天数或"订阅"
  if (isSubscription.value) return true
  // 标准类型：显示倍率（包括专属倍率）
  return props.rateMultiplier !== undefined || hasCustomRate.value
})

// Label text
const labelText = computed(() => {
  const rateLabel = props.rateMultiplier !== undefined ? `${props.rateMultiplier}x` : ''
  if (isSubscription.value && !props.alwaysShowRate) {
    // 如果有剩余天数，显示天数
    if (props.daysRemaining !== null && props.daysRemaining !== undefined) {
      if (props.daysRemaining <= 0) {
        return t('admin.users.expired')
      }
      return t('admin.users.daysRemaining', { days: props.daysRemaining })
    }
    // 否则显示"订阅"
    return t('groups.subscription')
  }
  return rateLabel
})

// Label style based on type and days remaining
const labelClass = computed(() => {
  const base = 'px-1.5 py-0.5 font-read text-[0.6875rem] font-semibold'

  if (!isSubscription.value) {
    // Standard: subtle background (不再为专属倍率使用不同的背景色)
    // 倍率标记压在分组徽章上：用墨色 10% 而不是纯黑/纯白，暖纸上才不发脏
    return `${base} bg-ink/10`
  }

  // 订阅类型：根据剩余天数显示不同颜色
  if (props.daysRemaining !== null && props.daysRemaining !== undefined) {
    if (props.daysRemaining <= 0 || props.daysRemaining <= 3) {
      // 已过期或紧急（<=3天）：红色
      return `${base} bg-danger-200/80 text-danger-800 dark:bg-danger-800/50 dark:text-danger-300`
    }
    if (props.daysRemaining <= 7) {
      // 警告（<=7天）：橙色
      return `${base} bg-warning-200/80 text-warning-800 dark:bg-warning-800/50 dark:text-warning-300`
    }
  }

  // 正常状态或无天数：根据平台显示主题色
  if (props.platform === 'anthropic') {
    return `${base} bg-warning-200/60 text-warning-800 dark:bg-warning-800/40 dark:text-warning-300`
  }
  if (props.platform === 'openai') {
    return `${base} bg-success-200/60 text-success-800 dark:bg-success-800/40 dark:text-success-300`
  }
  if (props.platform === 'gemini') {
    return `${base} bg-primary-200/60 text-primary-800 dark:bg-primary-800/40 dark:text-primary-300`
  }
  if (props.platform === 'antigravity') {
    return `${base} bg-primary-200/60 text-primary-800 dark:bg-primary-800/40 dark:text-primary-300`
  }
  if (props.platform === 'grok') {
    return `${base} bg-ink/10 text-ink`
  }
  if (props.platform === 'kimi') {
    return `${base} bg-danger-200/60 text-danger-800 dark:bg-danger-800/40 dark:text-danger-300`
  }
  if (props.platform === 'zhipu') {
    return `${base} bg-primary-200/60 text-primary-800 dark:bg-primary-800/40 dark:text-primary-300`
  }
  if (props.platform === 'deepseek') {
    return `${base} bg-success-200/60 text-success-800 dark:bg-success-800/40 dark:text-success-300`
  }
  if (props.platform === 'minimax') {
    return `${base} bg-danger-200/60 text-danger-800 dark:bg-danger-800/40 dark:text-danger-300`
  }
  if (props.platform === 'composite') {
    return `${base} bg-primary-200/70 text-primary-900 dark:bg-primary-900/50 dark:text-primary-300`
  }
  return `${base} bg-primary-200/60 text-primary-800 dark:bg-primary-800/40 dark:text-primary-300`
})

const peakRateClass = computed(() => {
  return 'px-1.5 py-0.5 font-read text-[0.6875rem] font-semibold bg-warning-100 text-warning-700 dark:bg-warning-900/30 dark:text-warning-300'
})

// Badge color based on platform and subscription type
const badgeClass = computed(() => {
  if (props.platform === 'anthropic') {
    // Claude: orange theme
    return isSubscription.value
      ? 'bg-warning-100 text-warning-700 dark:bg-warning-900/30 dark:text-warning-400'
      : 'bg-warning-50 text-warning-700 dark:bg-warning-900/20 dark:text-warning-400'
  } else if (props.platform === 'openai') {
    // OpenAI: green theme
    return isSubscription.value
      ? 'bg-success-100 text-success-700 dark:bg-success-900/30 dark:text-success-400'
      : 'bg-success-50 text-success-700 dark:bg-success-900/20 dark:text-success-400'
  }
  if (props.platform === 'gemini') {
    return isSubscription.value
      ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
      : 'bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-400'
  }
  if (props.platform === 'antigravity') {
    return isSubscription.value
      ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
      : 'bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-400'
  }
  if (props.platform === 'grok') {
    return isSubscription.value
      ? 'bg-ink/12 text-ink'
      : 'bg-ink/8 text-ink-soft'
  }
  if (props.platform === 'kimi') {
    return isSubscription.value
      ? 'bg-danger-100 text-danger-700 dark:bg-danger-900/30 dark:text-danger-400'
      : 'bg-danger-50 text-danger-700 dark:bg-danger-900/20 dark:text-danger-400'
  }
  if (props.platform === 'zhipu') {
    return isSubscription.value
      ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
      : 'bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-400'
  }
  if (props.platform === 'deepseek') {
    return isSubscription.value
      ? 'bg-success-100 text-success-700 dark:bg-success-900/30 dark:text-success-400'
      : 'bg-success-50 text-success-700 dark:bg-success-900/20 dark:text-success-400'
  }
  if (props.platform === 'minimax') {
    return isSubscription.value
      ? 'bg-danger-100 text-danger-700 dark:bg-danger-900/30 dark:text-danger-400'
      : 'bg-danger-50 text-danger-700 dark:bg-danger-900/20 dark:text-danger-400'
  }
  if (props.platform === 'composite') {
    return isSubscription.value
      ? 'bg-primary-100 text-primary-800 dark:bg-primary-900/30 dark:text-primary-300'
      : 'bg-primary-50 text-primary-800 dark:bg-primary-900/20 dark:text-primary-300'
  }
  // Fallback: original colors
  return isSubscription.value
    ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
    : 'bg-success-100 text-success-700 dark:bg-success-900/30 dark:text-success-400'
})
</script>