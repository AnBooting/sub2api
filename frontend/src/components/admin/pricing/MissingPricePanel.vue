<template>
  <div class="flex h-full min-h-0 flex-col gap-4">
    <!-- 说明 -->
    <div class="flex flex-col gap-1">
      <h3 class="text-base font-semibold text-gray-900 dark:text-white">
        {{ t('admin.pricing.missing.title') }}
      </h3>
      <p class="text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.pricing.missing.description') }}
      </p>
      <p class="text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.pricing.missing.wildcardNotice') }}
      </p>
      <p class="text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.pricing.missing.priorityNotice') }}
      </p>
      <p class="text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.pricing.missing.probeNotice') }}
      </p>
    </div>

    <!-- 控制条 -->
    <div class="flex flex-wrap items-center gap-3">
      <button
        v-if="!probeRunning"
        type="button"
        class="btn btn-primary"
        data-testid="start-probe"
        @click="startDetection"
      >
        <Icon name="play" size="sm" class="mr-1" />
        {{ t('admin.pricing.missing.start') }}
      </button>
      <button
        v-else
        type="button"
        class="btn btn-secondary"
        data-testid="stop-probe"
        @click="stopDetection"
      >
        {{ t('admin.pricing.table.stopDetect') }}
      </button>
      <span
        v-if="probeStarted"
        class="text-xs text-gray-500 dark:text-gray-400"
        data-testid="probe-progress"
      >
        {{ t('admin.pricing.missing.progress', { done: probe.state.completed, total: probe.state.total }) }}
        {{ t('admin.pricing.missing.percent', { percent: probePercent }) }}
      </span>
      <span
        v-if="probe.state.status === 'stopped'"
        class="text-xs text-amber-600 dark:text-amber-400"
        data-testid="probe-stopped"
      >
        {{ t('admin.pricing.missing.stopped') }}
      </span>
    </div>

    <!-- 结果 -->
    <div class="min-h-0 flex-1 overflow-auto">
      <EmptyState
        v-if="!probeStarted"
        :title="t('admin.pricing.missing.notStarted')"
        :description="t('admin.pricing.missing.notStartedHint')"
        :action-text="t('admin.pricing.missing.start')"
        @action="startDetection"
      />
      <EmptyState
        v-else-if="rows.length === 0"
        :title="t('admin.pricing.missing.noMissing')"
        :description="t('admin.pricing.missing.probeNotice')"
      />
      <table v-else class="w-full text-sm" data-testid="missing-table">
        <thead>
          <tr>
            <th class="px-3 py-2 text-left font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.pricing.missing.columns.model') }}
            </th>
            <th class="px-3 py-2 text-left font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.pricing.missing.columns.scope') }}
            </th>
            <th class="px-3 py-2 text-left font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.pricing.missing.columns.missing') }}
            </th>
            <th class="px-3 py-2 text-left font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.pricing.missing.columns.probe') }}
            </th>
            <th class="px-3 py-2 text-left font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.pricing.missing.columns.action') }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="row in rows"
            :key="rowKey(row)"
            class="border-b border-gray-100 dark:border-dark-800"
            data-testid="missing-row"
          >
            <td class="px-3 py-2 font-medium text-gray-900 dark:text-white">{{ row.model }}</td>
            <td class="px-3 py-2 text-gray-700 dark:text-gray-300">{{ scopeLabel(row) }}</td>
            <td class="px-3 py-2 text-gray-700 dark:text-gray-300">{{ missingLabel(row) }}</td>
            <td class="px-3 py-2 text-gray-700 dark:text-gray-300">
              <span v-if="row.probeFailed" class="text-red-600 dark:text-red-400">
                {{ t('admin.pricing.missing.probeFailed') }}
              </span>
              <span v-else>—</span>
            </td>
            <td class="px-3 py-2">
              <button
                v-if="row.scopeId != null"
                type="button"
                class="btn btn-secondary btn-sm"
                data-testid="edit-pricing"
                @click="goToPricing(row)"
              >
                <Icon name="edit" size="sm" class="mr-1" />
                {{ t('admin.pricing.missing.editPricing') }}
              </button>
              <span v-else class="text-xs text-gray-400">
                {{ t('admin.pricing.missing.groupFallback') }}
              </span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import { resolveMissingPriceRows } from './pricingAudit'
import type { MissingPriceRow, PricingEditTarget } from './pricingAudit'
import { createPriceCardProbe } from './priceCardProbe'
import { usePricingRuleData } from './usePricingRuleData'

const emit = defineEmits<{
  (event: 'edit-pricing', target: PricingEditTarget): void
}>()

const { t } = useI18n()

const { data, load } = usePricingRuleData()
const probe = createPriceCardProbe()

const probeResults = computed(() => ({
  results: probe.state.results,
  failures: probe.state.failures
}))

const rows = computed<MissingPriceRow[]>(() =>
  resolveMissingPriceRows(data.value, probeResults.value)
)

const probeRunning = computed(() => probe.state.status === 'running')
const probeStarted = computed(() => probe.state.status !== 'idle')
const probePercent = computed(() => {
  if (probe.state.total === 0) return 0
  return Math.round((probe.state.completed / probe.state.total) * 100)
})

function rowKey(row: MissingPriceRow): string {
  return `${row.model}|${row.scope}|${row.scopeId ?? 'card'}|${row.kind}`
}

function scopeLabel(row: MissingPriceRow): string {
  if (row.scope === 'channel') {
    return `${t('admin.pricing.missing.scopeChannel', { name: row.scopeName })}${row.platform ? ' · ' + row.platform : ''}`
  }
  if (row.scope === 'group') {
    return `${t('admin.pricing.missing.scopeGroup', { name: row.scopeName })}${row.platform ? ' · ' + row.platform : ''}`
  }
  return t('admin.pricing.missing.scopeCard')
}

/** 缺失字段的展示：规则来源列出具体字段；价卡来源说明整体缺价或快照未就绪。 */
function missingLabel(row: MissingPriceRow): string {
  if (row.kind === 'card_not_found') return t('admin.pricing.missing.allRequired')
  if (row.kind === 'card_not_ready') return t('admin.pricing.missing.cardNotReady')
  if (row.kind === 'probe_failed') return t('admin.pricing.missing.probeFailed')
  return row.missingFields.map(fieldLabel).join('、')
}

/** 字段名 → 文案（t() 使用字面量 key，避免运行时拼 key 缺文案）。 */
function fieldLabel(field: string): string {
  switch (field) {
    case 'input_price': return t('admin.pricing.fields.inputPrice')
    case 'output_price': return t('admin.pricing.fields.outputPrice')
    case 'cache_write_price': return t('admin.pricing.fields.cacheWritePrice')
    case 'cache_read_price': return t('admin.pricing.fields.cacheReadPrice')
    case 'per_request_price': return t('admin.pricing.fields.perRequestPrice')
    default: return field
  }
}

function goToPricing(row: MissingPriceRow) {
  const target: PricingEditTarget = { model: row.model, platform: row.platform }
  if (row.scope === 'channel' && row.scopeId != null) {
    target.channelId = row.scopeId
  } else if (row.scope === 'group' && row.scopeId != null) {
    target.groupId = row.scopeId
  }
  emit('edit-pricing', target)
}

async function startDetection() {
  const models = data.value.models
  await probe.start(models)
}

function stopDetection() {
  probe.stop()
}

onMounted(() => {
  void load()
})

onUnmounted(() => {
  // 组件卸载时中断在途探测请求
  probe.stop()
})
</script>
