<template>
  <span v-if="props.quotas === undefined" class="text-xs text-gray-400 dark:text-gray-500">…</span>
  <span v-else-if="configured.length === 0" class="text-xs text-gray-400 dark:text-gray-500">
    {{ t('admin.users.platformQuota.cellNotConfigured') }}
  </span>
  <div v-else class="space-y-0.5 text-xs">
    <div
      v-for="row in configured"
      :key="row.platform"
      class="flex items-center gap-2 whitespace-nowrap"
    >
      <span class="w-20 shrink-0 font-mono text-gray-700 dark:text-gray-300">{{ row.platform }}</span>
      <span class="text-gray-500 dark:text-gray-400">
        {{ t('admin.users.platformQuota.windowDaily') }}
        <span class="text-gray-900 dark:text-white">{{ fmtUsage(row.daily_usage_usd) }}/{{ fmtLimit(row.daily_limit_usd) }}</span>
      </span>
      <span class="text-gray-500 dark:text-gray-400">
        {{ t('admin.users.platformQuota.windowWeekly') }}
        <span class="text-gray-900 dark:text-white">{{ fmtUsage(row.weekly_usage_usd) }}/{{ fmtLimit(row.weekly_limit_usd) }}</span>
      </span>
      <span class="text-gray-500 dark:text-gray-400">
        {{ t('admin.users.platformQuota.windowMonthly') }}
        <span class="text-gray-900 dark:text-white">{{ fmtUsage(row.monthly_usage_usd) }}/{{ fmtLimit(row.monthly_limit_usd) }}</span>
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatCustomerCredit } from '@/utils/billingCredits'
import type { PlatformQuotaItem } from '@/api/admin/users'
import { listPlatformIds } from '@/constants/platformCatalog'

const props = defineProps<{ quotas?: PlatformQuotaItem[] }>()
const { t } = useI18n()

// 按平台清单顺序展示；未登记的平台排在最后。
function platformRank(platform: string): number {
  const index = listPlatformIds().indexOf(platform)
  return index === -1 ? Number.MAX_SAFE_INTEGER : index
}

// 仅展示「至少一档限额非空」的平台（配额列，非用量列）
const configured = computed(() => {
  if (!props.quotas) return []
  return props.quotas
    .filter(
      (q) =>
        q.daily_limit_usd != null ||
        q.weekly_limit_usd != null ||
        q.monthly_limit_usd != null
    )
    .slice()
    .sort((a, b) => platformRank(a.platform) - platformRank(b.platform))
})

// 账户口径统一用 Credit（客户账本单位），与充值页/订单页一致，不再混用 $ 前缀。
function fmtUsage(n: number): string {
  if (n == null || Number.isNaN(n)) return formatCustomerCredit(0)
  return formatCustomerCredit(n)
}
function fmtLimit(n: number | null): string {
  return n == null ? '—' : formatCustomerCredit(n)
}
</script>
