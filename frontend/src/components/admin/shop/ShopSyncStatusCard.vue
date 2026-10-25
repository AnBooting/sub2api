<template>
  <section class="card" data-testid="shop-sync-status">
    <div class="flex flex-wrap items-start gap-3 px-6 py-4">
      <div class="min-w-0 flex-1">
        <h2 class="text-sm font-medium text-gray-900 dark:text-white">
          {{ t('admin.shopSync.title') }}
        </h2>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.shopSync.description') }}
        </p>
      </div>
      <span class="badge" :class="stateClass" data-testid="shop-sync-state">{{ stateText }}</span>
    </div>

    <div class="grid gap-3 border-t border-gray-100 px-6 py-4 text-sm sm:grid-cols-3 dark:border-dark-700">
      <div>
        <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.shopSync.lastSuccessAt') }}</p>
        <p class="mt-0.5 font-medium text-gray-900 dark:text-white" data-testid="shop-sync-last-success">
          {{ lastSuccessText }}
        </p>
      </div>
      <div>
        <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.shopSync.lastAttemptAt') }}</p>
        <p class="mt-0.5 font-medium text-gray-900 dark:text-white" data-testid="shop-sync-last-attempt">
          {{ lastAttemptText }}
        </p>
      </div>
      <div>
        <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.shopSync.productCount') }}</p>
        <p class="mt-0.5 font-medium text-gray-900 dark:text-white" data-testid="shop-sync-product-count">
          {{ productCount }}
        </p>
      </div>
    </div>

    <p
      v-if="errorText"
      class="px-6 pb-4 text-sm font-medium text-amber-600 dark:text-amber-400"
      :title="rawErrorCode"
      data-testid="shop-sync-error"
    >
      {{ errorText }}
    </p>
    <p
      v-if="paused"
      class="px-6 pb-4 text-sm font-medium text-amber-600 dark:text-amber-400"
      data-testid="shop-sync-paused"
    >
      {{ t('admin.shopSync.paused') }}
    </p>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatDateTime } from '@/utils/format'
import { getShopSyncStatus, type AdminShopSyncStatus } from '@/api/admin/shop'
import { shopSyncErrorI18nKey } from './shopSyncError'

const { t } = useI18n()

// 状态条是只读运维视图：进入页面拉一次，之后按固定周期刷新，避免把上游故障
// 变成管理端的缓存快照。后端该接口在未配置/上游不可用时同样返回 200。
const POLL_INTERVAL_MS = 30_000

const status = ref<AdminShopSyncStatus | null>(null)
const loadFailed = ref(false)
let timer: ReturnType<typeof setInterval> | undefined
let disposed = false

const rawErrorCode = computed(() => (status.value?.error ?? '').trim())
const isNotConfigured = computed(() => rawErrorCode.value === 'shop_not_configured')

const stateText = computed(() => {
  if (loadFailed.value) return t('admin.shopSync.stateUnknown')
  if (!status.value) return t('admin.shopSync.stateUnknown')
  if (!rawErrorCode.value) return t('admin.shopSync.stateOk')
  if (isNotConfigured.value) return t('admin.shopSync.stateNotConfigured')
  return t('admin.shopSync.stateFailed')
})

const stateClass = computed(() => {
  if (loadFailed.value || !status.value) return 'badge-gray'
  if (!rawErrorCode.value) return 'badge-success'
  if (isNotConfigured.value) return 'badge-gray'
  return 'badge-warning'
})

// 已知错误码翻成可读文案；未知码回落成通用文案，原始码只留在 title 里便于排查。
const errorText = computed(() => {
  if (loadFailed.value) return t('admin.shopSync.loadFailed')
  const key = shopSyncErrorI18nKey(rawErrorCode.value)
  return key ? t(key) : ''
})

const paused = computed(() => status.value?.paused === true)
const productCount = computed(() => status.value?.product_count ?? 0)

const formatTimestamp = (value: string | null | undefined): string => {
  if (!value || value.startsWith('0001-')) return ''
  return formatDateTime(value)
}

const lastSuccessText = computed(() => {
  const formatted = formatTimestamp(status.value?.last_success_at)
  return formatted || t('admin.shopSync.neverSynced')
})

const lastAttemptText = computed(() => {
  const formatted = formatTimestamp(status.value?.last_attempt_at)
  return formatted || t('admin.shopSync.neverAttempted')
})

async function load() {
  try {
    const next = await getShopSyncStatus()
    if (disposed) return
    status.value = next
    loadFailed.value = false
  } catch (err) {
    if (disposed) return
    // 状态接口本身失败时不能把管理页带崩：保留上一次已知状态并提示无法获取。
    console.error('Failed to load shop sync status:', err)
    loadFailed.value = true
  }
}

onMounted(async () => {
  await load()
  if (disposed) return
  timer = setInterval(load, POLL_INTERVAL_MS)
})

onUnmounted(() => {
  disposed = true
  if (timer) clearInterval(timer)
})
</script>
