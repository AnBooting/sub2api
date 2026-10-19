<template>
  <AppLayout>
    <div class="space-y-5">
      <!-- Loading: skeleton mirrors the real layout instead of a bare spinner -->
      <div v-if="loading" class="space-y-5" aria-busy="true" aria-live="polite">
        <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
          <div v-for="i in 4" :key="i" class="card p-4">
            <div class="flex items-center gap-3">
              <div class="skeleton-block h-9 w-9"></div>
              <div class="flex-1 space-y-2">
                <div class="skeleton-text w-16"></div>
                <div class="skeleton-text h-5 w-24"></div>
                <div class="skeleton-text w-20"></div>
              </div>
            </div>
          </div>
        </div>
        <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
          <div v-for="i in 4" :key="i" class="card p-4">
            <div class="flex items-center gap-3">
              <div class="skeleton-block h-9 w-9"></div>
              <div class="flex-1 space-y-2">
                <div class="skeleton-text w-20"></div>
                <div class="skeleton-text h-5 w-16"></div>
                <div class="skeleton-text w-24"></div>
              </div>
            </div>
          </div>
        </div>
        <div class="skeleton-block h-72 w-full"></div>
      </div>

      <template v-else-if="stats">
        <UserDashboardStats
          :stats="stats"
          :balance="user?.balance || 0"
          :is-simple="authStore.isSimpleMode"
          :platform-quotas="platformQuotas"
        />
        <UserDashboardCharts
          v-model:startDate="startDate"
          v-model:endDate="endDate"
          v-model:granularity="granularity"
          :loading="loadingCharts"
          :trend="trendData"
          :models="modelStats"
          @dateRangeChange="loadCharts"
          @granularityChange="loadCharts"
          @refresh="refreshAll"
        />
        <div class="grid grid-cols-1 gap-5 lg:grid-cols-3">
          <div class="lg:col-span-2">
            <UserDashboardRecentUsage :data="recentUsage" :loading="loadingUsage" />
          </div>
          <div class="lg:col-span-1">
            <UserDashboardQuickActions />
          </div>
        </div>
      </template>

      <!-- Error / empty: one clear recovery action -->
      <div v-else class="card">
        <div class="state-panel">
          <div class="state-panel-icon">
            <Icon name="chart" size="lg" />
          </div>
          <p class="state-panel-title">{{ t('dashboard.loadFailedTitle') }}</p>
          <p class="state-panel-desc">{{ t('dashboard.loadFailedDesc') }}</p>
          <button type="button" class="btn btn-primary mt-1" @click="refreshAll">
            {{ t('dashboard.refresh') }}
          </button>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { usageAPI, type UserDashboardStats as UserStatsType } from '@/api/usage'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import UserDashboardStats from '@/components/user/dashboard/UserDashboardStats.vue'
import UserDashboardCharts from '@/components/user/dashboard/UserDashboardCharts.vue'
import UserDashboardRecentUsage from '@/components/user/dashboard/UserDashboardRecentUsage.vue'
import UserDashboardQuickActions from '@/components/user/dashboard/UserDashboardQuickActions.vue'
import type { UsageLog, TrendDataPoint, ModelStat, PlatformQuotaItem } from '@/types'
import { getMyPlatformQuotas } from '@/api/user'
import { formatDateLocalInput } from '@/utils/format'

const { t } = useI18n()
const authStore = useAuthStore()
const user = computed(() => authStore.user)

const stats = ref<UserStatsType | null>(null)
const loading = ref(false)
const loadingUsage = ref(false)
const loadingCharts = ref(false)
const trendData = ref<TrendDataPoint[]>([])
const modelStats = ref<ModelStat[]>([])
const recentUsage = ref<UsageLog[]>([])
const platformQuotas = ref<PlatformQuotaItem[] | null>(null)

const startDate = ref(formatDateLocalInput(new Date(Date.now() - 6 * 86400000)))
const endDate = ref(formatDateLocalInput(new Date()))
const granularity = ref('day')

const loadStats = async () => {
  loading.value = true
  try {
    await authStore.refreshUser()
    stats.value = await usageAPI.getDashboardStats()
  } catch (error) {
    console.error('Failed to load dashboard stats:', error)
    stats.value = null
  } finally {
    loading.value = false
  }
}

const loadCharts = async () => {
  loadingCharts.value = true
  try {
    const res = await Promise.all([
      usageAPI.getDashboardTrend({ start_date: startDate.value, end_date: endDate.value, granularity: granularity.value as any }),
      usageAPI.getDashboardModels({ start_date: startDate.value, end_date: endDate.value })
    ])
    trendData.value = res[0].trend || []
    modelStats.value = res[1].models || []
  } catch (error) {
    console.error('Failed to load charts:', error)
  } finally {
    loadingCharts.value = false
  }
}

const loadRecent = async () => {
  loadingUsage.value = true
  try {
    const res = await usageAPI.getByDateRange(startDate.value, endDate.value)
    recentUsage.value = res.items.slice(0, 5)
  } catch (error) {
    console.error('Failed to load recent usage:', error)
  } finally {
    loadingUsage.value = false
  }
}

const loadPlatformQuotas = async () => {
  try {
    const data = await getMyPlatformQuotas()
    platformQuotas.value = data.platform_quotas ?? []
  } catch (error) {
    console.warn('Failed to load platform quotas:', error)
    platformQuotas.value = []
  }
}

const refreshAll = () => {
  loadStats()
  loadCharts()
  loadRecent()
  loadPlatformQuotas()
}

onMounted(() => { refreshAll() })
</script>
