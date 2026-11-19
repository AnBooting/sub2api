<template>
  <AppLayout>
   <div class="space-y-6">
   <!-- Loading State -->
   <div v-if="loading" class="flex justify-center py-12">
   <div
   class="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"
   ></div>
   </div>

   <!-- Empty State -->
   <div v-else-if="subscriptions.length === 0" class="card p-12 text-center">
   <div
   class="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-full bg-paper-inset"
   >
   <Icon name="creditCard" size="xl" class="text-ink-faint" />
   </div>
   <h3 class="mb-2 text-lg font-semibold text-ink">
   {{ t('userSubscriptions.noActiveSubscriptions') }}
   </h3>
   <p class="text-ink-soft">
   {{ t('userSubscriptions.noActiveSubscriptionsDesc') }}
   </p>
   </div>

   <!-- Subscriptions Grid -->
   <div v-else class="grid gap-6 lg:grid-cols-2">
   <div
   v-for="subscription in subscriptions"
   :key="subscription.id"
   class="overflow-hidden border bg-white"
   :class="platformBorderClass(subscription.group?.platform || '')"
   >
   <!-- Header -->
   <div
   class="flex items-center justify-between border-b border-rule p-4"
   >
   <div class="flex items-center gap-3">
   <div :class="['h-1.5 w-1.5 shrink-0 rounded-full', platformAccentDotClass(subscription.group?.platform || '')]" />
   <div>
   <div class="flex items-center gap-2">
   <h3 class="font-semibold text-ink">
   {{ subscription.group?.name || `Group #${subscription.group_id}` }}
   </h3>
   <span :class="['rounded-md border px-2 py-0.5 text-[11px] font-medium', platformBadgeClass(subscription.group?.platform || '')]">
   {{ platformLabel(subscription.group?.platform || '') }}
   </span>
   </div>
   <p v-if="subscription.group?.description" class="mt-0.5 text-xs text-ink-soft">
   {{ subscription.group.description }}
   </p>
   <div class="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-ink-faint">
   <span>{{ t('payment.planCard.rate') }}: ×{{ subscription.group?.rate_multiplier ?? 1 }}</span>
   <span v-if="subscriptionHasPeakRate(subscription)" class="text-warning-700 dark:text-warning-300">
   {{ t('payment.planCard.peakRate') }}: {{ subscriptionPeakRateLabel(subscription) }}
   </span>
   <!-- Live 开关与 Realtime 每分钟单价（分组 DTO 已下发） -->
   <GroupLiveBadge :group="subscription.group" />
   <!-- DeepSeek 峰谷当前状态：状态/文案/倍率都按后端下发，字段缺失时自动隐藏 -->
   <GroupPricingStateBadge :group="subscription.group" />
   </div>
   </div>
   </div>
   <div class="flex items-center gap-2">
   <span
   :class="[ 'rounded-full px-2 py-0.5 text-xs font-medium', subscription.status === 'active' ? 'bg-success-100 text-success-700 dark:bg-success-900/40 dark:text-success-300' : subscription.status === 'expired' ? 'bg-paper-inset text-ink-soft ' : 'bg-danger-100 text-danger-700 dark:bg-danger-900/40 dark:text-danger-300' ]"
   >
   {{ t(`userSubscriptions.status.${subscription.status}`) }}
   </span>
   <button
   v-if="subscription.status === 'active'"
   :class="['rounded-lg px-3 py-1.5 text-xs font-semibold text-on-signal transition-colors', platformButtonClass(subscription.group?.platform || '')]"
   @click="router.push({ path: '/purchase', query: { tab: 'subscription', group: String(subscription.group_id) } })"
   >
   {{ t('payment.renewNow') }}
   </button>
   </div>
   </div>

   <!-- Usage Progress -->
   <div class="space-y-4 p-4">
   <!-- Expiration Info -->
   <div v-if="subscription.expires_at" class="flex items-center justify-between text-sm">
   <span class="text-ink-soft">{{
   t('userSubscriptions.expires')
   }}</span>
   <span :class="getExpirationClass(subscription.expires_at)">
   {{ formatExpirationDate(subscription.expires_at) }}
   </span>
   </div>
   <div v-else class="flex items-center justify-between text-sm">
   <span class="text-ink-soft">{{
   t('userSubscriptions.expires')
   }}</span>
   <span class="text-ink">{{
   t('userSubscriptions.noExpiration')
   }}</span>
   </div>

   <!-- Daily Usage -->
   <div v-if="subscription.group?.daily_limit_usd" class="space-y-2">
   <div class="flex items-center justify-between">
   <span class="text-sm font-medium text-ink">
   {{ t('userSubscriptions.daily') }}
   </span>
   <span class="text-sm text-ink-soft">
   {{ formatCustomerCredit((subscription.daily_usage_usd || 0).toFixed(2)) }} / {{ formatCustomerCredit(subscription.group.daily_limit_usd.toFixed(2)) }}
   </span>
   </div>
   <div class="relative h-2 overflow-hidden rounded-full bg-rule">
   <div
   class="absolute inset-y-0 left-0 rounded-full transition-all duration-300"
   :class="getProgressBarClass( subscription.daily_usage_usd, subscription.group.daily_limit_usd )"
   :style="{
   width: getProgressWidth(
   subscription.daily_usage_usd,
   subscription.group.daily_limit_usd
   )
   }"
   ></div>
   </div>
   <p
   v-if="subscription.daily_window_start"
   class="text-xs text-ink-soft"
   >
   {{ formatDailyUsageWindow(subscription) }}
   </p>
   </div>

   <!-- Weekly Usage -->
   <div v-if="subscription.group?.weekly_limit_usd" class="space-y-2">
   <div class="flex items-center justify-between">
   <span class="text-sm font-medium text-ink">
   {{ t('userSubscriptions.weekly') }}
   </span>
   <span class="text-sm text-ink-soft">
   {{ formatCustomerCredit((subscription.weekly_usage_usd || 0).toFixed(2)) }} / {{ formatCustomerCredit(subscription.group.weekly_limit_usd.toFixed(2)) }}
   </span>
   </div>
   <div class="relative h-2 overflow-hidden rounded-full bg-rule">
   <div
   class="absolute inset-y-0 left-0 rounded-full transition-all duration-300"
   :class="getProgressBarClass( subscription.weekly_usage_usd, subscription.group.weekly_limit_usd )"
   :style="{
   width: getProgressWidth(
   subscription.weekly_usage_usd,
   subscription.group.weekly_limit_usd
   )
   }"
   ></div>
   </div>
   <p
   v-if="subscription.weekly_window_start"
   class="text-xs text-ink-soft"
   >
   {{
   t('userSubscriptions.resetIn', {
   time: formatResetTime(subscription.weekly_window_start, 168)
   })
   }}
   </p>
   </div>

   <!-- Monthly Usage -->
   <div v-if="subscription.group?.monthly_limit_usd" class="space-y-2">
   <div class="flex items-center justify-between">
   <span class="text-sm font-medium text-ink">
   {{ t('userSubscriptions.monthly') }}
   </span>
   <span class="text-sm text-ink-soft">
   {{ formatCustomerCredit((subscription.monthly_usage_usd || 0).toFixed(2)) }} / {{ formatCustomerCredit(subscription.group.monthly_limit_usd.toFixed(2)) }}
   </span>
   </div>
   <div class="relative h-2 overflow-hidden rounded-full bg-rule">
   <div
   class="absolute inset-y-0 left-0 rounded-full transition-all duration-300"
   :class="getProgressBarClass( subscription.monthly_usage_usd, subscription.group.monthly_limit_usd )"
   :style="{
   width: getProgressWidth(
   subscription.monthly_usage_usd,
   subscription.group.monthly_limit_usd
   )
   }"
   ></div>
   </div>
   <p
   v-if="subscription.monthly_window_start"
   class="text-xs text-ink-soft"
   >
   {{
   t('userSubscriptions.resetIn', {
   time: formatResetTime(subscription.monthly_window_start, 720)
   })
   }}
   </p>
   </div>

   <!-- No limits configured - Unlimited badge -->
   <div
   v-if="
   !subscription.group?.daily_limit_usd &&
   !subscription.group?.weekly_limit_usd &&
   !subscription.group?.monthly_limit_usd
  "
   class="flex items-center justify-center border border-success-200 bg-success-50 py-6 dark:border-success-800 dark:bg-success-950/30"
   >
   <div class="flex items-center gap-3">
   <span class="text-4xl text-success-600 dark:text-success-400">∞</span>
   <div>
   <p class="text-sm font-medium text-success-700 dark:text-success-300">
   {{ t('userSubscriptions.unlimited') }}
   </p>
   <p class="text-xs text-success-600/70 dark:text-success-400/70">
   {{ t('userSubscriptions.unlimitedDesc') }}
   </p>
   </div>
   </div>
   </div>
   </div>
   </div>
   </div>
   </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { formatCustomerCredit } from '@/utils/billingCredits'
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/stores/app'
import subscriptionsAPI from '@/api/subscriptions'
import type { UserSubscription } from '@/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import GroupPricingStateBadge from '@/components/user/GroupPricingStateBadge.vue'
import Icon from '@/components/icons/Icon.vue'
import GroupLiveBadge from '@/components/user/GroupLiveBadge.vue'
import { formatDateTimeToMinute } from '@/utils/format'
import { hasPeakRate, formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'
import { platformBorderClass, platformBadgeClass, platformButtonClass, platformLabel } from '@/utils/platformColors'
import {
  getExpirationDateRelation,
  getRemainingDurationParts,
  isOneTimeDailyQuota,
  type RemainingDurationParts
} from '@/utils/subscriptionQuota'

function platformAccentDotClass(p: string): string {
  switch (p) {
   case 'anthropic': return 'bg-warning-500'
   case 'openai': return 'bg-success-500'
   case 'antigravity': return 'bg-primary-500'
   case 'gemini': return 'bg-primary-500'
   default: return 'bg-ink-faint'
  }
}

const { t } = useI18n()
const router = useRouter()
const appStore = useAppStore()

const subscriptions = ref<UserSubscription[]>([])
const loading = ref(true)

function subscriptionHasPeakRate(subscription: UserSubscription): boolean {
  return hasPeakRate(subscription.group)
}

function subscriptionPeakRateLabel(subscription: UserSubscription): string {
  return formatPeakRateWindow(subscription.group, serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset))
}

async function loadSubscriptions() {
  try {
   loading.value = true
   subscriptions.value = await subscriptionsAPI.getMySubscriptions()
  } catch (error) {
   console.error('Failed to load subscriptions:', error)
   appStore.showError(t('userSubscriptions.failedToLoad'))
  } finally {
   loading.value = false
  }
}

function getProgressWidth(used: number | undefined, limit: number | null | undefined): string {
  if (!limit || limit === 0) return '0%'
  const percentage = Math.min(((used || 0) / limit) * 100, 100)
  return `${percentage}%`
}

function getProgressBarClass(used: number | undefined, limit: number | null | undefined): string {
  if (!limit || limit === 0) return 'bg-ink-faint'
  const percentage = ((used || 0) / limit) * 100
  if (percentage >= 90) return 'bg-danger-500'
  if (percentage >= 70) return 'bg-warning-500'
  return 'bg-success-500'
}

function formatExpirationDate(expiresAt: string): string {
  const now = new Date()
  const expires = new Date(expiresAt)
  const diff = expires.getTime() - now.getTime()
  const days = Math.ceil(diff / (1000 * 60 * 60 * 24))
  const relation = getExpirationDateRelation(expires, now)

  if (relation === null) return ''

  if (relation === 'expired') {
   return t('userSubscriptions.status.expired')
  }

  const dateStr = formatDateTimeToMinute(expires)

  if (relation === 'today') {
   return `${dateStr} (${t('common.today')})`
  }
  if (relation === 'tomorrow') {
   return `${dateStr} (${t('common.tomorrow')})`
  }

  return t('userSubscriptions.daysRemaining', { days }) + ` (${dateStr})`
}

function getExpirationClass(expiresAt: string): string {
  const now = new Date()
  const expires = new Date(expiresAt)
  const diff = expires.getTime() - now.getTime()
  const days = Math.ceil(diff / (1000 * 60 * 60 * 24))

  if (diff <= 0) return 'text-danger-600 dark:text-danger-400 font-medium'
  if (days <= 3) return 'text-danger-600 dark:text-danger-400'
  if (days <= 7) return 'text-warning-600 dark:text-warning-400'
  return 'text-ink'
}

function formatDurationParts(parts: RemainingDurationParts): string {
  if (parts.days > 0) {
   return `${parts.days}d ${parts.hours}h`
  }

  if (parts.hours > 0) {
   return `${parts.hours}h ${parts.minutes}m`
  }

  return `${parts.minutes}m`
}

function formatDailyUsageWindow(subscription: UserSubscription): string {
  if (isOneTimeDailyQuota(subscription) && subscription.expires_at) {
   const parts = getRemainingDurationParts(subscription.expires_at)
   if (!parts) return t('userSubscriptions.windowNotActive')
   return t('userSubscriptions.quotaEndsIn', { time: formatDurationParts(parts) })
  }

  return t('userSubscriptions.resetIn', {
   time: formatResetTime(subscription.daily_window_start, 24)
  })
}

function formatResetTime(windowStart: string | null, windowHours: number): string {
  if (!windowStart) return t('userSubscriptions.windowNotActive')

  const start = new Date(windowStart)
  const end = new Date(start.getTime() + windowHours * 60 * 60 * 1000)
  const parts = getRemainingDurationParts(end)

  return parts ? formatDurationParts(parts) : t('userSubscriptions.windowNotActive')
}

onMounted(() => {
  loadSubscriptions()
})
</script>
