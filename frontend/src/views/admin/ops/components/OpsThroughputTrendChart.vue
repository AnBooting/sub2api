<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Chart as ChartJS, CategoryScale, Filler, Legend, LineElement, LinearScale, PointElement, Title, Tooltip } from 'chart.js'
import { Line } from 'vue-chartjs'
import type { ChartComponentRef } from 'vue-chartjs'
import type { OpsThroughputGroupBreakdownItem, OpsThroughputPlatformBreakdownItem, OpsThroughputTrendPoint } from '@/api/admin/ops'
import type { ChartState } from '../types'
import { formatHistoryLabel, sumNumbers } from '../utils/opsFormatters'
import { withAlpha } from '@/styles/chartPalette'
import { useChartTheme } from '@/features/chart-theme/useChartTheme'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import { formatNumber } from '@/utils/format'

ChartJS.register(Title, Tooltip, Legend, LineElement, LinearScale, PointElement, CategoryScale, Filler)

interface Props {
  points: OpsThroughputTrendPoint[]
  loading: boolean
  timeRange: string
  byPlatform?: OpsThroughputPlatformBreakdownItem[]
  topGroups?: OpsThroughputGroupBreakdownItem[]
  fullscreen?: boolean
}

const props = defineProps<Props>()
const { t } = useI18n()
const { palette } = useChartTheme()
const emit = defineEmits<{
  (e: 'selectPlatform', platform: string): void
  (e: 'selectGroup', groupId: number): void
  (e: 'openDetails'): void
}>()

const throughputChartRef = ref<ChartComponentRef | null>(null)
watch(
  () => props.timeRange,
  () => {
    setTimeout(() => {
      const chart: any = throughputChartRef.value?.chart
      if (chart && typeof chart.resetZoom === 'function') {
        chart.resetZoom()
      }
    }, 100)
  }
)

const totalRequests = computed(() => sumNumbers(props.points.map((p) => p.request_count)))

const chartData = computed(() => {
  if (!props.points.length || totalRequests.value <= 0) return null
  const series = palette.value.series
  return {
    labels: props.points.map((p) => formatHistoryLabel(p.bucket_start, props.timeRange)),
    datasets: [
      {
        label: 'QPS',
        data: props.points.map((p) => p.qps ?? 0),
        borderColor: series[0],
        backgroundColor: withAlpha(series[0], 0.12),
        fill: true,
        tension: 0.4,
        pointRadius: 0,
        pointHitRadius: 10,
        borderWidth: 1.5
      },
      {
        label: t('admin.ops.tpsK'),
        data: props.points.map((p) => (p.tps ?? 0) / 1000),
        borderColor: series[2],
        backgroundColor: withAlpha(series[2], 0.12),
        fill: true,
        tension: 0.4,
        pointRadius: 0,
        pointHitRadius: 10,
        yAxisID: 'y1',
        borderWidth: 1.5
      }
    ]
  }
})

const state = computed<ChartState>(() => {
  if (chartData.value) return 'ready'
  if (props.loading) return 'loading'
  return 'empty'
})

const options = computed(() => {
  const p = palette.value
  return {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { intersect: false, mode: 'index' as const },
    plugins: {
      legend: {
        position: 'top' as const,
        align: 'end' as const,
        labels: { color: p.axis, usePointStyle: true, boxWidth: 6, font: { size: 10 } }
      },
      tooltip: {
        backgroundColor: p.surface,
        titleColor: p.ink,
        bodyColor: p.inkSoft,
        borderColor: p.grid,
        borderWidth: 1,
        padding: 10,
        displayColors: true,
        callbacks: {
          label: (context: any) => {
            let label = context.dataset.label || ''
            if (label) label += ': '
            if (context.raw !== null) label += context.parsed.y.toFixed(1)
            return label
          }
        }
      },
      zoom: {
        pan: { enabled: true, mode: 'x' as const, modifierKey: 'ctrl' as const },
        zoom: { wheel: { enabled: true }, pinch: { enabled: true }, mode: 'x' as const }
      }
    },
    scales: {
      x: {
        type: 'category' as const,
        grid: { display: false },
        ticks: {
          color: p.axis,
          font: { size: 10 },
          maxTicksLimit: 8,
          autoSkip: true,
          autoSkipPadding: 10
        }
      },
      y: {
        type: 'linear' as const,
        display: true,
        position: 'left' as const,
        grid: { color: p.grid, borderDash: [4, 4] },
        ticks: { color: p.axis, font: { size: 10 } }
      },
      y1: {
        type: 'linear' as const,
        display: true,
        position: 'right' as const,
        grid: { display: false },
        ticks: { color: p.series[2], font: { size: 10 } }
      }
    }
  }
})

function resetZoom() {
  const chart: any = throughputChartRef.value?.chart
  if (chart && typeof chart.resetZoom === 'function') chart.resetZoom()
}

function downloadChart() {
  const chart: any = throughputChartRef.value?.chart
  if (!chart || typeof chart.toBase64Image !== 'function') return
  const url = chart.toBase64Image('image/png', 1)
  const a = document.createElement('a')
  a.href = url
  a.download = `ops-throughput-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')}.png`
  a.click()
}
</script>

<template>
  <div class="panel flex h-full min-w-0 flex-col p-4 sm:p-5">
    <div
      data-testid="throughput-chart-header"
      class="mb-4 flex shrink-0 flex-col gap-3 sm:flex-row sm:items-center sm:justify-between"
    >
      <h3 class="flex min-w-0 items-center gap-2 text-sm font-semibold text-ink">
        <svg class="h-4 w-4 shrink-0 text-ink-faint" fill="none" viewBox="0 0 24 24" stroke="currentColor" aria-hidden="true">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M13 7h8m0 0v8m0-8l-8 8-4-4-6 6" />
        </svg>
        <span class="truncate">{{ t('admin.ops.throughputTrend') }}</span>
        <HelpTooltip v-if="!props.fullscreen" :content="t('admin.ops.tooltips.throughputTrend')" />
      </h3>
      <div
        data-testid="throughput-chart-toolbar"
        class="flex w-full min-w-0 flex-wrap items-center gap-2 text-xs text-ink-faint sm:w-auto sm:justify-end"
      >
        <span class="flex shrink-0 items-center gap-1">
          <span class="h-2 w-2 rounded-full" :style="{ backgroundColor: palette.series[0] }"></span>QPS
        </span>
        <span class="flex shrink-0 items-center gap-1">
          <span class="h-2 w-2 rounded-full" :style="{ backgroundColor: palette.series[2] }"></span>{{ t('admin.ops.tpsK') }}
        </span>
        <template v-if="!props.fullscreen">
          <button type="button" class="btn btn-secondary btn-sm shrink-0" :disabled="state !== 'ready'" :title="t('admin.ops.requestDetails.title')" @click="emit('openDetails')">
            {{ t('admin.ops.requestDetails.details') }}
          </button>
          <button type="button" class="btn btn-secondary btn-sm shrink-0" :disabled="state !== 'ready'" :title="t('admin.ops.charts.resetZoomHint')" @click="resetZoom">
            {{ t('admin.ops.charts.resetZoom') }}
          </button>
          <button type="button" class="btn btn-secondary btn-sm shrink-0" :disabled="state !== 'ready'" :title="t('admin.ops.charts.downloadChartHint')" @click="downloadChart">
            {{ t('admin.ops.charts.downloadChart') }}
          </button>
        </template>
      </div>
    </div>

    <!-- Drilldown chips: 点击即设全局过滤 -->
    <div v-if="(props.topGroups?.length ?? 0) > 0" class="mb-3 flex flex-wrap gap-2">
      <button
        v-for="g in props.topGroups"
        :key="g.group_id"
        type="button"
        class="badge badge-gray gap-2 hover:border-rule-strong hover:text-ink"
        @click="emit('selectGroup', g.group_id)"
      >
        <span class="max-w-[180px] truncate">{{ g.group_name || `#${g.group_id}` }}</span>
        <span class="text-ink-faint">{{ formatNumber(g.request_count) }}</span>
      </button>
    </div>

    <div v-else-if="(props.byPlatform?.length ?? 0) > 0" class="mb-3 flex flex-wrap gap-2">
      <button
        v-for="p in props.byPlatform"
        :key="p.platform"
        type="button"
        class="badge badge-gray gap-2 hover:border-rule-strong hover:text-ink"
        @click="emit('selectPlatform', p.platform)"
      >
        <span>{{ p.platform }}</span>
        <span class="text-ink-faint">{{ formatNumber(p.request_count) }}</span>
      </button>
    </div>

    <div class="min-h-0 min-w-0 flex-1">
      <Line v-if="state === 'ready' && chartData" ref="throughputChartRef" :data="chartData" :options="options" />
      <div v-else class="flex h-full items-center justify-center">
        <div v-if="state === 'loading'" class="animate-pulse text-sm text-ink-faint">{{ t('common.loading') }}</div>
        <EmptyState v-else :title="t('common.noData')" :description="t('admin.ops.charts.emptyRequest')" />
      </div>
    </div>
  </div>
</template>
