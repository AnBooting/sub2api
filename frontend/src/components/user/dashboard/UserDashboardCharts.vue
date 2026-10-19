<template>
  <div class="space-y-4">
    <!-- Date range + granularity toolbar: single compact row -->
    <div class="card flex flex-wrap items-center gap-3 px-4 py-3">
      <span class="text-sm font-medium text-ink-700 dark:text-ink-200">{{ t('dashboard.timeRange') }}</span>
      <DateRangePicker
        :start-date="startDate"
        :end-date="endDate"
        @update:startDate="$emit('update:startDate', $event)"
        @update:endDate="$emit('update:endDate', $event)"
        @change="$emit('dateRangeChange', $event)"
      />
      <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="$emit('refresh')">
        {{ t('common.refresh') }}
      </button>
      <div class="ml-auto flex items-center gap-2">
        <span class="text-sm font-medium text-ink-700 dark:text-ink-200">{{ t('dashboard.granularity') }}</span>
        <div class="w-28">
          <Select
            :model-value="granularity"
            :options="[{ value: 'day', label: t('dashboard.day') }, { value: 'hour', label: t('dashboard.hour') }]"
            @update:model-value="$emit('update:granularity', $event)"
            @change="$emit('granularityChange')"
          />
        </div>
      </div>
    </div>

    <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
      <!-- Model distribution -->
      <div class="card p-4">
        <div class="section-title">
          <h3 class="section-title-text">{{ t('dashboard.modelDistribution') }}</h3>
        </div>

        <!-- Loading: skeleton instead of a spinner overlay -->
        <div v-if="loading" class="flex flex-col items-center gap-4 sm:flex-row sm:gap-6" aria-busy="true" aria-live="polite">
          <div class="skeleton-block h-48 w-48 shrink-0 !rounded-full"></div>
          <div class="w-full min-w-0 flex-1 space-y-2">
            <div v-for="i in 6" :key="i" class="flex items-center gap-3">
              <div class="skeleton-text flex-1"></div>
              <div class="skeleton-text w-10"></div>
              <div class="skeleton-text w-12"></div>
            </div>
          </div>
        </div>

        <div v-else-if="!modelData" class="state-panel py-10">
          <div class="state-panel-icon">
            <Icon name="chart" size="lg" />
          </div>
          <p class="state-panel-title">{{ t('dashboard.noDataAvailable') }}</p>
          <p class="state-panel-desc">{{ t('dashboard.startUsingApi') }}</p>
        </div>

        <div v-else class="flex flex-col items-center gap-4 sm:flex-row sm:gap-6">
          <div class="h-48 w-48 shrink-0">
            <Doughnut :data="modelData" :options="doughnutOptions" />
          </div>
          <div class="max-h-48 w-full min-w-0 flex-1 overflow-auto">
            <table class="table table-dense">
              <thead>
                <tr>
                  <th class="text-left">{{ t('dashboard.model') }}</th>
                  <th class="num">{{ t('dashboard.requests') }}</th>
                  <th class="num">{{ t('dashboard.tokens') }}</th>
                  <th class="num">{{ t('dashboard.actual') }}</th>
                  <th class="num">{{ t('dashboard.standard') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="model in models" :key="model.model">
                  <td class="max-w-[120px] truncate font-medium" :title="model.model">{{ model.model }}</td>
                  <td class="num">{{ formatNumber(model.requests) }}</td>
                  <td class="num">{{ formatTokens(model.total_tokens) }}</td>
                  <td class="num font-medium text-ink-900 dark:text-white">{{ formatCustomerCredit(formatCost(model.actual_cost)) }}</td>
                  <td class="num text-ink-400 dark:text-ink-300">{{ formatCustomerCredit(formatCost(model.cost)) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>

      <TokenUsageTrend :trend-data="trend" :loading="loading" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { formatCustomerCredit } from '@/utils/billingCredits'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { Doughnut } from 'vue-chartjs'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import type { TrendDataPoint, ModelStat } from '@/types'
import { formatCostFixed as formatCost, formatNumberLocaleString as formatNumber, formatTokensK as formatTokens } from '@/utils/format'
import { Chart as ChartJS, CategoryScale, LinearScale, PointElement, LineElement, ArcElement, Title, Tooltip, Legend, Filler } from 'chart.js'
ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, ArcElement, Title, Tooltip, Legend, Filler)

const props = defineProps<{ loading: boolean, startDate: string, endDate: string, granularity: string, trend: TrendDataPoint[], models: ModelStat[] }>()
defineEmits(['update:startDate', 'update:endDate', 'update:granularity', 'dateRangeChange', 'granularityChange', 'refresh'])
const { t } = useI18n()

// 图表色板收敛到灰蓝体系：主色 + 中性阶，避免同一页出现 8 种高饱和色相。
const CHART_PALETTE = [
  '#0f7fae',
  '#41b6e2',
  '#7f8c98',
  '#aeb8c3',
  '#145976',
  '#7ed0ef',
  '#5a6a75',
  '#c8d0d9',
]

const modelData = computed(() => !props.models?.length ? null : {
  labels: props.models.map((m: ModelStat) => m.model),
  datasets: [{
    data: props.models.map((m: ModelStat) => m.total_tokens),
    backgroundColor: CHART_PALETTE,
    borderWidth: 0,
  }]
})

const doughnutOptions = {
  responsive: true,
  maintainAspectRatio: false,
  cutout: '62%',
  plugins: {
    legend: { display: false },
    tooltip: {
      callbacks: {
        label: (context: any) => `${context.label}: ${formatTokens(context.parsed)} tokens`
      }
    }
  }
}
</script>
