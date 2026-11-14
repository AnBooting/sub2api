<template>
  <div class="flex min-h-screen flex-col bg-paper text-ink">
    <!-- 页头：与首页同一张纸 -->
    <header class="border-b border-rule">
      <nav class="mx-auto flex min-w-0 max-w-6xl items-center justify-between gap-3 px-gutter py-3">
        <router-link to="/" class="flex min-h-11 min-w-0 items-center gap-2.5 sm:min-h-0">
          <img :src="siteLogo || '/logo.svg'" alt="" class="h-8 w-8 shrink-0 object-contain" />
          <span class="brand-name min-w-0 truncate">{{ siteName }}</span>
        </router-link>

        <div class="flex min-w-0 shrink items-center gap-1">
          <LocaleSwitcher />
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="btn btn-ghost btn-icon"
            :title="t('home.viewDocs')"
            :aria-label="t('home.viewDocs')"
          >
            <Icon name="book" size="md" />
          </a>
          <button
            type="button"
            class="btn btn-ghost btn-icon"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            :aria-label="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            @click="toggleTheme"
          >
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>
        </div>
      </nav>
    </header>

    <main class="mx-auto w-full min-w-0 max-w-5xl flex-1 px-gutter py-8 sm:py-10">
      <!-- 图签：标题 + 右侧读数 -->
      <div class="title-block">
        <div class="min-w-0">
          <h1 class="title-block-title">{{ t('keyUsage.title') }}</h1>
          <p class="mt-1.5 max-w-measure text-sm leading-[1.65] text-ink-soft">
            {{ t('keyUsage.subtitle') }}
          </p>
        </div>
        <dl v-if="showResults && resultData" class="title-block-meta">
          <div class="flex items-center gap-1.5">
            <dt>{{ t('keyUsage.dateRange') }}</dt>
            <dd class="text-ink">{{ currentRangeLabel }}</dd>
          </div>
          <div class="flex items-center gap-1.5">
            <dt>UTC</dt>
            <dd class="text-ink">{{ browserTimezone }}</dd>
          </div>
        </dl>
      </div>

      <!-- 查询：一个输入、一个动作 -->
      <section class="panel mb-5 min-w-0 p-4 sm:p-5">
        <form class="flex min-w-0 flex-col gap-2 sm:flex-row" @submit.prevent="queryKey">
          <div class="relative min-w-0 flex-1">
            <span class="pointer-events-none absolute inset-y-0 left-0 flex w-10 items-center justify-center text-ink-faint">
              <Icon name="lock" size="md" />
            </span>
            <input
              v-model="apiKey"
              :type="keyVisible ? 'text' : 'password'"
              :placeholder="t('keyUsage.placeholder')"
              :aria-label="t('keyUsage.placeholder')"
              autocomplete="off"
              spellcheck="false"
              class="input pl-10 pr-12"
              @keydown.enter.prevent="queryKey"
            />
            <button
              type="button"
              class="absolute inset-y-0 right-0 flex w-11 items-center justify-center text-ink-faint transition-colors hover:text-ink"
              :aria-label="keyVisible ? t('keyUsage.hideKey') : t('keyUsage.showKey')"
              @click="keyVisible = !keyVisible"
            >
              <Icon :name="keyVisible ? 'eyeOff' : 'eye'" size="md" />
            </button>
          </div>
          <button type="submit" :disabled="isQuerying" class="btn btn-primary shrink-0">
            <Icon v-if="isQuerying" name="refresh" size="sm" class="animate-spin" />
            <Icon v-else name="search" size="sm" />
            {{ isQuerying ? t('keyUsage.querying') : t('keyUsage.query') }}
          </button>
        </form>

        <p class="mt-2 text-xs leading-relaxed text-ink-faint">
          {{ t('keyUsage.privacyNote') }}
        </p>

        <!-- 统计范围 -->
        <div v-if="showDatePicker" class="mt-3 flex min-w-0 flex-wrap items-center gap-2 border-t border-rule pt-3">
          <span class="stat-label">{{ t('keyUsage.dateRange') }}</span>
          <button
            v-for="range in dateRanges"
            :key="range.key"
            type="button"
            class="btn btn-sm"
            :class="currentRange === range.key ? 'btn-primary' : 'btn-secondary'"
            @click="setDateRange(range.key)"
          >
            {{ range.label }}
          </button>
          <div v-if="currentRange === 'custom'" class="flex min-w-0 flex-wrap items-center gap-2">
            <input v-model="customStartDate" type="date" class="input w-auto min-w-0" />
            <span class="text-xs text-ink-faint">–</span>
            <input v-model="customEndDate" type="date" class="input w-auto min-w-0" />
            <button type="button" class="btn btn-sm btn-secondary" @click="queryKey">
              {{ t('keyUsage.apply') }}
            </button>
          </div>
        </div>
      </section>

      <!-- 尚未查询：空态写成邀请 -->
      <section v-if="!showResults" class="panel min-w-0">
        <div class="empty-state">
          <Icon name="key" size="lg" class="empty-state-icon" />
          <p class="empty-state-title">{{ t('keyUsage.emptyTitle') }}</p>
          <p class="empty-state-description">{{ t('keyUsage.emptyDescription') }}</p>
        </div>
      </section>

      <div v-else class="min-w-0 space-y-5">
        <!-- 载入 -->
        <div v-if="showLoading" class="min-w-0 space-y-5">
          <div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
            <div v-for="n in 4" :key="n" class="panel p-4">
              <div class="skeleton h-3 w-16"></div>
              <div class="mx-auto mt-4 aspect-square w-full max-w-[10.5rem] max-h-[10.5rem] rounded-full skeleton"></div>
            </div>
          </div>
          <div class="panel p-4 sm:p-5">
            <div class="skeleton h-3 w-24"></div>
            <div class="mt-4 space-y-3">
              <div class="skeleton h-3 w-full"></div>
              <div class="skeleton h-3 w-3/4"></div>
              <div class="skeleton h-3 w-5/6"></div>
            </div>
          </div>
        </div>

        <template v-else-if="resultData">
          <!-- 状态：一行读数，不戴胶囊 -->
          <div v-if="statusInfo" class="flex min-w-0 flex-wrap items-baseline gap-x-2.5 gap-y-1">
            <span
              class="inline-block h-2 w-2 shrink-0 translate-y-[-1px] rounded-full"
              :class="statusInfo.isActive ? 'bg-success-500' : 'bg-danger-500'"
              aria-hidden="true"
            ></span>
            <span class="text-sm font-medium text-ink">{{ statusInfo.label }}</span>
            <span class="text-ink-faint" aria-hidden="true">·</span>
            <span class="text-sm text-ink-soft">{{ statusInfo.statusText }}</span>
          </div>

          <!-- 用量环：单一信号色圆环 + 轨道底色 -->
          <div v-if="ringItems.length > 0" :class="ringGridClass">
            <figure
              v-for="(ring, i) in ringItems"
              :key="i"
              class="panel m-0 min-w-0 p-4"
            >
              <figcaption class="flex min-w-0 items-baseline justify-between gap-2">
                <span class="min-w-0 break-words text-[0.8125rem] font-medium text-ink">{{ ring.title }}</span>
                <Icon
                  :name="ring.iconType === 'clock' ? 'clock' : ring.iconType === 'calendar' ? 'calendar' : 'dollar'"
                  size="sm"
                  class="shrink-0 text-ink-faint"
                />
              </figcaption>

              <div class="mt-3 flex justify-center">
                <div class="relative w-full max-w-[10.5rem]">
                  <svg viewBox="0 0 160 160" class="h-auto w-full -rotate-90" aria-hidden="true">
                    <circle class="ring-track" cx="80" cy="80" r="68" fill="none" stroke-width="10" />
                    <!-- 钱包余额没有「额度」可比，画满环会读成用光了，所以只画轨道 -->
                    <circle
                      v-if="!ring.isBalance"
                      class="ring-progress"
                      cx="80" cy="80" r="68" fill="none"
                      stroke-width="10" stroke-linecap="round"
                      :stroke-dasharray="CIRCUMFERENCE.toFixed(2)"
                      :stroke-dashoffset="getRingOffset(ring)"
                    />
                  </svg>

                  <div class="absolute inset-0 flex flex-col items-center justify-center px-2 text-center">
                    <template v-if="ring.isBalance">
                      <span class="font-read text-[1.0625rem] font-semibold tabular-nums text-ink">
                        {{ ring.amount }}
                      </span>
                      <span class="stat-label mt-1">{{ t('keyUsage.walletBalance') }}</span>
                    </template>
                    <template v-else>
                      <!-- 第一环是全页唯一的衬线大读数；其余用等宽无衬线，避免同屏衬线泛滥 -->
                      <span
                        :class="i === 0
                          ? 'readout-display ring-readout'
                          : 'font-read text-[1.25rem] font-semibold tabular-nums text-ink'"
                      >{{ ring.pct }}%</span>
                      <span class="stat-label mt-1">{{ t('keyUsage.used') }}</span>
                    </template>
                  </div>
                </div>
              </div>

              <!-- 用量写在环下面：环心的宽度不够放 "已用 / 上限"，塞进去会压出圆环 -->
              <p
                v-if="!ring.isBalance"
                class="mt-2 break-words text-center font-read text-[0.75rem] tabular-nums text-ink-soft"
              >
                {{ ring.amount }}
              </p>

              <p
                v-if="ring.resetAt && formatResetTime(ring.resetAt)"
                class="mt-2 text-center font-read text-[0.6875rem] tabular-nums text-ink-faint"
              >
                ⟳ {{ formatResetTime(ring.resetAt) }}
              </p>
            </figure>
          </div>

          <!-- 详细信息 -->
          <section v-if="detailRows.length > 0" class="panel min-w-0 overflow-hidden">
            <div class="panel-header">
              <h2 class="text-[0.8125rem] font-medium text-ink">{{ t('keyUsage.detailInfo') }}</h2>
            </div>
            <dl class="min-w-0 divide-y divide-rule">
              <div
                v-for="(row, i) in detailRows"
                :key="i"
                class="flex min-w-0 items-center justify-between gap-3 px-4 py-2.5 sm:px-5"
              >
                <dt class="flex min-w-0 items-center gap-2.5">
                  <span class="flex h-6 w-6 shrink-0 items-center justify-center" :class="row.iconBg">
                    <svg
                      class="h-3.5 w-3.5"
                      :class="row.iconColor"
                      viewBox="0 0 24 24" fill="none" stroke="currentColor"
                      stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
                      v-html="sanitizeSvg(row.iconSvg)"
                    ></svg>
                  </span>
                  <span class="min-w-0 break-words text-sm text-ink-soft">{{ row.label }}</span>
                </dt>
                <dd class="shrink-0 font-read text-sm tabular-nums" :class="row.valueClass || 'text-ink'">
                  {{ row.value }}
                </dd>
              </div>
            </dl>
          </section>

          <!-- Token 统计 -->
          <section v-if="usageStatCells.length > 0" class="panel min-w-0 overflow-hidden">
            <div class="panel-header">
              <h2 class="text-[0.8125rem] font-medium text-ink">{{ t('keyUsage.tokenStats') }}</h2>
            </div>
            <dl class="grid grid-cols-2 gap-px bg-rule sm:grid-cols-3 lg:grid-cols-4">
              <div v-for="(cell, i) in usageStatCells" :key="i" class="min-w-0 bg-surface px-4 py-3">
                <dt class="stat-label">{{ cell.label }}</dt>
                <dd class="mt-0.5 break-words font-read text-sm tabular-nums text-ink">{{ cell.value }}</dd>
              </div>
            </dl>
          </section>

          <!-- 按日明细 -->
          <section v-if="showDailyUsage" class="panel min-w-0 overflow-hidden">
            <div class="panel-header flex-wrap">
              <h2 class="text-[0.8125rem] font-medium text-ink">{{ t('keyUsage.dailyDetail') }}</h2>
              <div class="flex items-center gap-1">
                <button
                  v-for="option in dailyUsageOptions"
                  :key="option.value"
                  type="button"
                  class="btn btn-sm"
                  :class="dailyUsageDays === option.value ? 'btn-primary' : 'btn-secondary'"
                  @click="setDailyUsageDays(option.value)"
                >
                  {{ option.label }}
                </button>
              </div>
            </div>

            <div v-if="dailyUsageRows.length > 0" class="w-full min-w-0 overflow-x-auto">
              <table class="table">
                <thead>
                  <tr>
                    <th scope="col">{{ t('keyUsage.date') }}</th>
                    <th scope="col" class="num">{{ t('keyUsage.requests') }}</th>
                    <th scope="col" class="num">{{ t('keyUsage.inputTokens') }}</th>
                    <th scope="col" class="num">{{ t('keyUsage.outputTokens') }}</th>
                    <th scope="col" class="num">{{ t('keyUsage.cacheReadTokens') }}</th>
                    <th scope="col" class="num">{{ t('keyUsage.cacheWriteTokens') }}</th>
                    <th scope="col" class="num">{{ t('keyUsage.cost') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="row in dailyUsageRows" :key="row.date">
                    <td class="whitespace-nowrap font-medium text-ink">{{ row.date }}</td>
                    <td class="num">{{ fmtNum(row.requests) }}</td>
                    <td class="num">{{ fmtNum(row.input_tokens) }}</td>
                    <td class="num">{{ fmtNum(row.output_tokens) }}</td>
                    <td class="num">{{ fmtNum(row.cache_read_tokens) }}</td>
                    <td class="num">{{ fmtNum(row.cache_write_tokens) }}</td>
                    <td class="num text-ink">{{ usd(row.actual_cost != null ? row.actual_cost : row.cost) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div v-else class="empty-state">
              <p class="empty-state-description">{{ t('keyUsage.noDailyUsage') }}</p>
            </div>
          </section>

          <!-- 模型用量 -->
          <section v-if="modelStats.length > 0" class="panel min-w-0 overflow-hidden">
            <div class="panel-header">
              <h2 class="text-[0.8125rem] font-medium text-ink">{{ t('keyUsage.modelStats') }}</h2>
            </div>
            <div class="w-full min-w-0 overflow-x-auto">
              <table class="table">
                <thead>
                  <tr>
                    <th scope="col">{{ t('keyUsage.model') }}</th>
                    <th scope="col" class="num">{{ t('keyUsage.requests') }}</th>
                    <th scope="col" class="num">{{ t('keyUsage.inputTokens') }}</th>
                    <th scope="col" class="num">{{ t('keyUsage.outputTokens') }}</th>
                    <th scope="col" class="num">{{ t('keyUsage.cacheCreationTokens') }}</th>
                    <th scope="col" class="num">{{ t('keyUsage.cacheReadTokens') }}</th>
                    <th scope="col" class="num">{{ t('keyUsage.totalTokens') }}</th>
                    <th scope="col" class="num">{{ t('keyUsage.cost') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(m, i) in modelStats" :key="i">
                    <td class="whitespace-nowrap font-medium text-ink">{{ m.model || '-' }}</td>
                    <td class="num">{{ fmtNum(m.requests) }}</td>
                    <td class="num">{{ fmtNum(m.input_tokens) }}</td>
                    <td class="num">{{ fmtNum(m.output_tokens) }}</td>
                    <td class="num">{{ fmtNum(m.cache_creation_tokens) }}</td>
                    <td class="num">{{ fmtNum(m.cache_read_tokens) }}</td>
                    <td class="num">{{ fmtNum(m.total_tokens) }}</td>
                    <td class="num text-ink">{{ usd(m.actual_cost != null ? m.actual_cost : m.cost) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>
        </template>
      </div>
    </main>

    <footer class="border-t border-rule">
      <div class="mx-auto flex max-w-6xl flex-col gap-2 px-gutter py-4 text-xs text-ink-faint sm:flex-row sm:items-center sm:justify-between">
        <p class="[overflow-wrap:anywhere]">
          &copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}
        </p>
        <div class="flex flex-wrap items-center gap-x-4">
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="inline-flex min-h-11 items-center hover:text-ink sm:min-h-0"
          >{{ t('home.docs') }}</a>
          <a
            :href="githubUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="inline-flex min-h-11 items-center hover:text-ink sm:min-h-0"
          >GitHub</a>
        </div>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { formatCustomerCredit } from '@/utils/billingCredits'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import { FeatureFlags, resolveFeatureFlag } from '@/utils/featureFlags'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import { buildGatewayUrl } from '@/api/client'
import { formatDateLocalInput } from '@/utils/format'
import { sanitizeUrl } from '@/utils/url'
import { sanitizeSvg } from '@/utils/sanitize'
import { isDarkTheme, toggleTheme as toggleThemeState, initTheme as applyStoredTheme } from '@/composables/useTheme'

const { t, locale } = useI18n()
const appStore = useAppStore()
const subscriptionFeatureEnabled = computed(() => resolveFeatureFlag(appStore.cachedPublicSettings, FeatureFlags.subscription))

// ==================== Site Settings (same as HomeView) ====================

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const docUrl = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || ''))
const githubUrl = 'https://github.com/Wei-Shaw/sub2api'

// ==================== Theme ====================

// 主题只有一个入口（composables/useTheme），它同时同步 class / theme-color / 存储。
const isDark = isDarkTheme
const toggleTheme = () => toggleThemeState()

const currentYear = computed(() => new Date().getFullYear())

// 图签右侧读数：当前统计范围与浏览器时区（后端按它切日）
const currentRangeLabel = computed(
  () => dateRanges.value.find((range) => range.key === currentRange.value)?.label || '',
)
const browserTimezone = computed(() => getBrowserTimezone())

// ==================== Key Query State ====================

const apiKey = ref('')
const keyVisible = ref(false)
const isQuerying = ref(false)
const showResults = ref(false)
const showLoading = ref(false)
const showDatePicker = ref(false)
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const resultData = ref<any>(null)
const now = ref(new Date())
let resetTimer: ReturnType<typeof setInterval> | null = null

// ==================== Date Range State ====================

type DateRangeKey = 'today' | '7d' | '30d' | 'custom'
const currentRange = ref<DateRangeKey>('today')
const customStartDate = ref('')
const customEndDate = ref('')
const dailyUsageDays = ref<7 | 30 | 90>(30)

const dateRanges = computed(() => [
  { key: 'today' as const, label: t('keyUsage.dateRangeToday') },
  { key: '7d' as const, label: t('keyUsage.dateRange7d') },
  { key: '30d' as const, label: t('keyUsage.dateRange30d') },
  { key: 'custom' as const, label: t('keyUsage.dateRangeCustom') },
])

const dailyUsageOptions = computed(() => [
  { value: 7 as const, label: t('keyUsage.dateRange7d') },
  { value: 30 as const, label: t('keyUsage.dateRange30d') },
  { value: 90 as const, label: t('keyUsage.dateRange90d') },
])

function setDateRange(key: DateRangeKey) {
  currentRange.value = key
  if (key !== 'custom') {
    queryKey()
  }
}

function getDateParams(): string {
  const now = new Date()
  const params = new URLSearchParams()

  if (currentRange.value === 'custom') {
    if (customStartDate.value && customEndDate.value) {
      params.set('start_date', customStartDate.value)
      params.set('end_date', customEndDate.value)
    }
  } else {
    const end = formatDateLocalInput(now)
    let start: string
    switch (currentRange.value) {
      case 'today': start = end; break
      case '7d': start = formatDateLocalInput(new Date(now.getTime() - 7 * 86400000)); break
      case '30d': start = formatDateLocalInput(new Date(now.getTime() - 30 * 86400000)); break
      default: start = formatDateLocalInput(new Date(now.getTime() - 30 * 86400000))
    }
    params.set('start_date', start)
    params.set('end_date', end)
  }
  params.set('days', String(dailyUsageDays.value))
  params.set('timezone', getBrowserTimezone())
  return params.toString()
}

function setDailyUsageDays(days: 7 | 30 | 90) {
  if (dailyUsageDays.value === days) return
  dailyUsageDays.value = days
  if (resultData.value && apiKey.value.trim()) {
    queryKey()
  }
}

// ==================== Ring Animation ====================

// 用量环只表达「消耗了多少」：单一信号色 + 轨道底色即可，不需要四色渐变。
// 环的颜色由 .ring-track / .ring-progress 从令牌取，脚本里不出现任何色值。
const CIRCUMFERENCE = 2 * Math.PI * 68

const ringAnimated = ref(false)

interface RingItem {
  title: string
  pct: number
  amount: string
  isBalance?: boolean
  iconType: 'clock' | 'calendar' | 'dollar'
  resetAt?: string | null
}

function getRingOffset(ring: RingItem): number {
  if (!ringAnimated.value) return CIRCUMFERENCE
  if (ring.isBalance) return 0
  return CIRCUMFERENCE - (Math.min(ring.pct, 100) / 100) * CIRCUMFERENCE
}

/** 环只在数据到达后画一次：先归零再放开，让 stroke-dashoffset 从满轨道走过去。 */
function triggerRingAnimation(_items: RingItem[]) {
  ringAnimated.value = false
  nextTick(() => {
    requestAnimationFrame(() => {
      setTimeout(() => {
        ringAnimated.value = true
      }, 50)
    })
  })
}

// ==================== Computed Data ====================

const statusInfo = computed(() => {
  const data = resultData.value
  if (!data) return null

  if (data.mode === 'quota_limited') {
    const isValid = data.isValid !== false
    const statusMap: Record<string, string> = {
      active: 'Active',
      quota_exhausted: 'Quota Exhausted',
      expired: 'Expired',
    }
    return {
      label: t('keyUsage.quotaMode'),
      statusText: statusMap[data.status] || data.status || 'Unknown',
      isActive: isValid && data.status === 'active',
    }
  }

  return {
    label: data.planName || t('keyUsage.walletBalance'),
    statusText: 'Active',
    isActive: true,
  }
})

const ringItems = computed<RingItem[]>(() => {
  const data = resultData.value
  if (!data) return []

  const items: RingItem[] = []

  if (data.mode === 'quota_limited') {
    if (data.quota) {
      const pct = data.quota.limit > 0 ? Math.min(Math.round((data.quota.used / data.quota.limit) * 100), 100) : 0
      items.push({ title: t('keyUsage.totalQuota'), pct, amount: `${usd(data.quota.used)} / ${usd(data.quota.limit)}`, iconType: 'dollar' })
    }
    if (data.rate_limits) {
      const windowLabels: Record<string, string> = { '5h': t('keyUsage.limit5h'), '1d': t('keyUsage.limitDaily'), '7d': t('keyUsage.limit7d') }
      const windowIcons: Record<string, 'clock' | 'calendar'> = { '5h': 'clock', '1d': 'calendar', '7d': 'calendar' }
      for (const rl of data.rate_limits) {
        const pct = rl.limit > 0 ? Math.min(Math.round((rl.used / rl.limit) * 100), 100) : 0
        items.push({
          title: windowLabels[rl.window] || rl.window,
          pct,
          amount: `${usd(rl.used)} / ${usd(rl.limit)}`,
          iconType: windowIcons[rl.window] || 'clock',
          resetAt: rl.reset_at,
        })
      }
    }
  } else {
    if (data.subscription) {
      const sub = data.subscription
      const limits = [
        { label: t('keyUsage.limitDaily'), usage: sub.daily_usage_usd, limit: sub.daily_limit_usd },
        { label: t('keyUsage.limitWeekly'), usage: sub.weekly_usage_usd, limit: sub.weekly_limit_usd },
        { label: t('keyUsage.limitMonthly'), usage: sub.monthly_usage_usd, limit: sub.monthly_limit_usd },
      ]
      for (const l of limits) {
        if (l.limit != null && l.limit > 0) {
          const pct = Math.min(Math.round((l.usage / l.limit) * 100), 100)
          items.push({ title: l.label, pct, amount: `${usd(l.usage)} / ${usd(l.limit)}`, iconType: 'calendar' })
        }
      }
    }
    if (!data.subscription && data.balance != null) {
      items.push({ title: t('keyUsage.walletBalance'), pct: 0, amount: usd(data.balance), isBalance: true, iconType: 'dollar' })
    }
  }

  return items
})

// 窄屏两列：环是小读数，一列会把页面拉得过长；四列在 320 下每个环只剩 60px。
const ringGridClass = computed(() => {
  if (ringItems.value.length === 1) return 'grid grid-cols-1 mx-auto max-w-md gap-3'
  return 'grid grid-cols-2 gap-3 lg:grid-cols-4'
})

interface DetailRow {
  iconBg: string
  iconColor: string
  iconSvg: string
  label: string
  value: string
  valueClass: string
}

function getUsageColor(pct: number): string {
  if (pct > 90) return 'text-danger-500'
  if (pct > 70) return 'text-warning-500'
  return 'text-success-500'
}

const detailRows = computed<DetailRow[]>(() => {
  const data = resultData.value
  if (!data) return []

  const rows: DetailRow[] = []
  const ICON_SHIELD = '<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>'
  const ICON_CALENDAR = '<rect x="3" y="4" width="18" height="18" rx="2" ry="2"/><line x1="16" y1="2" x2="16" y2="6"/><line x1="8" y1="2" x2="8" y2="6"/><line x1="3" y1="10" x2="21" y2="10"/>'
  const ICON_DOLLAR = '<line x1="12" y1="1" x2="12" y2="23"/><path d="M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"/>'
  const ICON_CHECK = '<polyline points="20 6 9 17 4 12"/>'

  if (data.mode === 'quota_limited') {
    if (data.quota) {
      const remainColor = data.quota.remaining <= 0 ? 'text-danger-500'
        : data.quota.remaining < data.quota.limit * 0.1 ? 'text-warning-500'
        : 'text-success-500'
      rows.push({
        iconBg: 'bg-success-500/10', iconColor: 'text-success-500', iconSvg: ICON_SHIELD,
        label: t('keyUsage.remainingQuota'), value: usd(data.quota.remaining), valueClass: remainColor,
      })
    }
    if (data.expires_at) {
      const daysLeft = data.days_until_expiry
      let expiryStr = formatDate(data.expires_at)
      if (daysLeft != null) {
        expiryStr += daysLeft > 0 ? ` ${t('keyUsage.daysLeft', { days: daysLeft })}` : daysLeft === 0 ? ` ${t('keyUsage.todayExpires')}` : ''
      }
      rows.push({
        iconBg: 'bg-warning-500/10', iconColor: 'text-warning-500', iconSvg: ICON_CALENDAR,
        label: t('keyUsage.expiresAt'), value: expiryStr, valueClass: '',
      })
    }
    if (data.rate_limits) {
      const windowMap: Record<string, string> = { '5h': '5H', '1d': locale.value === 'zh' ? '日' : 'D', '7d': '7D' }
      for (const rl of data.rate_limits) {
        const pct = rl.limit > 0 ? (rl.used / rl.limit) * 100 : 0
        let valueStr = `${usd(rl.used)} / ${usd(rl.limit)}`
        const resetStr = formatResetTime(rl.reset_at)
        if (resetStr) {
          valueStr += ` (⟳ ${resetStr})`
        }
        rows.push({
          iconBg: 'bg-primary-500/10', iconColor: 'text-primary-500', iconSvg: ICON_DOLLAR,
          label: `${t('keyUsage.usedQuota')} (${windowMap[rl.window] || rl.window})`,
          value: valueStr,
          valueClass: getUsageColor(pct),
        })
      }
    }
  } else {
    rows.push({
      iconBg: 'bg-success-500/10', iconColor: 'text-success-500', iconSvg: ICON_CHECK,
      // 订阅功能关闭后这一行只会是「钱包余额」，标签改用不带「订阅」字样的「计费方式」。
      label: subscriptionFeatureEnabled.value ? t('keyUsage.subscriptionType') : t('keyUsage.billingType'),
      value: data.planName || t('keyUsage.walletBalance'), valueClass: '',
    })

    if (data.subscription) {
      const sub = data.subscription
      if (sub.daily_limit_usd > 0) {
        const pct = (sub.daily_usage_usd / sub.daily_limit_usd) * 100
        rows.push({
          iconBg: 'bg-primary-500/10', iconColor: 'text-primary-500', iconSvg: ICON_DOLLAR,
          label: `${t('keyUsage.usedQuota')} (${locale.value === 'zh' ? '日' : 'D'})`, value: `${usd(sub.daily_usage_usd)} / ${usd(sub.daily_limit_usd)}`, valueClass: getUsageColor(pct),
        })
      }
      if (sub.weekly_limit_usd > 0) {
        const pct = (sub.weekly_usage_usd / sub.weekly_limit_usd) * 100
        rows.push({
          iconBg: 'bg-primary-500/10', iconColor: 'text-primary-500', iconSvg: ICON_DOLLAR,
          label: `${t('keyUsage.usedQuota')} (${locale.value === 'zh' ? '周' : 'W'})`, value: `${usd(sub.weekly_usage_usd)} / ${usd(sub.weekly_limit_usd)}`, valueClass: getUsageColor(pct),
        })
      }
      if (sub.monthly_limit_usd > 0) {
        const pct = (sub.monthly_usage_usd / sub.monthly_limit_usd) * 100
        rows.push({
          iconBg: 'bg-success-500/10', iconColor: 'text-success-500', iconSvg: ICON_DOLLAR,
          label: `${t('keyUsage.usedQuota')} (${locale.value === 'zh' ? '月' : 'M'})`, value: `${usd(sub.monthly_usage_usd)} / ${usd(sub.monthly_limit_usd)}`, valueClass: getUsageColor(pct),
        })
      }
      if (sub.expires_at) {
        rows.push({
          iconBg: 'bg-warning-500/10', iconColor: 'text-warning-500', iconSvg: ICON_CALENDAR,
          label: t('keyUsage.subscriptionExpires'), value: formatDate(sub.expires_at), valueClass: '',
        })
      }
    }

    const remainColor = data.remaining != null
      ? (data.remaining <= 0 ? 'text-danger-500' : data.remaining < 10 ? 'text-warning-500' : 'text-success-500')
      : ''
    rows.push({
      iconBg: 'bg-success-500/10', iconColor: 'text-success-500', iconSvg: ICON_SHIELD,
      label: t('keyUsage.remainingQuota'), value: data.remaining != null ? usd(data.remaining) : '-', valueClass: remainColor,
    })
  }

  return rows
})

interface StatCell {
  label: string
  value: string
}

const usageStatCells = computed<StatCell[]>(() => {
  const usage = resultData.value?.usage
  if (!usage) return []

  const today = usage.today || {}
  const total = usage.total || {}

  return [
    { label: t('keyUsage.todayRequests'), value: fmtNum(today.requests) },
    { label: t('keyUsage.todayInputTokens'), value: fmtNum(today.input_tokens) },
    { label: t('keyUsage.todayOutputTokens'), value: fmtNum(today.output_tokens) },
    { label: t('keyUsage.todayTokens'), value: fmtNum(today.total_tokens) },
    { label: t('keyUsage.todayCacheCreation'), value: fmtNum(today.cache_creation_tokens) },
    { label: t('keyUsage.todayCacheRead'), value: fmtNum(today.cache_read_tokens) },
    { label: t('keyUsage.todayCost'), value: usd(today.actual_cost) },
    { label: t('keyUsage.rpmTpm'), value: `${usage.rpm || 0} / ${usage.tpm || 0}` },
    { label: t('keyUsage.totalRequests'), value: fmtNum(total.requests) },
    { label: t('keyUsage.totalInputTokens'), value: fmtNum(total.input_tokens) },
    { label: t('keyUsage.totalOutputTokens'), value: fmtNum(total.output_tokens) },
    { label: t('keyUsage.totalTokensLabel'), value: fmtNum(total.total_tokens) },
    { label: t('keyUsage.totalCacheCreation'), value: fmtNum(total.cache_creation_tokens) },
    { label: t('keyUsage.totalCacheRead'), value: fmtNum(total.cache_read_tokens) },
    { label: t('keyUsage.totalCost'), value: usd(total.actual_cost) },
    { label: t('keyUsage.avgDuration'), value: usage.average_duration_ms ? `${Math.round(usage.average_duration_ms)} ms` : '-' },
  ]
})

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const modelStats = computed<any[]>(() => resultData.value?.model_stats || [])

interface DailyUsageRow {
  date: string
  requests: number
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  cost: number
  actual_cost?: number
}

const dailyUsageRows = computed<DailyUsageRow[]>(() => {
  const rows = resultData.value?.daily_usage
  return Array.isArray(rows) ? rows : []
})

const showDailyUsage = computed(() => Boolean(resultData.value && Array.isArray(resultData.value.daily_usage)))

// ==================== Utility Functions ====================

function usd(value: number | null | undefined): string {
  if (value == null || value < 0) return '-'
  return formatCustomerCredit(value)
}

function fmtNum(val: number | null | undefined): string {
  if (val == null) return '-'
  return val.toLocaleString()
}

function formatDate(iso: string | null | undefined): string {
  if (!iso) return '-'
  const d = new Date(iso)
  const loc = locale.value === 'zh' ? 'zh-CN' : 'en-US'
  return d.toLocaleDateString(loc, { year: 'numeric', month: 'long', day: 'numeric' })
}

function getBrowserTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  } catch {
    return 'UTC'
  }
}

// ==================== API Query ====================

async function fetchUsage(key: string) {
  const dateParams = getDateParams()
  const url = buildGatewayUrl('/v1/usage') + (dateParams ? '?' + dateParams : '')
  const res = await fetch(url, {
    headers: { 'Authorization': 'Bearer ' + key },
  })
  if (!res.ok) {
    const body = await res.json().catch(() => null)
    const msg = body?.error?.message || body?.message || `${t('keyUsage.queryFailed')} (${res.status})`
    throw new Error(msg)
  }
  return await res.json()
}

async function queryKey() {
  if (isQuerying.value) return
  const key = apiKey.value.trim()
  if (!key) {
    appStore.showInfo(t('keyUsage.enterApiKey'))
    return
  }

  isQuerying.value = true
  showResults.value = true
  showLoading.value = true
  resultData.value = null

  try {
    const data = await fetchUsage(key)
    resultData.value = data
    showLoading.value = false
    showDatePicker.value = true

    // Trigger ring animations after DOM update
    nextTick(() => {
      triggerRingAnimation(ringItems.value)
    })

    appStore.showSuccess(t('keyUsage.querySuccess'))
  } catch (err) {
    showResults.value = false
    showLoading.value = false
    appStore.showError((err as Error).message || t('keyUsage.queryFailedRetry'))
  } finally {
    isQuerying.value = false
  }
}

// ==================== Lifecycle ====================

// 主题只走 composables/useTheme：它同时改根节点 class、theme-color 与存储。
// 早期这里自己往根节点加 dark 类，会留下「深色纸 + 浅色系统栏」的裂缝。
function initTheme() {
  applyStoredTheme()
}

function formatResetTime(resetAt: string | null | undefined): string {
  if (!resetAt) return ''
  const diff = new Date(resetAt).getTime() - now.value.getTime()
  if (diff <= 0) return t('keyUsage.resetNow')
  const days = Math.floor(diff / 86400000)
  const hours = Math.floor((diff % 86400000) / 3600000)
  const mins = Math.floor((diff % 3600000) / 60000)
  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${mins}m`
  return `${mins}m`
}

onMounted(() => {
  initTheme()
  if (!appStore.publicSettingsLoaded) {
    appStore.fetchPublicSettings()
  }
  resetTimer = setInterval(() => { now.value = new Date() }, 60000)
})

onUnmounted(() => {
  if (resetTimer) clearInterval(resetTimer)
})
</script>
<style scoped>
/* v2「暖纸上的编辑体」：本页只有两处需要自定义 —— 环的描边色与一次划线进场的动效。
 * 所有色值都取自设计令牌，文件里不出现任何色字面量。 */

/* 品牌名：与首页同一处衬线（本页唯一的衬线除了环心大读数） */
.brand-name {
  font-family: var(--font-display);
  font-size: 1rem;
  line-height: 1.25;
  letter-spacing: var(--track-display);
  color: rgb(var(--ink));
}

/* 用量环：单一信号色。轨道用比痕量线更轻的一档，让"已用 / 未用"靠明度分开。 */
.ring-track {
  stroke: rgb(var(--rule) / 0.85);
}

.ring-progress {
  stroke: rgb(var(--signal));
  transition: stroke-dashoffset var(--motion-trace) var(--ease-settle);
}

/* 环心大读数：字号由 .readout-display 给，这里只收到环的尺度内 */
.ring-readout {
  font-size: clamp(1.25rem, 1rem + 1vw, 1.75rem);
}

/* 页面进入：内容从 8px 落下一次，不做逐卡错峰 */
@keyframes kv-settle {
  from { opacity: 0; transform: translateY(8px); }
  to { opacity: 1; transform: none; }
}

@media (prefers-reduced-motion: reduce) {
  .ring-progress { transition: none; }
}
</style>