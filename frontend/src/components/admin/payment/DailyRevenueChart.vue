<template>
  <div class="card p-4">
    <h3 class="mb-4 text-sm font-semibold text-ink">
      {{ t('payment.admin.dailyRevenue') }}
    </h3>
    <div class="h-64">
      <div v-if="loading" class="flex h-full items-center justify-center">
        <LoadingSpinner size="md" />
      </div>
      <Line v-else-if="chartData" :data="chartData" :options="chartOptions" />
      <div
        v-else
        class="flex h-full items-center justify-center text-sm text-ink-soft"
      >
        {{ t('payment.admin.noData') }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
  Filler
} from 'chart.js'
import { Line } from 'vue-chartjs'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { withAlpha } from '@/styles/chartPalette'
import { useChartTheme } from '@/features/chart-theme/useChartTheme'
import type { DailyPaymentStats } from '@/types/payment'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, Filler)

const { t } = useI18n()
const { palette } = useChartTheme()

const props = defineProps<{
  data: DailyPaymentStats[]
  loading?: boolean
}>()

const chartData = computed(() => {
  if (!props.data || props.data.length === 0) return null
  const series = palette.value.series
  const currencies = [...new Set(props.data.flatMap(day => Object.keys(day.amount)))].sort()
  return {
    labels: props.data.map(d => d.date),
    datasets: [
      ...currencies.map((currency, index) => {
        const color = series[index % series.length]
        return {
          label: `${currency} ${t('payment.admin.revenue')}`,
          data: props.data.map(day => day.amount[currency] || 0),
          borderColor: color,
          backgroundColor: withAlpha(color, 0.1),
          fill: true,
          tension: 0.3,
          pointRadius: 3,
          pointHoverRadius: 5,
          borderWidth: 1.5,
        }
      }),
      {
        label: t('payment.admin.orderCount'),
        data: props.data.map(d => d.count),
        borderColor: series[2],
        backgroundColor: withAlpha(series[2], 0.1),
        fill: false,
        tension: 0.3,
        pointRadius: 3,
        pointHoverRadius: 5,
        borderWidth: 1.5,
        yAxisID: 'y1',
      }
    ]
  }
})

const chartOptions = computed(() => {
  const p = palette.value
  return {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { mode: 'index' as const, intersect: false },
    scales: {
      x: {
        grid: { display: false },
        ticks: { color: p.axis, font: { size: 10 } }
      },
      y: {
        type: 'linear' as const,
        display: true,
        position: 'left' as const,
        grid: { color: p.grid, borderDash: [4, 4] },
        ticks: { color: p.axis, font: { size: 10 } },
        title: { display: true, text: t('payment.admin.revenue'), color: p.axis, font: { size: 10 } },
      },
      y1: {
        type: 'linear' as const,
        display: true,
        position: 'right' as const,
        ticks: { color: p.axis, font: { size: 10 } },
        title: { display: true, text: t('payment.admin.orderCount'), color: p.axis, font: { size: 10 } },
        grid: { drawOnChartArea: false },
      }
    },
    plugins: {
      legend: {
        position: 'top' as const,
        labels: { color: p.axis, usePointStyle: true, boxWidth: 6, font: { size: 10 } }
      },
      tooltip: {
        backgroundColor: p.surface,
        titleColor: p.ink,
        bodyColor: p.inkSoft,
        borderColor: p.grid,
        borderWidth: 1,
        padding: 10
      },
    }
  }
})
</script>
