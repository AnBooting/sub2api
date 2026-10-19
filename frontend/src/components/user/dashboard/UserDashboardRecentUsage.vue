<template>
  <div class="card">
    <div class="card-header flex items-center justify-between">
      <h2 class="panel-title">{{ t('dashboard.recentUsage') }}</h2>
      <span class="badge badge-gray">{{ t('dashboard.last7Days') }}</span>
    </div>

    <div class="p-4">
      <!-- Loading: skeleton mirrors the row layout instead of a bare spinner -->
      <div v-if="loading" class="space-y-2" aria-busy="true" aria-live="polite">
        <div v-for="i in 5" :key="i" class="flex items-center justify-between rounded-control px-3 py-2.5">
          <div class="flex min-w-0 flex-1 items-center gap-3">
            <div class="skeleton-block h-8 w-8 shrink-0"></div>
            <div class="min-w-0 flex-1 space-y-1.5">
              <div class="skeleton-text w-40 max-w-full"></div>
              <div class="skeleton-text w-24"></div>
            </div>
          </div>
          <div class="flex shrink-0 flex-col items-end gap-1.5">
            <div class="skeleton-text w-20"></div>
            <div class="skeleton-text w-14"></div>
          </div>
        </div>
      </div>

      <!-- Empty: one clear next step -->
      <div v-else-if="data.length === 0" class="state-panel py-10">
        <div class="state-panel-icon">
          <Icon name="chart" size="lg" />
        </div>
        <p class="state-panel-title">{{ t('dashboard.noUsageRecords') }}</p>
        <p class="state-panel-desc">{{ t('dashboard.startUsingApi') }}</p>
        <router-link to="/keys" class="btn btn-primary mt-1">
          {{ t('dashboard.createApiKey') }}
        </router-link>
      </div>

      <div v-else class="space-y-1">
        <div
          v-for="log in data"
          :key="log.id"
          class="flex items-center justify-between gap-3 rounded-control px-3 py-2.5 transition-colors hover:bg-surface-200/70 dark:hover:bg-dark-700/50"
        >
          <div class="flex min-w-0 items-center gap-3">
            <div class="flex h-8 w-8 shrink-0 items-center justify-center rounded-control bg-surface-200 text-ink-500 ring-1 ring-inset ring-ink-900/5 dark:bg-dark-700 dark:text-ink-300 dark:ring-white/5">
              <Icon name="beaker" size="sm" />
            </div>
            <div class="min-w-0">
              <p class="truncate text-sm font-medium text-ink-900 dark:text-white" :title="log.model">{{ log.model }}</p>
              <p class="mt-0.5 text-xs text-ink-500 dark:text-ink-300">{{ formatDateTime(log.created_at) }}</p>
            </div>
          </div>
          <div class="shrink-0 text-right">
            <p class="num text-sm font-semibold">
              <span class="text-ink-900 dark:text-white" :title="t('dashboard.actual')">{{ formatCustomerCredit(formatCost(log.actual_cost)) }}</span>
              <span class="font-normal text-ink-400 dark:text-ink-300" :title="t('dashboard.standard')"> / {{ formatCustomerCredit(formatCost(log.total_cost)) }}</span>
            </p>
            <p class="num mt-0.5 text-xs text-ink-500 dark:text-ink-300">{{ (log.input_tokens + log.output_tokens).toLocaleString() }} tokens</p>
          </div>
        </div>

        <router-link
          to="/usage"
          class="mt-1 flex items-center justify-center gap-1.5 rounded-control border-t border-borderline-100 py-2.5 text-sm font-medium text-primary-600 transition-colors hover:text-primary-700 dark:border-dark-700 dark:text-primary-400 dark:hover:text-primary-300"
        >
          {{ t('dashboard.viewAllUsage') }}
          <Icon name="arrowRight" size="sm" />
        </router-link>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { formatCustomerCredit } from '@/utils/billingCredits'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { formatDateTime } from '@/utils/format'
import type { UsageLog } from '@/types'

defineProps<{
  data: UsageLog[]
  loading: boolean
}>()
const { t } = useI18n()
const formatCost = (c: number) => c.toFixed(4)
</script>
