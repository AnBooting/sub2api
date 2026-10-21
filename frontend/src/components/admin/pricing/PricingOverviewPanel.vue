<template>
  <div class="flex h-full min-h-0 flex-col gap-4">
    <!-- 说明 -->
    <div class="flex flex-col gap-1">
      <h3 class="text-base font-semibold text-gray-900 dark:text-white">
        {{ t('admin.pricing.table.title') }}
      </h3>
      <p class="text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.pricing.table.description') }}
      </p>
      <p class="text-xs text-amber-600 dark:text-amber-400">
        {{ t('admin.pricing.table.limitNotice') }}
      </p>
      <p class="text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.pricing.table.cardNotice') }}
      </p>
    </div>

    <!-- 过滤 + 检测 -->
    <div class="flex flex-wrap items-center gap-3">
      <input
        v-model="modelFilter"
        type="text"
        class="input w-56"
        data-testid="filter-model"
        :placeholder="t('admin.pricing.table.filterModel')"
      />
      <select v-model="currencyFilter" class="input w-36" data-testid="filter-currency">
        <option value="">{{ t('admin.pricing.table.allCurrencies') }}</option>
        <option v-for="currency in currencies" :key="currency" :value="currency">
          {{ currency }}
        </option>
      </select>
      <button
        v-if="!probeRunning"
        type="button"
        class="btn btn-secondary"
        data-testid="detect"
        @click="startDetection"
      >
        {{ t('admin.pricing.table.detect') }}
      </button>
      <button
        v-else
        type="button"
        class="btn btn-secondary"
        data-testid="stop-detect"
        @click="stopDetection"
      >
        {{ t('admin.pricing.table.stopDetect') }}
      </button>
      <span
        v-if="probe.state.status !== 'idle'"
        class="text-xs text-gray-500 dark:text-gray-400"
        data-testid="probe-progress"
      >
        {{ t('admin.pricing.table.progress', { done: probe.state.completed, total: probe.state.total }) }}
        {{ t('admin.pricing.table.percent', { percent: probePercent }) }}
      </span>
    </div>

    <!-- 表格 -->
    <div class="min-h-0 flex-1 overflow-auto">
      <p v-if="loading" class="p-4 text-sm text-gray-500">
        {{ t('admin.pricing.table.loading') }}
      </p>
      <p v-else-if="loadFailed" class="p-4 text-sm text-red-500">
        {{ t('admin.pricing.table.loadFailed') }}
      </p>
      <EmptyState v-else-if="rows.length === 0" :title="t('admin.pricing.table.empty')" />
      <table v-else class="w-full text-sm" data-testid="price-table">
        <thead>
          <tr>
            <th class="px-3 py-2 text-left font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.pricing.table.columns.model') }}
            </th>
            <th class="px-3 py-2 text-left font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.pricing.table.columns.currency') }}
            </th>
            <th class="px-3 py-2 text-left font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.pricing.table.columns.unit') }}
            </th>
            <th class="px-3 py-2 text-left font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.pricing.table.columns.unitPrice') }}
            </th>
            <th class="px-3 py-2 text-left font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.pricing.table.columns.inputPrice') }}
            </th>
            <th class="px-3 py-2 text-left font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.pricing.table.columns.outputPrice') }}
            </th>
            <th class="px-3 py-2 text-left font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.pricing.table.columns.cacheReadPrice') }}
            </th>
            <th class="px-3 py-2 text-left font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.pricing.table.columns.cacheWritePrice') }}
            </th>
            <th class="px-3 py-2 text-left font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.pricing.table.columns.imagePrice') }}
            </th>
            <th class="px-3 py-2 text-left font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.pricing.table.columns.card') }}
            </th>
            <th class="px-3 py-2 text-left font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.pricing.table.columns.source') }}
            </th>
            <th class="px-3 py-2 text-left font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.pricing.table.columns.scope') }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(row, index) in pagedRows"
            :key="rowKey(row, index)"
            class="border-b border-gray-100 dark:border-dark-800"
            data-testid="price-row"
          >
            <td class="px-3 py-2 font-medium text-gray-900 dark:text-white">{{ row.model }}</td>
            <td class="px-3 py-2 text-gray-700 dark:text-gray-300">{{ row.currency }}</td>
            <td class="px-3 py-2 text-gray-700 dark:text-gray-300">{{ unitLabel(row.unit) }}</td>
            <td class="px-3 py-2 text-gray-700 dark:text-gray-300">
              {{ formatPrice(row.perRequestPrice) }}
            </td>
            <td class="px-3 py-2 text-gray-700 dark:text-gray-300">{{ formatPrice(row.inputPrice) }}</td>
            <td class="px-3 py-2 text-gray-700 dark:text-gray-300">{{ formatPrice(row.outputPrice) }}</td>
            <td class="px-3 py-2 text-gray-700 dark:text-gray-300">
              {{ formatPrice(row.cacheReadPrice) }}
            </td>
            <td class="px-3 py-2 text-gray-700 dark:text-gray-300">
              {{ formatPrice(row.cacheWritePrice) }}
            </td>
            <td class="px-3 py-2 text-gray-700 dark:text-gray-300">{{ formatPrice(row.imagePrice) }}</td>
            <td class="px-3 py-2 text-gray-700 dark:text-gray-300">{{ cardLabel(row.model) }}</td>
            <td class="px-3 py-2 text-gray-700 dark:text-gray-300">{{ sourceLabel(row.source) }}</td>
            <td class="px-3 py-2 text-gray-700 dark:text-gray-300">{{ scopeLabel(row) }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 分页 -->
    <div class="flex flex-wrap items-center justify-between gap-3 text-xs text-gray-600 dark:text-gray-400">
      <span data-testid="total-count">
        {{ t('admin.pricing.table.totalCount', { count: filteredRows.length }) }}
      </span>
      <span data-testid="row-range">
        {{ t('admin.pricing.table.range', { from: rangeFrom, to: rangeTo, count: filteredRows.length }) }}
      </span>
      <label class="flex items-center gap-2">
        {{ t('admin.pricing.table.pageSize') }}
        <select v-model.number="pageSize" class="input w-24" data-testid="page-size">
          <option v-for="size in pageSizeOptions" :key="size" :value="size">{{ size }}</option>
        </select>
      </label>
      <div class="flex items-center gap-2">
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          data-testid="prev-page"
          :disabled="page <= 1"
          @click="page = page - 1"
        >
          {{ t('admin.pricing.table.prevPage') }}
        </button>
        <span data-testid="page-info">
          {{ t('admin.pricing.table.pageInfo', { page, pages: totalPages }) }}
        </span>
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          data-testid="next-page"
          :disabled="page >= totalPages"
          @click="page = page + 1"
        >
          {{ t('admin.pricing.table.nextPage') }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import EmptyState from '@/components/common/EmptyState.vue'
import type { BillingMode } from '@/constants/channel'
import { buildPriceTableRows, cardStatusOf } from './pricingAudit'
import type { PriceSourceKind, PriceTableRow } from './pricingAudit'
import { createPriceCardProbe } from './priceCardProbe'
import { usePricingRuleData } from './usePricingRuleData'

const { t } = useI18n()

const { data, loading, loadFailed, load } = usePricingRuleData()
const probe = createPriceCardProbe()

const modelFilter = ref('')
const currencyFilter = ref('')
const page = ref(1)
const pageSize = ref(50)
const pageSizeOptions = [20, 50, 100]

const probeResults = computed(() => ({
  results: probe.state.results,
  failures: probe.state.failures
}))

const rows = computed<PriceTableRow[]>(() => buildPriceTableRows(data.value, probeResults.value))

const currencies = computed(() => {
  const set = new Set<string>()
  for (const row of rows.value) set.add(row.currency)
  return [...set].sort()
})

const filteredRows = computed(() => {
  const keyword = modelFilter.value.trim().toLowerCase()
  return rows.value.filter(row => {
    if (keyword && !row.model.toLowerCase().includes(keyword)) return false
    if (currencyFilter.value && row.currency !== currencyFilter.value) return false
    return true
  })
})

const totalPages = computed(() => Math.max(1, Math.ceil(filteredRows.value.length / pageSize.value)))

const pagedRows = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredRows.value.slice(start, start + pageSize.value)
})

const rangeFrom = computed(() =>
  filteredRows.value.length === 0 ? 0 : (page.value - 1) * pageSize.value + 1
)
const rangeTo = computed(() => Math.min(page.value * pageSize.value, filteredRows.value.length))

const probeRunning = computed(() => probe.state.status === 'running')
const probePercent = computed(() => {
  if (probe.state.total === 0) return 0
  return Math.round((probe.state.completed / probe.state.total) * 100)
})

watch(filteredRows, () => {
  page.value = 1
})

function rowKey(row: PriceTableRow, index: number): string {
  return `${row.model}|${row.scope}|${row.scopeId}|${index}`
}

function formatPrice(value: number | null): string {
  if (value == null) return '—'
  return String(Math.round(value * 1e6) / 1e6)
}

function unitLabel(mode: BillingMode): string {
  if (mode === 'token') return t('admin.pricing.units.token')
  if (mode === 'per_request') return t('admin.pricing.units.perRequest')
  if (mode === 'image') return t('admin.pricing.units.image')
  return t('admin.pricing.units.video')
}

function sourceLabel(source: PriceSourceKind): string {
  if (source === 'manual') return t('admin.pricing.sources.manual')
  if (source === 'default') return t('admin.pricing.sources.default')
  return t('admin.pricing.sources.card')
}

function scopeLabel(row: PriceTableRow): string {
  if (row.scope === 'channel') {
    return t('admin.pricing.table.scopeChannel', { name: row.scopeName })
  }
  return t('admin.pricing.table.scopeGroup', { name: row.scopeName })
}

function cardLabel(model: string): string {
  const status = cardStatusOf(model, probeResults.value)
  if (status === 'usable') return t('admin.pricing.table.cardUsable')
  if (status === 'unusable') return t('admin.pricing.table.cardUnusable')
  if (status === 'failed') return t('admin.pricing.table.cardFailed')
  return t('admin.pricing.table.cardNotProbed')
}

function startDetection() {
  const models = data.value.models
  void probe.start(models)
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
