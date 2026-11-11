<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Chart as ChartJS, ArcElement, Legend, Tooltip } from 'chart.js'
import { Doughnut } from 'vue-chartjs'
import type { OpsErrorDistributionResponse } from '@/api/admin/ops'
import type { ChartState } from '../types'
import { useChartTheme } from '@/features/chart-theme/useChartTheme'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import EmptyState from '@/components/common/EmptyState.vue'

ChartJS.register(ArcElement, Tooltip, Legend)

interface Props {
  data: OpsErrorDistributionResponse | null
  loading: boolean
}

const props = defineProps<Props>()
const emit = defineEmits<{
  (e: 'openDetails'): void
}>()
const { t } = useI18n()
const { palette } = useChartTheme()

const totalSlaErrors = computed(() =>
  (props.data?.items ?? []).reduce((total, item) => total + Number(item.sla || 0), 0)
)

const hasData = computed(() => totalSlaErrors.value > 0)

const state = computed<ChartState>(() => {
  if (hasData.value) return 'ready'
  if (props.loading) return 'loading'
  return 'empty'
})

interface ErrorCategory {
  label: string
  count: number
  color: string
}

/**
 * 错误分类是「测量结果」，不是装饰：上游断线用赭石（需要盯），
 * 客户端错误用苔绿（通路侧），系统错误用氧化红（断线），其余用中性灰。
 * 序列色一律取自 palette，不在组件里写色值。
 */
const categories = computed<ErrorCategory[]>(() => {
  if (!props.data) return []
  const series = palette.value.series

  let upstream = 0 // 502, 503, 504
  let client = 0 // 4xx
  let system = 0 // 500
  let other = 0

  for (const item of props.data.items || []) {
    const code = Number(item.status_code || 0)
    const count = Number(item.sla || 0)
    if (!Number.isFinite(code) || !Number.isFinite(count)) continue

    if ([502, 503, 504].includes(code)) upstream += count
    else if (code >= 400 && code < 500) client += count
    else if (code === 500) system += count
    else other += count
  }

  const out: ErrorCategory[] = []
  if (upstream > 0) out.push({ label: t('admin.ops.upstream'), count: upstream, color: series[1] })
  if (client > 0) out.push({ label: t('admin.ops.client'), count: client, color: series[2] })
  if (system > 0) out.push({ label: t('admin.ops.system'), count: system, color: series[3] })
  if (other > 0) out.push({ label: t('admin.ops.other'), count: other, color: series[5] })
  return out
})

const topReason = computed(() => {
  if (categories.value.length === 0) return null
  return categories.value.reduce((prev, cur) => (cur.count > prev.count ? cur : prev))
})

const chartData = computed(() => {
  if (!hasData.value || categories.value.length === 0) return null
  return {
    labels: categories.value.map((c) => c.label),
    datasets: [
      {
        data: categories.value.map((c) => c.count),
        backgroundColor: categories.value.map((c) => c.color),
        borderColor: palette.value.surface,
        borderWidth: 1
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
            d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"
          />
        </svg>
        {{ t('admin.ops.errorDistribution') }}
        <HelpTooltip :content="t('admin.ops.tooltips.errorDistribution')" />
      </h3>
      <button
        type="button"
        class="btn btn-secondary btn-sm"
        :disabled="state !== 'ready'"
        :title="t('admin.ops.errorTrend')"
        @click="emit('openDetails')"
      >
        {{ t('admin.ops.requestDetails.details') }}
      </button>
    </div>

    <div class="relative min-h-0 flex-1">
      <div v-if="state === 'ready' && chartData" class="flex h-full flex-col">
        <div class="flex-1">
          <Doughnut :data="chartData" :options="{ ...options, cutout: '65%' }" />
        </div>
        <div class="mt-4 flex flex-col items-center gap-2">
          <div v-if="topReason" class="text-xs font-semibold text-ink">
            {{ t('admin.ops.top') }}: <span :style="{ color: topReason.color }">{{ topReason.label }}</span>
          </div>
          <div class="flex flex-wrap justify-center gap-3">
            <div v-for="item in categories" :key="item.label" class="flex items-center gap-1.5 text-xs">
              <span class="h-2 w-2 rounded-full" :style="{ backgroundColor: item.color }"></span>
              <span class="text-ink-soft"><span class="read">{{ item.count }}</span> {{ item.label }}</span>
            </div>
          </div>
        </div>
      </div>

      <div v-else class="flex h-full items-center justify-center">
        <div v-if="state === 'loading'" class="animate-pulse text-sm text-ink-faint">{{ t('common.loading') }}</div>
        <EmptyState v-else :title="t('common.noData')" :description="t('admin.ops.charts.emptyError')" />
      </div>
    </div>
  </div>
</template>
