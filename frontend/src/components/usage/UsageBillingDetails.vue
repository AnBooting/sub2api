<template>
  <div class="space-y-4" data-testid="usage-billing-details">
    <dl class="grid grid-cols-1 gap-3 text-sm sm:grid-cols-2">
      <div><dt class="text-gray-500">{{ t('usage.model') }}</dt><dd class="break-words font-medium">{{ record.model }}</dd></div>
      <div><dt class="text-gray-500">{{ t('usage.recordedGroup') }}</dt><dd>{{ record.group?.name ?? (record.group_id != null ? '#' + record.group_id : '—') }}</dd></div>
      <div><dt class="text-gray-500">{{ t('usage.requestedGroup') }}</dt><dd data-testid="requested-group">{{ groupId(record.requested_group_id) }}</dd></div>
      <div><dt class="text-gray-500">{{ t('usage.servedGroup') }}</dt><dd data-testid="served-group">{{ groupId(servedGroup) }}<span v-if="record.served_group_id == null && servedGroup != null" class="ml-1 text-xs text-gray-500">{{ t('usage.fromSnapshot') }}</span></dd></div>
      <div><dt class="text-gray-500">{{ t('usage.failover') }}</dt><dd data-testid="usage-failover">{{ record.failover != null ? t('usage.failoverKinds.' + record.failover) : '—' }}</dd></div>
      <div><dt class="text-gray-500">{{ t('usage.attemptCount') }}</dt><dd data-testid="attempt-count">{{ record.attempt_count ?? '—' }}</dd></div>
      <div><dt class="text-gray-500">{{ t('usage.recordedCredit') }}</dt><dd data-testid="recorded-credit">{{ formatStoredCustomerCredit(record.actual_cost) }}</dd></div>
    </dl>
    <!-- Live 会话：展示可用的时长与计费口径（后端当前不提供实时语音时长字段） -->
    <div
      v-if="isLive"
      class="rounded-lg bg-emerald-50 p-3 text-sm text-emerald-900 dark:bg-emerald-900/20 dark:text-emerald-100"
      data-testid="usage-live-details"
    >
      <p class="font-medium">{{ t('usage.liveTitle') }}</p>
      <dl class="mt-2 grid grid-cols-1 gap-2 sm:grid-cols-2">
        <div>
          <dt class="text-gray-500 dark:text-emerald-200/70">{{ t('usage.liveSessionDuration') }}</dt>
          <dd data-testid="usage-live-duration">{{ liveDuration }}</dd>
        </div>
        <div>
          <dt class="text-gray-500 dark:text-emerald-200/70">{{ t('usage.liveBilling') }}</dt>
          <dd data-testid="usage-live-billing">{{ liveBilling }}</dd>
        </div>
      </dl>
    </div>
    <p v-if="!snapshot" class="rounded-lg bg-gray-50 p-3 text-sm text-gray-500 dark:bg-dark-700" data-testid="missing-snapshot">{{ t('usage.missingLockedSnapshot') }}</p>
    <template v-else>
      <p v-if="conflicts.length" role="alert" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-700 dark:bg-amber-900/20 dark:text-amber-300" data-testid="snapshot-conflict">{{ t('usage.snapshotConflict') }}</p>
      <h3 class="text-sm font-semibold">{{ t('usage.lockedSnapshot') }}</h3>
      <dl class="grid grid-cols-1 gap-3 text-sm sm:grid-cols-2">
        <div><dt class="text-gray-500">{{ t('usage.requestStartedAt') }}</dt><dd class="break-words">{{ snapshot.request_started_at ?? '—' }}</dd></div>
        <div><dt class="text-gray-500">{{ t('usage.priceVersion') }}</dt><dd class="break-all">{{ snapshot.price_version ?? '—' }}</dd></div>
        <div><dt class="text-gray-500">{{ t('usage.priceSource') }}</dt><dd>{{ snapshot.source ?? '—' }}</dd></div>
        <div><dt class="text-gray-500">{{ t('usage.sourceCurrency') }}</dt><dd data-testid="source-currency">{{ snapshot.currency ?? '—' }}</dd></div>
        <div><dt class="text-gray-500">{{ t('usage.pricingUnit') }}</dt><dd>{{ snapshot.unit ?? '—' }}</dd></div>
        <div><dt class="text-gray-500">{{ t('usage.lockedPeriod') }}</dt><dd>{{ snapshot.period ? [snapshot.period.label, snapshot.period.timezone, snapshot.period.window].filter((value) => value != null).join(' · ') : '—' }}</dd></div>
        <div><dt class="text-gray-500">{{ t('usage.snapshotServedGroup') }}</dt><dd>{{ groupId(snapshot.served_group_id) }}</dd></div>
        <div><dt class="text-gray-500">{{ t('usage.lockedMultiplier') }}</dt><dd data-testid="locked-multiplier">{{ storedValue(snapshot.group_multiplier) }}</dd></div>
        <div><dt class="text-gray-500">{{ t('usage.lockedTotalCredit') }}</dt><dd data-testid="locked-credit">{{ formatStoredCustomerCredit(snapshot.total_credit) }}</dd></div>
      </dl>
      <div class="overflow-x-auto">
        <table class="w-full text-left text-xs" data-testid="locked-price-items">
          <caption class="sr-only">{{ t('usage.lockedSnapshot') }}</caption>
          <thead><tr><th scope="col" class="p-2">{{ t('usage.priceKind') }}</th><th scope="col" class="p-2">{{ t('usage.baseCreditPrice') }}</th><th scope="col" class="p-2">{{ t('usage.storedUsage') }}</th><th scope="col" class="p-2">{{ t('usage.normalizedUsage') }}</th><th scope="col" class="p-2">{{ t('usage.subtotalCredit') }}</th></tr></thead>
          <tbody><tr v-for="(item, index) in snapshot.items ?? []" :key="index" class="border-t border-gray-100 dark:border-dark-600"><th scope="row" class="p-2 font-medium">{{ item.kind }}</th><td class="p-2">{{ formatStoredCustomerCredit(item.base_credit_price) }}</td><td class="p-2">{{ storedValue(item.usage) }}</td><td class="p-2">{{ storedValue(item.normalized_usage) }}</td><td class="p-2">{{ formatStoredCustomerCredit(item.subtotal) }}</td></tr></tbody>
        </table>
      </div>
      <p v-if="!snapshot.items?.length" class="text-xs text-gray-500">{{ t('usage.missingStoredPriceItems') }}</p>
      <dl v-if="snapshot.media" class="grid grid-cols-1 gap-3 text-sm sm:grid-cols-2" data-testid="locked-media">
        <div><dt class="text-gray-500">{{ t('usage.mediaBillingMode') }}</dt><dd>{{ snapshot.media.mode ?? '—' }}</dd></div>
        <div><dt class="text-gray-500">{{ t('usage.mediaBillableCount') }}</dt><dd>{{ snapshot.media.billable_count ?? '—' }}</dd></div>
        <div><dt class="text-gray-500">{{ t('usage.mediaBillableSeconds') }}</dt><dd>{{ snapshot.media.billable_seconds ?? '—' }}</dd></div>
        <div><dt class="text-gray-500">{{ t('usage.mediaSecondsSource') }}</dt><dd>{{ snapshot.media.seconds_source ?? '—' }}</dd></div>
      </dl>
      <p class="text-xs text-gray-500">{{ t('usage.storedSnapshotOnly') }}</p>
    </template>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UsageLog } from '@/types'
import { formatStoredCustomerCredit } from '@/utils/billingCredits'
import { storedCreditSnapshot, storedServedGroupId, storedValue, usageSnapshotConflicts } from '@/utils/usageBillingSnapshot'
import { resolveUsageRequestType } from '@/utils/usageRequestType'
const props = defineProps<{ record: UsageLog }>()
const { t } = useI18n()
const snapshot = computed(() => storedCreditSnapshot(props.record))
const servedGroup = computed(() => storedServedGroupId(props.record))
const conflicts = computed(() => usageSnapshotConflicts(props.record))
const groupId = (id: number | null | undefined) => id != null ? '#' + id : '—'

/**
 * Live 会话行：展示已有字段能说明的时长与计费口径。
 *
 * 注意（develop 分支现状）：Live 会话**尚未计费**——后端 openai_live.go 落库时
 * TotalCost/ActualCost 恒为 0，结算实现要等 PR #5。因此这里只按记录里的实际扣费展示，
 * 也**没有**实时语音时长字段可用（只有会话时长 duration_ms），不做臆造。
 */
const isLive = computed(() => resolveUsageRequestType(props.record) === 'live')

const liveDuration = computed(() => formatLiveDuration(props.record.duration_ms))

const liveBilling = computed(() => {
  const cost = Number(props.record.actual_cost ?? props.record.total_cost ?? 0)
  return cost === 0 ? t('usage.liveNoCharge') : formatStoredCustomerCredit(cost)
})

function formatLiveDuration(ms: number | null | undefined): string {
  if (ms == null || !Number.isFinite(ms)) return '—'
  const totalSeconds = Math.max(0, Math.round(ms / 1000))
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60
  return minutes === 0
    ? t('usage.liveDurationSeconds', { seconds })
    : t('usage.liveDurationMinutes', { minutes, seconds })
}
</script>
