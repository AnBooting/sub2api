<template>
  <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
    <!-- Today Revenue -->
    <div class="card p-4">
      <div class="flex items-center gap-3">
        <div class="rounded-lg bg-success-100 p-2 dark:bg-success-900/30">
          <Icon name="dollar" size="md" class="text-success-600 dark:text-success-400" :stroke-width="2" />
        </div>
        <div>
          <p class="text-xs font-medium text-ink-faint">{{ t('payment.admin.todayRevenue') }}</p>
          <p v-for="[currency, amount] in sortedAmounts(stats.today_amount)" :key="currency" class="text-xl font-bold text-ink">
            {{ formatMoney(currency, amount) }}
          </p>
          <p class="text-xs text-ink-faint">
            {{ stats.today_count }} {{ t('payment.admin.orders') }}
          </p>
        </div>
      </div>
    </div>

    <!-- Total Revenue -->
    <div class="card p-4">
      <div class="flex items-center gap-3">
        <div class="rounded-lg bg-primary-100 p-2 dark:bg-primary-900/30">
          <Icon name="creditCard" size="md" class="text-primary-600 dark:text-primary-400" :stroke-width="2" />
        </div>
        <div>
          <p class="text-xs font-medium text-ink-faint">{{ t('payment.admin.totalRevenue') }}</p>
          <p v-for="[currency, amount] in sortedAmounts(stats.total_amount)" :key="currency" class="text-xl font-bold text-ink">
            {{ formatMoney(currency, amount) }}
          </p>
          <p class="text-xs text-ink-faint">
            {{ stats.total_count }} {{ t('payment.admin.orders') }}
          </p>
        </div>
      </div>
    </div>

    <!-- Today Orders -->
    <div class="card p-4">
      <div class="flex items-center gap-3">
        <div class="rounded-lg bg-primary-100 p-2 dark:bg-primary-900/30">
          <Icon name="chart" size="md" class="text-primary-600 dark:text-primary-400" :stroke-width="2" />
        </div>
        <div>
          <p class="text-xs font-medium text-ink-faint">{{ t('payment.admin.todayOrders') }}</p>
          <p class="text-xl font-bold text-ink">{{ stats.today_count }}</p>
        </div>
      </div>
    </div>

    <!-- Average Amount -->
    <div class="card p-4">
      <div class="flex items-center gap-3">
        <div class="rounded-lg bg-warning-100 p-2 dark:bg-warning-900/30">
          <Icon name="chart" size="md" class="text-warning-600 dark:text-warning-400" :stroke-width="2" />
        </div>
        <div>
          <p class="text-xs font-medium text-ink-faint">{{ t('payment.admin.avgAmount') }}</p>
          <p v-for="[currency, amount] in sortedAmounts(stats.avg_amount)" :key="currency" class="text-xl font-bold text-ink">
            {{ formatMoney(currency, amount) }}
          </p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { CurrencyAmounts, DashboardStats } from '@/types/payment'

const { t } = useI18n()

defineProps<{
  stats: DashboardStats
}>()

function sortedAmounts(amounts: CurrencyAmounts): [string, number][] {
  return Object.entries(amounts).sort(([left], [right]) => left.localeCompare(right))
}

function formatMoney(currency: string, amount: number): string {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(amount)
}
</script>
