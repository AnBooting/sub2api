<template>
  <AppLayout>
    <div class="space-y-5">
      <!-- 图签 -->
      <header class="title-block">
        <div class="min-w-0">
          <h1 class="title-block-title">{{ t('admin.dashboard.title') }}</h1>
          <p class="lead mt-1 hidden sm:block">{{ t('admin.dashboard.description') }}</p>
        </div>
        <div class="title-block-meta">
          <span v-if="stats">
            {{ formatNumber(stats.today_requests) }} {{ t('admin.dashboard.todayRequests') }}
          </span>
          <span v-if="stats">
            {{ formatNumber(stats.active_users ?? 0) }} {{ t('admin.dashboard.activeUsers') }}
          </span>
        </div>
      </header>

      <div v-if="loading" class="flex items-center justify-center py-12">
        <LoadingSpinner />
      </div>

      <template v-else-if="stats">
        <div v-if="hasPartialUsage" role="status" data-testid="usage-partial-notice"
          class="border-l-2 border-l-warning-500 border-y border-r border-rule bg-warning-50 px-4 py-3 text-sm text-warning-900 dark:bg-warning-900/20 dark:text-warning-200">
          {{ t('admin.dashboard.partialUsage') }}
        </div>

        <!-- 读数册：核心计数 -->
        <section class="panel">
          <div class="panel-header">
            <h2 class="text-sm font-semibold text-ink">{{ t('admin.dashboard.quickActions') === '' ? '' : '' }}{{ t('admin.dashboard.apiKeys') }} · {{ t('admin.dashboard.accounts') }}</h2>
            <span class="font-read text-[0.6875rem] text-ink-faint">{{ t('common.total') }}</span>
          </div>
          <div class="grid grid-cols-2 divide-rule border-t-0 sm:grid-cols-4 sm:divide-x">
            <div class="border-b border-rule p-3 sm:border-b-0">
              <p class="stat-label">{{ t('admin.dashboard.apiKeys') }}</p>
              <p class="stat-value mt-1">{{ stats.total_api_keys }}</p>
              <p class="stat-trend stat-trend-up">{{ stats.active_api_keys }} {{ t('common.active') }}</p>
            </div>
            <div class="border-b border-rule p-3 sm:border-b-0">
              <p class="stat-label">{{ t('admin.dashboard.accounts') }}</p>
              <p class="stat-value mt-1">{{ stats.total_accounts }}</p>
              <p class="stat-trend">
                <span class="text-success-600 dark:text-success-400">{{ stats.normal_accounts }} {{ t('common.active') }}</span>
                <span v-if="stats.error_accounts > 0" class="ml-1 text-danger-600 dark:text-danger-400">
                  {{ stats.error_accounts }} {{ t('common.error') }}
                </span>
              </p>
            </div>
            <div class="border-b border-rule p-3 sm:border-b-0">
              <p class="stat-label">{{ t('admin.dashboard.todayRequests') }}</p>
              <p class="readout-display mt-1">{{ formatNumber(stats.today_requests) }}</p>
              <p class="stat-trend">{{ t('common.total') }} {{ formatNumber(stats.total_requests) }}</p>
            </div>
            <div class="p-3">
              <p class="stat-label">{{ t('admin.dashboard.users') }}</p>
              <p class="stat-value mt-1">+{{ stats.today_new_users }}</p>
              <p class="stat-trend">{{ t('common.total') }} {{ formatNumber(stats.total_users) }}</p>
            </div>
          </div>
        </section>

        <!-- 读数册：用量与性能 -->
        <section class="panel">
          <div class="panel-header">
            <h2 class="text-sm font-semibold text-ink">{{ t('admin.dashboard.todayTokens') }} · {{ t('admin.dashboard.performance') }}</h2>
            <span class="font-read text-[0.6875rem] text-ink-faint">
              {{ startDate }} → {{ endDate }}
            </span>
          </div>
          <div class="grid grid-cols-1 divide-rule sm:grid-cols-2 sm:divide-x lg:grid-cols-4">
            <div class="border-b border-rule p-3 lg:border-b-0">
              <p class="stat-label">{{ t('admin.dashboard.todayTokens') }}</p>
              <p class="stat-value mt-1">{{ formatTokens(stats.today_tokens) }}</p>
              <p v-if="usageAvailable('today')" class="stat-trend">
                <span class="text-success-600 dark:text-success-400" :title="t('admin.dashboard.actual')">
                  {{ formatCustomerCredit(formatCost(stats.today_actual_cost)) }}
                </span>
                <span class="text-ink-faint">/</span>
                <span class="text-warning-600 dark:text-warning-400" :title="t('admin.dashboard.accountCost')">
                  ${{ formatCost(stats.today_account_cost) }}
                </span>
                <span class="text-ink-faint">/</span>
                <span class="text-ink-faint" :title="t('admin.dashboard.standard')">
                  {{ formatCustomerCredit(formatCost(stats.today_cost)) }}
                </span>
              </p>
              <p v-else class="stat-trend text-warning-700 dark:text-warning-300">{{ t('admin.dashboard.unavailable') }}</p>
            </div>
            <div class="border-b border-rule p-3 lg:border-b-0">
              <p class="stat-label">{{ t('admin.dashboard.totalTokens') }}</p>
              <p data-testid="total-tokens" class="stat-value mt-1">{{ formatTokens(stats.total_tokens) }}</p>
              <p v-if="usageAvailable('total')" class="stat-trend">
                <span class="text-success-600 dark:text-success-400" :title="t('admin.dashboard.actual')">
                  {{ formatCustomerCredit(formatCost(stats.total_actual_cost)) }}
                </span>
                <span class="text-ink-faint">/</span>
                <span class="text-warning-600 dark:text-warning-400" :title="t('admin.dashboard.accountCost')">
                  ${{ formatCost(stats.total_account_cost) }}
                </span>
                <span class="text-ink-faint">/</span>
                <span class="text-ink-faint" :title="t('admin.dashboard.standard')">
                  {{ formatCustomerCredit(formatCost(stats.total_cost)) }}
                </span>
              </p>
              <p v-else class="stat-trend text-warning-700 dark:text-warning-300">{{ t('admin.dashboard.unavailable') }}</p>
            </div>
            <div class="border-b border-rule p-3 lg:border-b-0">
              <p class="stat-label">{{ t('admin.dashboard.performance') }}</p>
              <p class="stat-value mt-1">{{ formatTokens(stats.rpm) }}<span class="ml-1 font-read text-[11px] text-ink-faint">RPM</span></p>
              <p class="stat-trend">{{ formatTokens(stats.tpm) }} TPM</p>
            </div>
            <div class="p-3">
              <p class="stat-label">{{ t('admin.dashboard.avgResponse') }}</p>
              <p class="stat-value mt-1">{{ formatDuration(stats.average_duration_ms) }}</p>
              <p class="stat-trend">
                {{ stats.active_users ?? t('admin.dashboard.unavailable') }} {{ t('admin.dashboard.activeUsers') }}
              </p>
            </div>
          </div>
        </section>

        <!-- 快捷入口：文字按钮，不是彩色卡片 -->
        <section class="panel">
          <div class="panel-header">
            <h2 class="text-sm font-semibold text-ink">{{ t('admin.dashboard.quickActions') }}</h2>
          </div>
          <div class="flex flex-wrap gap-2 px-4 py-3 sm:px-5">
            <button v-if="canUseBatchImage" type="button" class="btn btn-secondary" @click="router.push('/batch-image')">
              <Icon name="sparkles" size="sm" />
              {{ t('admin.dashboard.batchImage') }}
            </button>
            <button type="button" class="btn btn-secondary" @click="router.push('/admin/groups')">
              <Icon name="grid" size="sm" />
              {{ t('admin.dashboard.groupPricing') }}
            </button>
          </div>
        </section>

        <!-- 图表：外框 1px 痕量线，无投影 -->
        <section class="panel">
          <div class="panel-header">
            <h2 class="text-sm font-semibold text-ink">{{ t('admin.dashboard.timeRange') }}</h2>
            <div class="flex flex-wrap items-center gap-2">
              <DateRangePicker
                v-model:start-date="startDate"
                v-model:end-date="endDate"
                @change="onDateRangeChange"
              />
              <button @click="loadDashboardStats" :disabled="chartsLoading" class="btn btn-secondary btn-sm">
                {{ t('common.refresh') }}
              </button>
              <div class="w-28">
                <Select v-model="granularity" :options="granularityOptions" @change="loadChartData" />
              </div>
            </div>
          </div>

          <div v-if="chartsUnavailable" data-testid="charts-unavailable" role="status"
            class="px-4 py-3 text-sm text-warning-700 dark:text-warning-300">
            {{ t('admin.dashboard.chartsUnavailable') }}
          </div>
          <div v-else class="grid grid-cols-1 gap-4 border-t border-rule p-4 sm:p-5 lg:grid-cols-2">
            <ModelDistributionChart
              :model-stats="modelStats"
              :enable-ranking-view="true"
              :ranking-items="rankingItems"
              :ranking-total-actual-cost="rankingTotalActualCost"
              :ranking-total-requests="rankingTotalRequests"
              :ranking-total-tokens="rankingTotalTokens"
              :loading="chartsLoading"
              :ranking-loading="rankingLoading"
              :ranking-error="rankingError"
              :start-date="startDate"
              :end-date="endDate"
              @ranking-click="goToUserUsage"
            />
            <TokenUsageTrend :trend-data="trendData" :loading="chartsLoading" />
          </div>
        </section>

        <!-- 用户用量趋势 -->
        <section class="panel">
          <div class="panel-header">
            <h2 class="text-sm font-semibold text-ink">
              {{ t('admin.dashboard.recentUsage') }}<span class="ml-1 font-read text-[11px] text-ink-faint">Top 12</span>
            </h2>
            <div class="flex gap-1" role="group" :aria-label="t('admin.dashboard.recentUsage')">
              <button v-for="metric in (['tokens', 'actual_cost'] as const)" :key="metric" type="button"
                class="btn btn-sm"
                :class="userTrendMetric === metric ? 'btn-secondary text-ink' : 'btn-ghost'"
                :aria-pressed="userTrendMetric === metric" @click="setUserTrendMetric(metric)">
                {{ t(metric === 'tokens' ? 'admin.dashboard.tokens' : 'admin.dashboard.actualSpending') }}
              </button>
            </div>
          </div>
          <div class="h-64 border-t border-rule p-4">
            <div v-if="userTrendLoading" class="flex h-full items-center justify-center">
              <LoadingSpinner size="md" />
            </div>
            <div v-else-if="chartsUnavailable" class="flex h-full items-center justify-center text-sm text-warning-700 dark:text-warning-300">
              {{ t('admin.dashboard.chartsUnavailable') }}
            </div>
            <Line v-else-if="userTrendChartData" :data="userTrendChartData" :options="lineOptions" />
            <div v-else class="flex h-full items-center justify-center text-sm text-ink-soft">
              {{ t('admin.dashboard.noDataAvailable') }}
            </div>
          </div>
        </section>
      </template>
    </div>
  </AppLayout>
</template>


<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { formatCustomerCredit } from '@/utils/billingCredits'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/stores/app'

const { t } = useI18n()
import { adminAPI } from '@/api/admin'
import type {
  DashboardStats,
  TrendDataPoint,
  ModelStat,
  UserUsageTrendPoint,
  UserSpendingRankingItem
} from '@/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import Select from '@/components/common/Select.vue'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import { useBatchImageAccess } from '@/composables/useBatchImageAccess'
import { withAlpha } from '@/styles/chartPalette'
import { useChartTheme } from '@/features/chart-theme/useChartTheme'

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

// Register Chart.js components
ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
  Filler
)

const appStore = useAppStore()
const router = useRouter()
const { canUseBatchImage, refreshBatchImageAccess } = useBatchImageAccess()
const stats = ref<DashboardStats | null>(null)
const loading = ref(false)
const chartsLoading = ref(false)
const chartsUnavailable = ref(false)
const hasPartialUsage = computed(() => chartsUnavailable.value || Object.values(stats.value?.usage_availability ?? {}).some(available => !available))
const usageAvailable = (section: 'total' | 'today') => stats.value?.usage_availability?.[section] !== false
const userTrendLoading = ref(false)
const rankingLoading = ref(false)
const rankingError = ref(false)

// Chart data
const trendData = ref<TrendDataPoint[]>([])
const modelStats = ref<ModelStat[]>([])
const userTrend = ref<UserUsageTrendPoint[]>([])
const userTrendMetric = ref<'tokens' | 'actual_cost'>('tokens')
const rankingItems = ref<UserSpendingRankingItem[]>([])
const rankingTotalActualCost = ref(0)
const rankingTotalRequests = ref(0)
const rankingTotalTokens = ref(0)
let chartLoadSeq = 0
let usersTrendLoadSeq = 0
let rankingLoadSeq = 0
const rankingLimit = 12

// Helper function to format date in local timezone
const formatLocalDate = (date: Date): string => {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

const getLast24HoursRangeDates = (): { start: string; end: string } => {
  const end = new Date()
  const start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
  return {
    start: formatLocalDate(start),
    end: formatLocalDate(end)
  }
}

// Date range
const granularity = ref<'day' | 'hour'>('hour')
const defaultRange = getLast24HoursRangeDates()
const startDate = ref(defaultRange.start)
const endDate = ref(defaultRange.end)

// Granularity options for Select component
const granularityOptions = computed(() => [
  { value: 'day', label: t('admin.dashboard.day') },
  { value: 'hour', label: t('admin.dashboard.hour') }
])

// 图表配色：序列与网格/轴色一律来自 chartPalette，主题切换时自动重取
const { palette } = useChartTheme()

// Line chart options (for user trend chart)
const lineOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: {
    intersect: false,
    mode: 'index' as const
  },
  plugins: {
    legend: {
      position: 'top' as const,
      labels: {
        color: palette.value.axis,
        usePointStyle: true,
        pointStyle: 'circle',
        padding: 15,
        font: {
          size: 11
        }
      }
    },
    tooltip: {
      itemSort: (a: any, b: any) => {
        const aValue = typeof a?.raw === 'number' ? a.raw : Number(a?.parsed?.y ?? 0)
        const bValue = typeof b?.raw === 'number' ? b.raw : Number(b?.parsed?.y ?? 0)
        return bValue - aValue
      },
      callbacks: {
        label: (context: any) => {
          return `${context.dataset.label}: ${formatUserTrendValue(Number(context.raw))}`
        }
      }
    }
  },
  scales: {
    x: {
      grid: {
        color: palette.value.grid
      },
      ticks: {
        color: palette.value.axis,
        font: {
          size: 10
        }
      }
    },
    y: {
      grid: {
        color: palette.value.grid
      },
      ticks: {
        color: palette.value.axis,
        font: {
          size: 10
        },
        callback: (value: string | number) => formatUserTrendValue(Number(value))
      }
    }
  }
}))

// User trend chart data
const userTrendChartData = computed(() => {
  if (!userTrend.value?.length) return null

  const getDisplayName = (point: UserUsageTrendPoint): string => {
    const username = point.username?.trim()
    if (username) {
      return username
    }

    const email = point.email?.trim()
    if (email) {
      return email
    }

    return t('admin.redeem.userPrefix', { id: point.user_id })
  }

  // Group by user_id to avoid merging different users with the same display name
  const userGroups = new Map<number, { name: string; data: Map<string, number> }>()
  const allDates = new Set<string>()

  userTrend.value.forEach((point) => {
    allDates.add(point.date)
    const key = point.user_id
    if (!userGroups.has(key)) {
      userGroups.set(key, { name: getDisplayName(point), data: new Map() })
    }
    userGroups.get(key)!.data.set(point.date, userTrendMetric.value === 'tokens' ? point.tokens : point.actual_cost)
  })

  const sortedDates = Array.from(allDates).sort()
  // 序列色只有 palette 一个来源；超过 6 条时按顺序循环，保证第 1 条永远是信号色
  const colors = palette.value.series

  const datasets = Array.from(userGroups.values()).map((group, idx) => {
    const color = colors[idx % colors.length]
    return {
      label: group.name,
      data: sortedDates.map((date) => group.data.get(date) || 0),
      borderColor: color,
      backgroundColor: withAlpha(color, 0.12),
      borderWidth: 1.5,
      fill: false,
      tension: 0.3
    }
  })

  return {
    labels: sortedDates,
    datasets
  }
})

// Format helpers
const formatUserTrendValue = (value: number): string =>
  userTrendMetric.value === 'tokens' ? formatTokens(value) : `$${formatCost(value)}`

const formatTokens = (value: number | null | undefined): string => {
  if (value === null) return t('admin.dashboard.unavailable')
  if (value === undefined) return '0'
  if (value >= 1_000_000_000) {
    return `${(value / 1_000_000_000).toFixed(2)}B`
  } else if (value >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(2)}M`
  } else if (value >= 1_000) {
    return `${(value / 1_000).toFixed(2)}K`
  }
  return value.toLocaleString()
}

const toFiniteNumber = (value: unknown): number => {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) ? numberValue : 0
}

const formatNumber = (value: number | null | undefined): string => {
  if (value === null) return t('admin.dashboard.unavailable')
  return toFiniteNumber(value).toLocaleString()
}

const formatCost = (value: number | null | undefined): string => {
  const safeValue = toFiniteNumber(value)
  if (safeValue >= 1000) {
    return (safeValue / 1000).toFixed(2) + 'K'
  } else if (safeValue >= 1) {
    return safeValue.toFixed(2)
  } else if (safeValue >= 0.01) {
    return safeValue.toFixed(3)
  }
  return safeValue.toFixed(4)
}

const formatDuration = (ms: number | null): string => {
  if (ms === null) return t('admin.dashboard.unavailable')
  if (ms >= 1000) {
    return `${(ms / 1000).toFixed(2)}s`
  }
  return `${Math.round(ms)}ms`
}

const goToUserUsage = (item: UserSpendingRankingItem) => {
  void router.push({
    path: '/admin/usage',
    query: {
      user_id: String(item.user_id),
      start_date: startDate.value,
      end_date: endDate.value
    }
  })
}

// Date range change handler
const onDateRangeChange = (range: {
  startDate: string
  endDate: string
  preset: string | null
}) => {
  // Auto-select granularity based on date range
  const start = new Date(range.startDate)
  const end = new Date(range.endDate)
  const daysDiff = Math.ceil((end.getTime() - start.getTime()) / (1000 * 60 * 60 * 24))

  // If range is 1 day, use hourly granularity
  if (daysDiff <= 1) {
    granularity.value = 'hour'
  } else {
    granularity.value = 'day'
  }

  loadChartData()
}

// Load data
const loadDashboardSnapshot = async (includeStats: boolean) => {
  const currentSeq = ++chartLoadSeq
  if (includeStats && !stats.value) {
    loading.value = true
  }
  chartsLoading.value = true
  try {
    const response = await adminAPI.dashboard.getSnapshotV2({
      start_date: startDate.value,
      end_date: endDate.value,
      granularity: granularity.value,
      include_stats: includeStats,
      include_trend: true,
      include_model_stats: true,
      include_group_stats: false,
      include_users_trend: false
    })
    if (currentSeq !== chartLoadSeq) return
    if (includeStats && response.stats) {
      stats.value = response.stats
    }
    chartsUnavailable.value = (response.unavailable_sections?.length ?? 0) > 0
    trendData.value = response.trend || []
    modelStats.value = response.models || []
  } catch (error) {
    if (currentSeq !== chartLoadSeq) return
    chartsUnavailable.value = true
    appStore.showError(t('admin.dashboard.failedToLoad'))
    console.error('Error loading dashboard snapshot:', error)
  } finally {
    if (currentSeq === chartLoadSeq) {
      loading.value = false
      chartsLoading.value = false
    }
  }
}

const setUserTrendMetric = (metric: 'tokens' | 'actual_cost') => {
  if (userTrendMetric.value === metric) return
  userTrendMetric.value = metric
  userTrend.value = []
  loadUsersTrend()
}

const loadUsersTrend = async () => {
  if (chartsUnavailable.value) { userTrend.value = []; return }
  const currentSeq = ++usersTrendLoadSeq
  userTrendLoading.value = true
  try {
    const response = await adminAPI.dashboard.getUserUsageTrend({
      start_date: startDate.value,
      end_date: endDate.value,
      granularity: granularity.value,
      limit: 12,
      metric: userTrendMetric.value
    })
    if (currentSeq !== usersTrendLoadSeq) return
    userTrend.value = response.trend || []
  } catch (error) {
    if (currentSeq !== usersTrendLoadSeq) return
    console.error('Error loading users trend:', error)
    userTrend.value = []
  } finally {
    if (currentSeq === usersTrendLoadSeq) {
      userTrendLoading.value = false
    }
  }
}

const loadUserSpendingRanking = async () => {
  if (chartsUnavailable.value) { rankingItems.value = []; return }
  const currentSeq = ++rankingLoadSeq
  rankingLoading.value = true
  rankingError.value = false
  try {
    const response = await adminAPI.dashboard.getUserSpendingRanking({
      start_date: startDate.value,
      end_date: endDate.value,
      limit: rankingLimit
    })
    if (currentSeq !== rankingLoadSeq) return
    rankingItems.value = response.ranking || []
    rankingTotalActualCost.value = response.total_actual_cost || 0
    rankingTotalRequests.value = response.total_requests || 0
    rankingTotalTokens.value = response.total_tokens || 0
  } catch (error) {
    if (currentSeq !== rankingLoadSeq) return
    console.error('Error loading user spending ranking:', error)
    rankingItems.value = []
    rankingTotalActualCost.value = 0
    rankingTotalRequests.value = 0
    rankingTotalTokens.value = 0
    rankingError.value = true
  } finally {
    if (currentSeq === rankingLoadSeq) {
      rankingLoading.value = false
    }
  }
}

const loadDashboardStats = async () => {
  await loadDashboardSnapshot(true)
  await Promise.all([loadUsersTrend(), loadUserSpendingRanking()])
}

const loadChartData = async () => {
  await loadDashboardSnapshot(false)
  await Promise.all([loadUsersTrend(), loadUserSpendingRanking()])
}

onMounted(() => {
  void refreshBatchImageAccess()
  loadDashboardStats()
})
</script>

<style scoped>
</style>
