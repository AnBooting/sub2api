<template>
  <AppLayout>
   <div class="mx-auto max-w-2xl space-y-6">
   <!-- Current Balance Card -->
   <div class="card overflow-hidden">
   <div class="border-b border-rule px-5 py-5">
   <p class="stat-label">{{ t('redeem.currentBalance') }}</p>
   <p class="mt-1 font-read text-[1.75rem] font-semibold leading-tight tabular-nums text-ink">
   {{ formatCustomerCredit(user?.balance?.toFixed(2) || '0.00') }}
   </p>
   <p class="mt-1 font-read text-[0.6875rem] tabular-nums text-ink-faint">
   {{ t('redeem.concurrency') }} {{ user?.concurrency || 0 }} {{ t('redeem.requests') }}
   </p>
   </div>
   </div>

   <!-- 口径提示：¥1 = 1 Credit（不换汇）；卡密兑换后余额才会变化 -->
   <div
   class="card border-primary-200 bg-primary-50 p-4 text-xs text-primary-800 dark:border-primary-800/50 dark:bg-primary-900/20 dark:text-primary-200"
   data-testid="redeem-credit-hints"
   >
   <p>{{ t('redeem.creditRateHint') }}</p>
   <p class="mt-1">{{ t('redeem.manualCreditHint') }}</p>
   </div>

   <!-- Redeem Form -->
   <div class="card">
   <div class="p-6">
   <form @submit.prevent="handleRedeem" class="space-y-5">
   <div>
   <label for="code" class="input-label">
   {{ t('redeem.redeemCodeLabel') }}
   </label>
   <div class="relative mt-1">
   <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-4">
   <Icon name="gift" size="md" class="text-ink-faint dark:text-ink-faint" />
   </div>
   <input
   id="code"
   v-model="redeemCode"
   type="text"
   required
   :placeholder="t('redeem.redeemCodePlaceholder')"
   :disabled="submitting"
   class="input py-3 pl-12 text-lg"
   />
   </div>
   <p class="input-hint">
   {{ t('redeem.redeemCodeHint') }}
   </p>
   </div>

   <button
   type="submit"
   :disabled="!redeemCode || submitting"
   class="btn btn-primary w-full py-3"
   >
   <svg
   v-if="submitting"
   class="-ml-1 mr-2 h-5 w-5 animate-spin"
   fill="none"
   viewBox="0 0 24 24"
   >
   <circle
   class="opacity-25"
   cx="12"
   cy="12"
   r="10"
   stroke="currentColor"
   stroke-width="4"
   ></circle>
   <path
   class="opacity-75"
   fill="currentColor"
   d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
   ></path>
   </svg>
   <Icon v-else name="checkCircle" size="md" class="mr-2" />
   {{ submitting ? t('redeem.redeeming') : t('redeem.redeemButton') }}
   </button>
   </form>
   </div>
   </div>

   <!-- Success Message -->
   <transition name="fade">
   <div
   v-if="redeemResult"
   class="card border-success-200 bg-success-50 dark:border-success-800/50 dark:bg-success-900/20"
   >
   <div class="p-6">
   <div class="flex items-start gap-4">
   <div
   class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-xl bg-success-100 dark:bg-success-900/30"
   >
   <Icon name="checkCircle" size="md" class="text-success-600 dark:text-success-400" />
   </div>
   <div class="flex-1">
   <h3 class="text-sm font-semibold text-success-800 dark:text-success-300">
   {{ t('redeem.redeemSuccess') }}
   </h3>
   <div class="mt-2 text-sm text-success-700 dark:text-success-400">
   <p>{{ redeemResult.message }}</p>
   <div class="mt-3 space-y-1">
   <p v-if="redeemResult.type === 'balance'" class="font-medium">
   {{ t('redeem.added') }}: {{ formatCustomerCredit(redeemResult.value.toFixed(2)) }}
   </p>
   <p v-else-if="redeemResult.type === 'concurrency'" class="font-medium">
   {{ t('redeem.added') }}: {{ redeemResult.value }}
   {{ t('redeem.concurrentRequests') }}
   </p>
   <p v-else-if="redeemResult.type === 'subscription'" class="font-medium">
   {{ t('redeem.subscriptionAssigned') }}
   <span v-if="redeemResult.group_name"> - {{ redeemResult.group_name }}</span>
   <span v-if="redeemResult.validity_days">
   ({{
   t('redeem.subscriptionDays', { days: redeemResult.validity_days })
   }})</span
   >
   </p>
   <p v-if="redeemResult.new_balance !== undefined">
   {{ t('redeem.newBalance') }}:
   <span class="font-semibold">{{ formatCustomerCredit(redeemResult.new_balance.toFixed(2)) }}</span>
   </p>
   <p v-if="redeemResult.new_concurrency !== undefined">
   {{ t('redeem.newConcurrency') }}:
   <span class="font-semibold"
   >{{ redeemResult.new_concurrency }} {{ t('redeem.requests') }}</span
   >
   </p>
   </div>
   </div>
   </div>
   </div>
   </div>
   </div>
   </transition>

   <!-- Error Message -->
   <transition name="fade">
   <div
   v-if="errorMessage"
   class="card border-danger-200 bg-danger-50 dark:border-danger-800/50 dark:bg-danger-900/20"
   >
   <div class="p-6">
   <div class="flex items-start gap-4">
   <div
   class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-xl bg-danger-100 dark:bg-danger-900/30"
   >
   <Icon
   name="exclamationCircle"
   size="md"
   class="text-danger-600 dark:text-danger-400"
   />
   </div>
   <div class="flex-1">
   <h3 class="text-sm font-semibold text-danger-800 dark:text-danger-300">
   {{ t('redeem.redeemFailed') }}
   </h3>
   <p class="mt-2 text-sm text-danger-700 dark:text-danger-400">
   {{ errorMessage }}
   </p>
   </div>
   </div>
   </div>
   </div>
   </transition>

   <!-- Information Card -->
   <div
   class="card border-primary-200 bg-primary-50 dark:border-primary-800/50 dark:bg-primary-900/20"
   >
   <div class="p-6">
   <div class="flex items-start gap-4">
   <div
   class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-xl bg-primary-100 dark:bg-primary-900/30"
   >
   <Icon name="infoCircle" size="md" class="text-primary-600 dark:text-primary-400" />
   </div>
   <div class="flex-1">
   <h3 class="text-sm font-semibold text-primary-800 dark:text-primary-300">
   {{ t('redeem.aboutCodes') }}
   </h3>
   <ul
   class="mt-2 list-inside list-disc space-y-1 text-sm text-primary-700 dark:text-primary-400"
   >
   <li>{{ t('redeem.codeRule1') }}</li>
   <li>{{ t('redeem.codeRule2') }}</li>
   <li>
   {{ t('redeem.codeRule3') }}
   <span
   v-if="contactInfo"
   class="ml-1.5 inline-flex items-center rounded-md bg-primary-200/50 px-2 py-0.5 text-xs font-medium text-primary-800 dark:bg-primary-800/40 dark:text-primary-200"
   >
   {{ contactInfo }}
   </span>
   </li>
   <li>{{ t('redeem.codeRule4') }}</li>
   </ul>
   </div>
   </div>
   </div>
   </div>

   <!-- Recent Activity -->
   <div class="card">
   <div class="border-b border-rule px-6 py-4 dark:border-rule">
   <h2 class="text-ink">
   {{ t('redeem.recentActivity') }}
   </h2>
   </div>
   <div class="p-6">
   <!-- Loading State -->
   <div v-if="loadingHistory" class="flex items-center justify-center py-8">
   <svg class="h-6 w-6 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24">
   <circle
   class="opacity-25"
   cx="12"
   cy="12"
   r="10"
   stroke="currentColor"
   stroke-width="4"
   ></circle>
   <path
   class="opacity-75"
   fill="currentColor"
   d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
   ></path>
   </svg>
   </div>

   <!-- History List -->
   <div v-else-if="history.length > 0" class="space-y-3">
   <div
   v-for="item in history"
   :key="item.id"
   class="flex items-center justify-between rounded-xl bg-paper-inset p-4 dark:bg-surface"
   >
   <div class="flex items-center gap-4">
   <div
   :class="[ 'flex h-10 w-10 items-center justify-center rounded-xl', isBalanceType(item.type) ? item.value >= 0 ? 'bg-success-100 dark:bg-success-900/30' : 'bg-danger-100 dark:bg-danger-900/30' : isSubscriptionType(item.type) ? 'bg-lilac-100 dark:bg-lilac-900/30' : item.value >= 0 ? 'bg-primary-100 dark:bg-primary-900/30' : 'bg-warning-100 dark:bg-warning-900/30' ]"
   >
   <!-- 余额类型图标 -->
   <Icon
   v-if="isBalanceType(item.type)"
   name="dollar"
   size="md"
   :class="item.value >= 0 ? 'text-success-600 dark:text-success-400' : 'text-danger-600 dark:text-danger-400'"
   />
   <!-- 订阅类型图标 -->
   <Icon
   v-else-if="isSubscriptionType(item.type)"
   name="badge"
   size="md"
   class="text-lilac-600 dark:text-lilac-400"
   />
   <!-- 并发类型图标 -->
   <Icon
   v-else
   name="bolt"
   size="md"
   :class="item.value >= 0 ? 'text-primary-600 dark:text-primary-400' : 'text-warning-600 dark:text-warning-400'"
   />
   </div>
   <div>
   <p class="text-sm font-medium text-ink">
   {{ getHistoryItemTitle(item) }}
   </p>
   <p class="text-xs text-ink-soft dark:text-ink-soft">
   {{ formatDateTime(item.used_at) }}
   </p>
   </div>
   </div>
   <div class="text-right">
   <p
   :class="[ 'text-sm font-semibold', isBalanceType(item.type) ? item.value >= 0 ? 'text-success-600 dark:text-success-400' : 'text-danger-600 dark:text-danger-400' : isSubscriptionType(item.type) ? 'text-lilac-600 dark:text-lilac-400' : item.value >= 0 ? 'text-primary-600 dark:text-primary-400' : 'text-warning-600 dark:text-warning-400' ]"
   >
   {{ formatHistoryValue(item) }}
   </p>
   <p
   v-if="!isAdminAdjustment(item.type)"
   class="font-mono text-xs text-ink-faint dark:text-ink-faint"
   >
   {{ item.code.slice(0, 8) }}...
   </p>
   <p v-else class="text-xs text-ink-faint dark:text-ink-faint">
   {{ t('redeem.adminAdjustment') }}
   </p>
   <!-- Display notes for admin adjustments -->
   <p
   v-if="item.notes"
   class="mt-1 text-xs text-ink-soft dark:text-ink-soft italic max-w-[200px] truncate"
   :title="item.notes"
   >
   {{ item.notes }}
   </p>
   </div>
   </div>
   </div>

   <!-- Empty State -->
   <div v-else class="empty-state py-8">
   <div
   class="mb-4 flex h-16 w-16 items-center justify-center rounded-md bg-paper-inset dark:bg-surface"
   >
   <Icon name="clock" size="xl" class="text-ink-faint dark:text-ink-faint" />
   </div>
   <p class="text-sm text-ink-soft dark:text-ink-soft">
   {{ t('redeem.historyWillAppear') }}
   </p>
   </div>
   <div class="mt-4 flex flex-wrap items-center justify-between gap-3 text-sm">
   <span>{{ t('common.total') }}: {{ historyTotal }} {{ t('pagination.results') }}</span>
   <label>
   {{ t('pagination.perPage') }}
   <select
   v-model="historyPageSize"
   class="input w-20"
   :disabled="loadingHistory || submitting"
   @change="fetchHistory(1)"
   >
   <option v-for="size in [20, 50, 100]" :key="size" :value="size">{{ size }}</option>
   </select>
   </label>
   <button
   class="btn btn-secondary"
   :disabled="loadingHistory || submitting || historyPage <= 1"
   @click="fetchHistory(historyPage - 1)"
   >{{ t('pagination.previous') }}</button>
   <button
   class="btn btn-secondary"
   :disabled="loadingHistory || submitting || historyPage * historyPageSize >= historyTotal"
   @click="fetchHistory(historyPage + 1)"
   >{{ t('pagination.next') }}</button>
   </div>
   </div>
   </div>
   </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { formatCustomerCredit } from '@/utils/billingCredits'
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { useSubscriptionStore } from '@/stores/subscriptions'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { redeemAPI, authAPI, type RedeemHistoryItem } from '@/api'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatDateTime } from '@/utils/format'

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const subscriptionStore = useSubscriptionStore()

const user = computed(() => authStore.user)

const redeemCode = ref('')
const submitting = ref(false)
const redeemResult = ref<{
  message: string
  type: string
  value: number
  new_balance?: number
  new_concurrency?: number
  group_name?: string
  validity_days?: number
} | null>(null)
const errorMessage = ref('')

// History data
const history = ref<RedeemHistoryItem[]>([])
const loadingHistory = ref(false)
const historyPage = ref(1)
const historyPageSize = ref(20)
const historyTotal = ref(0)
let historyRequest = 0
let loadedHistoryPageSize = 20
const contactInfo = ref('')

// Helper functions for history display
const isBalanceType = (type: string) => {
  return type === 'balance' || type === 'admin_balance'
}

const isSubscriptionType = (type: string) => {
  return type === 'subscription'
}

const isAdminAdjustment = (type: string) => {
  return type === 'admin_balance' || type === 'admin_concurrency'
}

const getHistoryItemTitle = (item: RedeemHistoryItem) => {
  if (item.type === 'balance') {
   return t('redeem.balanceAddedRedeem')
  } else if (item.type === 'admin_balance') {
   return item.value >= 0 ? t('redeem.balanceAddedAdmin') : t('redeem.balanceDeductedAdmin')
  } else if (item.type === 'concurrency') {
   return t('redeem.concurrencyAddedRedeem')
  } else if (item.type === 'admin_concurrency') {
   return item.value >= 0 ? t('redeem.concurrencyAddedAdmin') : t('redeem.concurrencyReducedAdmin')
  } else if (item.type === 'subscription') {
   return t('redeem.subscriptionAssigned')
  }
  return t('common.unknown')
}

const formatHistoryValue = (item: RedeemHistoryItem) => {
  if (isBalanceType(item.type)) {
   const sign = item.value >= 0 ? '+' : ''
   return `${sign}${formatCustomerCredit(item.value)}`
  } else if (isSubscriptionType(item.type)) {
   // 订阅类型显示有效天数和分组名称
   const days = item.validity_days || Math.round(item.value)
   const groupName = item.group?.name || ''
   return groupName ? `${days}${t('redeem.days')} - ${groupName}` : `${days}${t('redeem.days')}`
  } else {
   const sign = item.value >= 0 ? '+' : ''
   return `${sign}${item.value} ${t('redeem.requests')}`
  }
}

const fetchHistory = async (page = 1) => {
  const request = ++historyRequest
  const pageSize = historyPageSize.value
  loadingHistory.value = true
  try {
   const result = await redeemAPI.getHistory(page, pageSize)
   if (request !== historyRequest) return
   history.value = result.items
   historyTotal.value = result.total
   historyPage.value = page
   historyPageSize.value = pageSize
   loadedHistoryPageSize = pageSize
  } catch (error) {
   if (request !== historyRequest) return
   historyPageSize.value = loadedHistoryPageSize
   appStore.showError(t('redeem.historyLoadFailed'))
   console.error('Failed to fetch history:', error)
  } finally {
   if (request === historyRequest) loadingHistory.value = false
  }
}

const handleRedeem = async () => {
  if (!redeemCode.value.trim()) {
   appStore.showError(t('redeem.pleaseEnterCode'))
   return
  }

  submitting.value = true
  errorMessage.value = ''
  redeemResult.value = null

  try {
   const result = await redeemAPI.redeem(redeemCode.value.trim())

   redeemResult.value = result

   // Refresh user data to get updated balance/concurrency
   try {
   await authStore.refreshUser()
   } catch (error) {
   console.error('Failed to refresh user after redeem:', error)
   appStore.showWarning(t('redeem.userRefreshFailed'))
   }

   // If subscription type, immediately refresh subscription status
   if (result.type === 'subscription') {
   try {
   await subscriptionStore.fetchActiveSubscriptions(true) // force refresh
   } catch (error) {
   console.error('Failed to refresh subscriptions after redeem:', error)
   appStore.showWarning(t('redeem.subscriptionRefreshFailed'))
   }
   }

   // Clear the input
   redeemCode.value = ''

   // Refresh history
   await fetchHistory()

   // Show success toast
   appStore.showSuccess(t('redeem.codeRedeemSuccess'))
  } catch (error: unknown) {
   // 后端错误信封里没有 detail 字段，稳定错误码在 reason 上（REDEEM_CODE_USED /
   // REDEEM_CODE_NOT_FOUND / REDEEM_CODE_EXPIRED），走仓库既有的 i18n 错误提取方式，
   // 未命中时回落到通用文案。
   errorMessage.value = extractI18nErrorMessage(error, t, 'redeem.errors', t('redeem.failedToRedeem'))

   appStore.showError(t('redeem.redeemFailed'))
  } finally {
   submitting.value = false
  }
}

onMounted(async () => {
  fetchHistory()
  try {
   const settings = await authAPI.getPublicSettings()
   contactInfo.value = settings.contact_info || ''
  } catch (error) {
   console.error('Failed to load contact info:', error)
  }
})
</script>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition: all 0.3s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>