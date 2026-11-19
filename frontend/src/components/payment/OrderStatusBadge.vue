<template>
  <span
    class="inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium"
    :class="statusClass"
  >
    {{ statusLabel }}
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { OrderStatus } from '@/types/payment'

const props = defineProps<{
  status: OrderStatus
}>()

const { t } = useI18n()

const statusMap: Record<OrderStatus, { key: string; class: string }> = {
  PENDING: { key: 'payment.status.pending', class: 'bg-warning-100 text-warning-800 dark:bg-warning-900/30 dark:text-warning-400' },
  PAID: { key: 'payment.status.paid', class: 'bg-primary-100 text-primary-800 dark:bg-primary-900/30 dark:text-primary-400' },
  RECHARGING: { key: 'payment.status.recharging', class: 'bg-primary-100 text-primary-800 dark:bg-primary-900/30 dark:text-primary-400' },
  COMPLETED: { key: 'payment.status.completed', class: 'bg-success-100 text-success-800 dark:bg-success-900/30 dark:text-success-400' },
  EXPIRED: { key: 'payment.status.expired', class: 'bg-gray-100 text-gray-800 dark:bg-dark-900/30 dark:text-ink-soft' },
  CANCELLED: { key: 'payment.status.cancelled', class: 'bg-gray-100 text-gray-800 dark:bg-dark-900/30 dark:text-ink-soft' },
  FAILED: { key: 'payment.status.failed', class: 'bg-danger-100 text-danger-800 dark:bg-danger-900/30 dark:text-danger-400' },
  REFUND_REQUESTED: { key: 'payment.status.refund_requested', class: 'bg-warning-100 text-warning-800 dark:bg-warning-900/30 dark:text-warning-400' },
  REFUNDING: { key: 'payment.status.refunding', class: 'bg-warning-100 text-warning-800 dark:bg-warning-900/30 dark:text-warning-400' },
  REFUND_PENDING: { key: 'payment.status.refund_pending', class: 'bg-warning-100 text-warning-800 dark:bg-warning-900/30 dark:text-warning-400' },
  REFUNDED: { key: 'payment.status.refunded', class: 'bg-primary-100 text-primary-800 dark:bg-primary-900/30 dark:text-primary-400' },
  PARTIALLY_REFUNDED: { key: 'payment.status.partially_refunded', class: 'bg-primary-100 text-primary-800 dark:bg-primary-900/30 dark:text-primary-400' },
  REFUND_FAILED: { key: 'payment.status.refund_failed', class: 'bg-danger-100 text-danger-800 dark:bg-danger-900/30 dark:text-danger-400' },
}

const statusLabel = computed(() => {
  const entry = statusMap[props.status]
  return entry ? t(entry.key) : props.status
})

const statusClass = computed(() => {
  const entry = statusMap[props.status]
  return entry?.class ?? 'bg-gray-100 text-gray-800 dark:bg-dark-900/30 dark:text-ink-soft'
})
</script>
