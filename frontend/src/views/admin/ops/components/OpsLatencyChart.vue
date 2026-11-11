<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Chart as ChartJS, BarElement, CategoryScale, Legend, LinearScale, Tooltip } from 'chart.js'
import { Bar } from 'vue-chartjs'
import type { OpsLatencyHistogramResponse } from '@/api/admin/ops'
import type { ChartState } from '../types'
import { withAlpha } from '@/styles/chartPalette'
import { useChartTheme } from '@/features/chart-theme/useChartTheme'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import EmptyState from '@/components/common/EmptyState.vue'

ChartJS.register(BarElement, CategoryScale, LinearScale, Tooltip, Legend)

interface Props {
  latencyData: OpsLatencyHistogramResponse | null
  loading: boolean
}

const props = defineProps<Props>()
const { t } = useI18n()
const { palette } = useChartTheme()

const hasData = computed(() => (props.latencyData?.total_requests ?? 0) > 0)

const state = computed<ChartState>(() => {
  if (hasData.value) return 'ready'
  if (props.loading) return 'loading'
  return 'empty'
})

const chartData = computed(() => {
  if (!props.latencyData || !hasData.value) return null
  const signal = palette.value.series[0]
  return {
    labels: props.latencyData.buckets.map((b) => b.range),
    datasets: [
      {
        label: t('admin.ops.requests'),
        data: props.latencyData.buckets.map((b) => b.count),
        backgroundColor: withAlpha(signal, 0.85),
        hoverBackgroundColor: signal,
        borderRadius: 2,
        barPercentage: 0.6
      }
    ]
  }
})

const options = computed(() => {
  const p = palette.value
  return {
    responsive: true,
    maintainAspectRatio: false,
    plugins: {
      legend: { display: false },
      tooltip: {
        backgroundColor: p.surface,
        titleColor: p.ink,
        bodyColor: p.inkSoft,
        borderColor: p.grid,
        borderWidth: 1,
        padding: 10
      }
    },
    scales: {
      x: {
        grid: { display: false },
        ticks: { color: p.axis, font: { size: 10 } }
      },
      y: {
        beginAtZero: true,
        grid: { color: p.grid, borderDash: [4, 4] },
        ticks: { color: p.axis, font: { size: 10 } }
      }
    }
  }
})
</script>

<template>
  <div class="panel flex h-full flex-col p-4 sm:p-5">
    <div class="mb-4 flex items-center justify-between">
      <h3 class="flex items-center gap-2 text-sm font-semibold text-ink">
        <svg class="h-4 w-4 text-ink-faint" fill="none" viewBox="0 0 24 24" stroke="currentColor" aria-hidden="true">
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="1.5"
            d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"
          />
        </svg>
        {{ t('admin.ops.latencyHistogram') }}
        <HelpTooltip :content="t('admin.ops.tooltips.latencyHistogram')" />
      </h3>
    </div>

    <div class="min-h-0 flex-1">
      <Bar v-if="state === 'ready' && chartData" :data="chartData" :options="options" />
      <div v-else class="flex h-full items-center justify-center">
        <div v-if="state === 'loading'" class="animate-pulse text-sm text-ink-faint">{{ t('common.loading') }}</div>
        <EmptyState v-else :title="t('common.noData')" :description="t('admin.ops.charts.emptyRequest')" />
      </div>
    </div>
  </div>
</template>
