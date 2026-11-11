<template>
  <!-- 自定义首页：整页模式（管理员配置 home_content） -->
  <div v-if="hasHomeContent" class="min-h-screen bg-paper text-ink">
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-screen w-full border-0"
      allowfullscreen
    ></iframe>
    <!-- HTML 模式 - SECURITY: homeContent 是管理员配置项，其 XSS 风险已被接受 -->
    <div v-else v-html="homeContent"></div>
  </div>

  <!-- 紧凑首页：品牌、一句说明、一个入口 -->
  <div
    v-else-if="compactHomeEnabled"
    data-testid="compact-home"
    class="flex min-h-screen flex-col bg-paper text-ink"
  >
    <header class="border-b border-rule">
      <nav class="mx-auto flex min-w-0 max-w-5xl flex-wrap items-center justify-between gap-3 px-gutter py-3">
        <div class="home-brand flex min-w-0 flex-1 items-center gap-2.5">
          <img :src="siteLogo || '/logo.svg'" alt="" class="h-8 w-8 shrink-0 object-contain" />
          <span class="brand-name min-w-0 truncate">{{ siteName }}</span>
        </div>
        <div class="flex max-w-full shrink-0 flex-wrap items-center justify-end gap-1.5">
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
          <router-link
            v-if="showModelPlazaEntry"
            to="/model-plaza"
            class="btn btn-ghost"
            :title="t('nav.modelPlaza')"
          >
            <Icon name="grid" size="md" />
            <span class="hidden sm:inline">{{ t('nav.modelPlaza') }}</span>
          </router-link>
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
          <router-link :to="isAuthenticated ? dashboardPath : '/login'" class="btn btn-primary">
            {{ isAuthenticated ? t('home.dashboard') : t('home.login') }}
          </router-link>
        </div>
      </nav>
    </header>

    <main class="flex min-w-0 flex-1 items-center px-gutter py-16">
      <div class="mx-auto w-full max-w-measure">
        <h1 class="title-block-display [overflow-wrap:anywhere]">{{ siteName }}</h1>
        <p class="lead mt-4 whitespace-pre-wrap [overflow-wrap:anywhere]">
          {{ siteSubtitle }}
        </p>
        <router-link
          :to="isAuthenticated ? dashboardPath : '/login'"
          class="btn btn-primary mt-7"
        >
          {{ isAuthenticated ? t('home.goToDashboard') : t('home.login') }}
          <Icon name="arrowRight" size="sm" />
        </router-link>
      </div>
    </main>

    <footer class="border-t border-rule px-gutter py-4">
      <p class="mx-auto max-w-5xl text-xs text-ink-faint [overflow-wrap:anywhere]">
        &copy; {{ currentYear }} {{ siteName }}
      </p>
    </footer>
  </div>

  <!-- 默认首页：一次请求在图上的走向 -->
  <div v-else class="flex min-h-screen flex-col bg-paper text-ink">
    <header class="border-b border-rule">
      <nav class="mx-auto flex min-w-0 max-w-6xl items-center justify-between gap-3 px-gutter py-3">
        <router-link to="/" class="home-brand flex min-w-0 items-center gap-2.5">
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
          <router-link
            v-if="showModelPlazaEntry"
            to="/model-plaza"
            class="btn btn-ghost"
            :title="t('nav.modelPlaza')"
          >
            <Icon name="grid" size="md" />
            <span class="hidden sm:inline">{{ t('nav.modelPlaza') }}</span>
          </router-link>
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
          <router-link
            :to="isAuthenticated ? dashboardPath : '/login'"
            class="btn btn-primary ml-1"
          >
            {{ isAuthenticated ? t('home.dashboard') : t('home.login') }}
          </router-link>
        </div>
      </nav>
    </header>

    <main class="min-w-0 flex-1">
      <!-- Hero：左边这一页是什么，右边画出请求怎么走 -->
      <section class="mx-auto max-w-6xl px-gutter pb-10 pt-12 sm:pt-16">
        <div class="grid min-w-0 gap-10 lg:grid-cols-12 lg:gap-12">
          <div class="min-w-0 lg:col-span-5">
            <h1 class="title-block-display [overflow-wrap:anywhere]">
              {{ siteName }}
            </h1>
            <p class="lead mt-4 [overflow-wrap:anywhere]">
              {{ siteSubtitle }}
            </p>
            <p class="mt-3 max-w-measure text-sm leading-[1.65] text-ink-soft [overflow-wrap:anywhere]">
              {{ t('home.heroDescription') }}
            </p>

            <div class="mt-7 flex flex-wrap items-center gap-2">
              <router-link
                :to="isAuthenticated ? dashboardPath : '/login'"
                class="btn btn-primary"
              >
                {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
                <Icon name="arrowRight" size="sm" />
              </router-link>
              <a
                v-if="docUrl"
                :href="docUrl"
                target="_blank"
                rel="noopener noreferrer"
                class="btn btn-secondary"
              >
                {{ t('home.viewDocs') }}
              </a>
            </div>

            <!-- 图签读数：接口地址与版本 -->
            <dl class="mt-8 flex flex-wrap gap-x-8 gap-y-2 border-t border-rule pt-3">
              <div class="min-w-0">
                <dt class="stat-label">{{ t('home.patch.baseUrl') }}</dt>
                <dd class="mt-0.5 min-w-0 break-all font-read text-[0.8125rem] tabular-nums text-ink">
                  {{ apiBaseUrl || dash }}
                </dd>
              </div>
              <div class="min-w-0">
                <dt class="stat-label">{{ t('home.patch.version') }}</dt>
                <dd class="mt-0.5 font-read text-[0.8125rem] tabular-nums text-ink">
                  {{ siteVersion || dash }}
                </dd>
              </div>
            </dl>
          </div>

          <!-- 主视觉：请求在图上的走向。宽屏横排、窄屏纵排，同一张图纸。 -->
          <div class="min-w-0 lg:col-span-7">
            <figure
              data-testid="patch-diagram"
              class="panel min-w-0 overflow-hidden"
              :aria-label="t('home.patch.title')"
            >
              <figcaption class="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 border-b border-rule px-3 py-2.5 sm:px-5">
                <span class="text-[0.8125rem] font-medium text-ink">{{ t('home.patch.title') }}</span>
                <span class="font-read text-[0.6875rem] tabular-nums text-ink-faint">
                  {{ requestLine }}
                </span>
              </figcaption>

              <div class="patch-body px-3 py-3 sm:px-5 sm:py-4">
                <div class="patch-col patch-col-ports">
                  <div
                    v-for="port in upstreamPorts"
                    :key="port.key"
                    class="patch-port"
                    :class="'is-' + lineState"
                  >
                    <span class="patch-port-dot" aria-hidden="true"></span>
                    <span class="patch-port-name">{{ t(port.label) }}</span>
                  </div>
                </div>

                <div class="patch-col patch-col-nodes" :class="'is-' + lineState">
                  <div
                    v-for="stage in stages"
                    :key="stage.key"
                    class="patch-node"
                    :class="'is-' + stage.state"
                  >
                    <div class="patch-node-head">
                      <span class="patch-node-name">{{ t(stage.label) }}</span>
                      <!-- 落账节点的读数是账户余额：全页最大的那个数，用衬线大读数 -->
                      <span
                        v-if="stage.key === 'ledger'"
                        class="readout-display patch-node-read-display"
                      >{{ stage.readout }}</span>
                      <span v-else class="patch-node-read">{{ stage.readout }}</span>
                    </div>
                    <p class="patch-note">{{ t(stage.hint) }}</p>
                  </div>

                  <!-- 备用线路：虚线，主线路断掉时接通 -->
                  <div class="patch-failover" :class="'is-' + failoverState">
                    <span class="patch-failover-tag">{{ t('home.patch.failover') }}</span>
                    <span class="patch-note patch-note-inline">{{ t('home.patch.failoverHint') }}</span>
                  </div>
                </div>
              </div>

              <p class="patch-legend">{{ t('home.patch.upstreamHint') }}</p>

              <p class="patch-foot">
                <span class="patch-status" :class="'is-' + lineState">{{ statusText }}</span>
                {{ t('home.patch.footnote') }}
              </p>
            </figure>
          </div>
        </div>
      </section>

      <!-- 环节说明：窄屏纵向读数条目，宽屏排成读数册 -->
      <section class="mx-auto max-w-6xl px-gutter pb-12">
        <div class="title-block">
          <h2 class="title-block-title">{{ t('home.spec.title') }}</h2>
          <p class="title-block-meta">{{ t('home.spec.meta') }}</p>
        </div>
        <ol class="spec-list">
          <li v-for="(row, index) in specRows" :key="row.key" class="spec-item">
            <span class="spec-index">{{ padIndex(index) }}</span>
            <span class="spec-name">{{ t(row.name) }}</span>
            <span class="spec-desc">{{ t(row.desc) }}</span>
          </li>
        </ol>
      </section>

      <!-- 上游端口 -->
      <section class="mx-auto max-w-6xl px-gutter pb-12">
        <div class="title-block">
          <h2 class="title-block-title">{{ t('home.providers.title') }}</h2>
          <p class="title-block-meta">{{ t('home.providers.description') }}</p>
        </div>
        <ul class="flex flex-wrap gap-2">
          <li
            v-for="provider in providers"
            :key="provider.key"
            class="port"
            :class="provider.supported ? '' : 'port-idle'"
          >
            <span class="port-mark" aria-hidden="true">{{ provider.mark }}</span>
            <span class="port-name">{{ t(provider.label) }}</span>
            <span class="badge" :class="provider.supported ? 'badge-success' : 'badge-gray'">
              {{ provider.supported ? t('home.providers.supported') : t('home.providers.soon') }}
            </span>
          </li>
        </ul>
      </section>

      <!-- 收口：一句引文 + 一个动作 -->
      <section class="mx-auto max-w-6xl px-gutter pb-16">
        <div class="panel flex flex-col gap-4 p-4 sm:flex-row sm:items-center sm:justify-between sm:gap-6 sm:p-5">
          <div class="min-w-0">
            <p class="pull-quote">{{ t('home.cta.title') }}</p>
            <p class="mt-2 max-w-measure text-sm leading-[1.65] text-ink-soft [overflow-wrap:anywhere]">
              {{ t('home.cta.description') }}
            </p>
          </div>
          <router-link
            :to="isAuthenticated ? dashboardPath : '/register'"
            class="btn btn-primary shrink-0"
          >
            {{ isAuthenticated ? t('home.goToDashboard') : t('home.cta.button') }}
          </router-link>
        </div>
      </section>
    </main>

    <footer class="border-t border-rule">
      <div class="mx-auto flex max-w-6xl flex-col gap-2 px-gutter py-4 text-xs text-ink-faint sm:flex-row sm:items-center sm:justify-between">
        <p class="[overflow-wrap:anywhere]">&copy; {{ currentYear }} {{ siteName }}</p>
        <div class="flex flex-wrap items-center gap-x-4">
          <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="home-foot-link hover:text-ink">
            {{ t('home.docs') }}
          </a>
          <a :href="githubUrl" target="_blank" rel="noopener noreferrer" class="home-foot-link hover:text-ink">GitHub</a>
        </div>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import { sanitizeUrl } from '@/utils/url'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import { isDarkTheme, toggleTheme as toggleThemeState, initTheme } from '@/composables/useTheme'

const { t } = useI18n()

const authStore = useAuthStore()
const appStore = useAppStore()

// 站点设置直取 appStore（已由注入的 __APP_CONFIG__ 初始化）
const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'AI API Gateway Platform')
const docUrl = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || ''))
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')
const hasHomeContent = computed(() => homeContent.value.trim().length > 0)
const compactHomeEnabled = computed(() => appStore.cachedPublicSettings?.compact_home_enabled === true)
const modelPlazaEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.modelPlaza))

// home_content 是 URL 时走 iframe
const isHomeContentUrl = computed(() => {
  const content = homeContent.value.trim()
  return content.startsWith('http://') || content.startsWith('https://')
})

// 主题：唯一入口是 useTheme，避免 theme-color 与 documentElement 不同步
const isDark = isDarkTheme
function toggleTheme() {
  toggleThemeState()
}

// GitHub 地址
const githubUrl = 'https://github.com/Wei-Shaw/sub2api'

// 登录态
const isAuthenticated = computed(() => authStore.isAuthenticated)
const modelPlazaRequiresAuth = computed(
  () => appStore.cachedPublicSettings?.model_plaza_require_auth === true,
)
const showModelPlazaEntry = computed(
  () => modelPlazaEnabled.value && (isAuthenticated.value || !modelPlazaRequiresAuth.value),
)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => isAdmin.value ? '/admin/dashboard' : '/dashboard')

const dash = '—'

/** 图纸状态：live = 通电（信号色）/ warn = 需要处理（赭石）/ broken = 断线（氧化红）/ idle = 未接线（灰） */
type LineState = 'live' | 'warn' | 'broken' | 'idle'

const apiBaseUrl = computed(
  () => appStore.cachedPublicSettings?.api_base_url || appStore.apiBaseUrl || '',
)
const siteVersion = computed(
  () => appStore.cachedPublicSettings?.version || appStore.siteVersion || '',
)

// 全部读数来自 authStore.user（后端 /user/me 与 /auth/login 的返回值），没有就地编造的数字
const userBalance = computed(() => {
  const value = authStore.user?.balance
  return typeof value === 'number' ? value : null
})
const userConcurrency = computed(() => {
  const value = authStore.user?.concurrency
  return typeof value === 'number' && value > 0 ? value : null
})
const userRpmLimit = computed(() => {
  const value = authStore.user?.rpm_limit
  return typeof value === 'number' && value > 0 ? value : null
})
const userDisabled = computed(() => authStore.user?.status === 'disabled')

/** 整条线路的状态：未登录 = 未接线；账号停用 = 断线 */
const lineState = computed<LineState>(() => {
  if (!isAuthenticated.value) return 'idle'
  if (userDisabled.value) return 'broken'
  return 'live'
})

/** 判定节点：并发上限是真实读数；账号停用即断线 */
const gateState = computed<LineState>(() => lineState.value)
const gateReadout = computed(() =>
  userConcurrency.value == null ? dash : '×' + userConcurrency.value,
)

/** 转发节点：用户级 RPM 上限是真实读数；未配置上限表示不额外限流 */
const routeState = computed<LineState>(() => {
  if (lineState.value === 'idle') return 'idle'
  if (lineState.value === 'broken') return 'broken'
  return userRpmLimit.value == null ? 'warn' : 'live'
})
const routeReadout = computed(() =>
  userRpmLimit.value == null ? t('home.patch.noRpmCap') : 'RPM ' + userRpmLimit.value,
)

/** 落账节点：余额是真实读数；零余额意味着下一次调用会被拒 */
const ledgerState = computed<LineState>(() => {
  if (lineState.value === 'idle') return 'idle'
  if (lineState.value === 'broken') return 'broken'
  return userBalance.value != null && userBalance.value > 0 ? 'live' : 'warn'
})
const ledgerReadout = computed(() =>
  userBalance.value == null ? dash : formatMoney(userBalance.value),
)

const failoverState = computed<LineState>(() =>
  lineState.value === 'live' ? 'live' : lineState.value === 'idle' ? 'idle' : 'broken',
)

const stages = computed(() => [
  { key: 'gate', label: 'home.patch.gate', hint: 'home.patch.gateHint', state: gateState.value, readout: gateReadout.value },
  { key: 'route', label: 'home.patch.route', hint: 'home.patch.routeHint', state: routeState.value, readout: routeReadout.value },
  { key: 'ledger', label: 'home.patch.ledger', hint: 'home.patch.ledgerHint', state: ledgerState.value, readout: ledgerReadout.value },
])

const statusText = computed(() => {
  if (!isAuthenticated.value) return t('home.patch.statusSignedOut')
  if (userDisabled.value) return t('home.patch.statusDisabled')
  if (userBalance.value === 0) return t('home.patch.statusNoBalance')
  return t('home.patch.statusLive')
})

// 请求行：图纸抬头标注这次调用长什么样
const requestLine = computed(() => 'POST /v1/messages')

// 上游端口：站点实际接入的四家（名称来自文案，读数在节点上）
const upstreamPorts = [
  { key: 'claude', label: 'home.providers.claude' },
  { key: 'gpt', label: 'home.providers.gpt' },
  { key: 'gemini', label: 'home.providers.gemini' },
  { key: 'antigravity', label: 'home.providers.antigravity' },
]

const specRows = [
  { key: 'gateway', name: 'home.features.unifiedGateway', desc: 'home.features.unifiedGatewayDesc' },
  { key: 'accounts', name: 'home.features.multiAccount', desc: 'home.features.multiAccountDesc' },
  { key: 'billing', name: 'home.features.balanceQuota', desc: 'home.features.balanceQuotaDesc' },
]

const providers = [
  { key: 'claude', mark: 'C', label: 'home.providers.claude', supported: true },
  { key: 'gpt', mark: 'G', label: 'home.providers.gpt', supported: true },
  { key: 'gemini', mark: 'G', label: 'home.providers.gemini', supported: true },
  { key: 'antigravity', mark: 'A', label: 'home.providers.antigravity', supported: true },
  { key: 'more', mark: '+', label: 'home.providers.more', supported: false },
]

const currentYear = computed(() => new Date().getFullYear())

/** 金额读数：等宽、两位小数；不引 locale 货币符号，避免图纸上出现两种排版 */
function formatMoney(value: number): string {
  return value.toFixed(2)
}

/** 序号补零：读数册的列要个位对齐 */
function padIndex(index: number): string {
  return String(index + 1).padStart(2, '0')
}

onMounted(() => {
  initTheme()

  // 检查登录状态
  authStore.checkAuth()

  // 保证公共设置已加载（注入配置已存在时直接用缓存）
  if (!appStore.publicSettingsLoaded) {
   appStore.fetchPublicSettings()
  }
})
</script>

<style scoped>
/* v2「暖纸上的编辑体」：结构不变，气质换暖。
 * 衬线同屏最多两处 —— 品牌名 + 落账大读数。 */
.brand-name {
  font-family: var(--font-display);
  font-size: 1rem;
  line-height: 1.25;
  letter-spacing: var(--track-display);
  color: rgb(var(--ink));
}

/* ============================================================================
 * 配线图：宽屏横排（端口列 → 竖总线 → 节点列），窄屏纵排。
 * 同一张图纸只改编排；走线一律 1px 痕量线，通电换信号色，断线换氧化红。
 * ========================================================================== */

.patch-body {
  display: grid;
  gap: 0.75rem;
}

.patch-col {
  display: grid;
  gap: 0.375rem;
  min-width: 0;
}

.patch-col-ports {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.patch-port {
  position: relative;
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.375rem;
  min-height: 1.75rem;
  padding: 0 0.375rem;
  border: 1px solid rgb(var(--rule));
  border-radius: var(--radius-cut);
  background-color: rgb(var(--paper-inset) / 0.5);
}

.patch-port-dot {
  width: 5px;
  height: 5px;
  flex: none;
  border-radius: 999px;
  background-color: rgb(var(--ink-faint));
}

.patch-port-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--font-read);
  font-size: 0.6875rem;
  font-variant-numeric: tabular-nums;
  color: rgb(var(--ink-soft));
}

.patch-port.is-live {
  border-color: rgb(var(--signal) / 0.45);
}
.patch-port.is-live .patch-port-dot {
  background-color: rgb(var(--signal));
}
.patch-port.is-broken {
  border-color: rgb(var(--c-oxide-400));
}
.patch-port.is-broken .patch-port-dot {
  background-color: rgb(var(--c-oxide-500));
}
.patch-port.is-warn .patch-port-dot {
  background-color: rgb(var(--c-ochre-500));
}
.patch-port.is-idle {
  border-style: dashed;
  border-color: rgb(var(--rule-strong));
}
.patch-port.is-idle .patch-port-name {
  color: rgb(var(--ink-faint));
}

/* 节点列：左侧一根竖导轨把三段串起来，这就是走线 */
.patch-col-nodes {
  position: relative;
  padding-left: 1.25rem;
  gap: 0.5rem;
}

.patch-col-nodes::before {
  content: '';
  position: absolute;
  left: 0.3125rem;
  top: 0.75rem;
  bottom: 0.75rem;
  width: 1px;
  background-color: rgb(var(--rule-strong));
}

.patch-col-nodes.is-live::before {
  background-color: rgb(var(--signal) / 0.55);
}
.patch-col-nodes.is-warn::before {
  background-color: rgb(var(--c-ochre-500));
}
.patch-col-nodes.is-broken::before {
  background-color: rgb(var(--c-oxide-500));
}

/* 每个节点向左伸出一根短线接到导轨上 */
.patch-node {
  position: relative;
  min-width: 0;
  padding: 0.4375rem 0.5625rem;
  border: 1px solid rgb(var(--rule));
  border-left: 2px solid rgb(var(--rule-strong));
  border-radius: var(--radius-cut);
  background-color: rgb(var(--surface));
}

.patch-node::before {
  content: '';
  position: absolute;
  left: -0.9375rem;
  top: 0.9375rem;
  width: 0.9375rem;
  height: 1px;
  background-color: rgb(var(--rule-strong));
}

.patch-node.is-live {
  border-left-color: rgb(var(--signal));
}
.patch-node.is-live::before {
  background-color: rgb(var(--signal) / 0.55);
}
.patch-node.is-warn {
  border-left-color: rgb(var(--c-ochre-500));
}
.patch-node.is-warn::before {
  background-color: rgb(var(--c-ochre-500));
}
.patch-node.is-broken {
  border-color: rgb(var(--c-oxide-400) / 0.6);
  border-left-color: rgb(var(--c-oxide-500));
}
.patch-node.is-broken::before {
  background-color: rgb(var(--c-oxide-500));
}
.patch-node.is-idle {
  border-style: dashed;
  border-left-style: solid;
  border-left-color: rgb(var(--rule-strong));
  background-color: transparent;
}

.patch-node-head {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 0 0.5rem;
}

.patch-node-name {
  font-size: 0.8125rem;
  font-weight: 500;
  color: rgb(var(--ink));
}

.patch-node-read-display {
  font-size: clamp(1.25rem, 1.05rem + 0.8vw, 1.625rem);
}

.patch-node-read {
  font-family: var(--font-read);
  font-size: 0.6875rem;
  font-variant-numeric: tabular-nums;
  color: rgb(var(--ink-faint));
}

.patch-node.is-live .patch-node-read {
  color: rgb(var(--signal));
}
.patch-node.is-warn .patch-node-read {
  color: rgb(var(--c-ochre-700));
}
.patch-node.is-broken .patch-node-read {
  color: rgb(var(--c-oxide-600));
}

.patch-note {
  margin-top: 0.125rem;
  max-width: 46ch;
  font-size: 0.6875rem;
  line-height: 1.45;
  color: rgb(var(--ink-faint));
  overflow-wrap: anywhere;
}

.patch-note-inline {
  margin-top: 0;
}

/* 备用线路：虚线，向导轨末端再接一路 */
.patch-failover {
  position: relative;
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 0 0.375rem;
  padding: 0.25rem 0 0.25rem 0.5625rem;
  border-left: 2px dashed rgb(var(--rule-strong));
}

.patch-failover::before {
  content: '';
  position: absolute;
  left: -0.9375rem;
  top: 0.8125rem;
  width: 0.9375rem;
  height: 0;
  border-top: 1px dashed rgb(var(--rule-strong));
}

.patch-failover.is-live {
  border-left-color: rgb(var(--signal) / 0.55);
}
.patch-failover.is-live::before {
  border-top-color: rgb(var(--signal) / 0.55);
}
.patch-failover.is-broken {
  border-left-color: rgb(var(--c-oxide-500));
}
.patch-failover.is-broken::before {
  border-top-color: rgb(var(--c-oxide-500));
}

.patch-failover-tag {
  font-size: 0.6875rem;
  color: rgb(var(--ink-soft));
}

/* 环节说明：窄屏纵向读数条目，宽屏同一组数据排成读数册（不靠横向溢出） */
.spec-list {
  border-top: 1px solid rgb(var(--rule));
}

.spec-item {
  display: grid;
  grid-template-columns: 2.25rem minmax(0, 1fr);
  column-gap: 0.75rem;
  row-gap: 0.125rem;
  padding: 0.625rem 0;
  border-bottom: 1px solid rgb(var(--rule));
}

.spec-index {
  grid-row: 1 / span 2;
  font-family: var(--font-read);
  font-size: 0.6875rem;
  font-variant-numeric: tabular-nums;
  line-height: 1.5;
  color: rgb(var(--ink-faint));
}

.spec-name {
  font-size: 0.8125rem;
  font-weight: 500;
  color: rgb(var(--ink));
  overflow-wrap: anywhere;
}

.spec-desc {
  font-size: 0.8125rem;
  line-height: 1.5;
  color: rgb(var(--ink-soft));
  overflow-wrap: anywhere;
}

/* 宽屏：序号 · 环节 · 说明三列对齐，读起来像报表 */
@media (min-width: 768px) {
  .spec-item {
   grid-template-columns: 2.25rem 11rem minmax(0, 1fr);
   align-items: baseline;
  }

  .spec-index {
   grid-row: auto;
  }
}

/* 上游说明：窄屏才显示（宽屏由端口列自己说明接法） */
.patch-legend {
  border-top: 1px solid rgb(var(--rule));
  padding: 0.5rem 0.75rem;
  font-size: 0.6875rem;
  color: rgb(var(--ink-faint));
  overflow-wrap: anywhere;
}

/* 图注：状态读数 + 一句说明 */
.patch-foot {
  border-top: 1px solid rgb(var(--rule));
  padding: 0.625rem 0.75rem;
  font-family: var(--font-read);
  font-size: 0.6875rem;
  line-height: 1.5;
  color: rgb(var(--ink-faint));
  overflow-wrap: anywhere;
}

.patch-status {
  margin-right: 0.375rem;
  font-weight: 500;
}
.patch-status.is-live {
  color: rgb(var(--signal));
}
.patch-status.is-warn {
  color: rgb(var(--c-ochre-700));
}
.patch-status.is-broken {
  color: rgb(var(--c-oxide-600));
}
.patch-status.is-idle {
  color: rgb(var(--ink-soft));
}

/* 上游端口插口 */
.port {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-height: 2.25rem;
  padding: 0 0.625rem;
  border: 1px solid rgb(var(--rule-strong));
  border-radius: var(--radius-cut);
  background-color: rgb(var(--surface));
}

.port-idle {
  border-style: dashed;
  background-color: transparent;
}

.port-mark {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 1.25rem;
  height: 1.25rem;
  flex: none;
  border: 1px solid rgb(var(--rule-strong));
  border-radius: var(--radius-cut);
  font-family: var(--font-read);
  font-size: 0.6875rem;
  color: rgb(var(--ink-soft));
}

.port-name {
  font-size: 0.8125rem;
  font-weight: 500;
  color: rgb(var(--ink));
  overflow-wrap: anywhere;
}

/* 触控下限：移动端可点元素 ≥44px（令牌 --touch-min） */
.home-brand {
  min-height: var(--touch-min);
}

.home-foot-link {
  display: inline-flex;
  align-items: center;
  min-height: var(--touch-min);
}

@media (min-width: 1024px) {
  .home-brand,
  .home-foot-link {
   min-height: 0;
  }
}

/* 宽屏：端口列在左，节点列在右；端口各自伸出一根短线接到节点导轨 */
@media (min-width: 640px) {
  .patch-body {
   grid-template-columns: minmax(0, 6.5rem) minmax(0, 1fr);
   column-gap: 1.75rem;
   align-items: start;
  }

  .patch-col-ports {
   grid-template-columns: minmax(0, 1fr);
   gap: 0.5rem;
   padding-top: 0.25rem;
  }

  /* 端口右侧的短线要接到节点列的导轨上：列间距 1.75rem + 导轨内缩 0.3125rem */
  .patch-port::after {
   content: '';
   position: absolute;
   right: -2.0625rem;
   top: 50%;
   width: 2.0625rem;
   height: 1px;
   background-color: rgb(var(--rule-strong));
  }

  .patch-port.is-live::after {
   background-color: rgb(var(--signal) / 0.45);
  }
  .patch-port.is-warn::after {
   background-color: rgb(var(--c-ochre-500));
  }
  .patch-port.is-broken::after {
   background-color: rgb(var(--c-oxide-500));
  }

  .patch-legend {
   display: none;
  }

  .patch-foot {
   padding-inline: 1.25rem;
  }
}
</style>