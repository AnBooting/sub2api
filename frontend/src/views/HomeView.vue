<template>
  <!-- Custom Home Content: Full Page Mode -->
  <div v-if="hasHomeContent" class="min-h-screen">
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-screen w-full border-0"
      allowfullscreen
    ></iframe>
    <div v-else v-html="homeContent"></div>
  </div>

  <!-- Compact/Default Home: restrained product shell -->
  <div
    v-else
    data-testid="compact-home"
    class="relative flex min-h-screen flex-col overflow-hidden bg-surface-100 text-ink-900 dark:bg-dark-950 dark:text-white"
  >
    <!-- 电影开场背景：三束暖光 + 极淡胶片颗粒，纯装饰 -->
    <div class="cinema-backdrop" aria-hidden="true">
      <div class="cinema-keylight"></div>
      <div class="cinema-rimlight"></div>
      <div class="cinema-floor"></div>
      <div class="cinema-grain"></div>
    </div>
    <div class="motion-activity-line relative z-10 overflow-hidden">
      <div class="h-0.5 w-full origin-left bg-gradient-primary"></div>
    </div>
    <header class="relative z-10 border-b border-borderline-200 bg-surface-50 dark:border-dark-800 dark:bg-dark-900">
      <nav class="mx-auto flex min-h-14 max-w-6xl flex-wrap items-center justify-between gap-3 px-4 py-3 sm:px-6">
        <div class="flex min-w-0 flex-1 items-center gap-2.5">
          <img
            :src="siteLogo || '/logo.svg'"
            alt="Logo"
            class="h-9 w-9 shrink-0 rounded-panel border border-borderline-200 object-contain dark:border-dark-700"
          />
          <div class="min-w-0">
            <div class="truncate text-sm font-semibold">{{ siteName }}</div>
            <div v-if="siteSubtitle" class="truncate text-xs text-ink-500 dark:text-dark-400">{{ siteSubtitle }}</div>
          </div>
        </div>
        <div class="flex max-w-full shrink-0 flex-wrap items-center justify-end gap-1.5">
          <LocaleSwitcher />
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="flex h-9 w-9 shrink-0 items-center justify-center rounded-control text-ink-500 hover:bg-surface-200 hover:text-ink-900 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
            :title="t('home.viewDocs')"
            :aria-label="t('home.viewDocs')"
          >
            <Icon name="book" size="md" />
          </a>
          <router-link
            v-if="showModelPlazaEntry"
            to="/model-plaza"
            class="flex h-9 shrink-0 items-center gap-1.5 rounded-control px-2.5 text-sm font-medium text-ink-600 hover:bg-surface-200 hover:text-ink-900 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
            :title="t('nav.modelPlaza')"
          >
            <Icon name="grid" size="md" />
            <span class="hidden sm:inline">{{ t('nav.modelPlaza') }}</span>
          </router-link>
          <button
            class="flex h-9 w-9 shrink-0 items-center justify-center rounded-control text-ink-500 hover:bg-surface-200 hover:text-ink-900 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            :aria-label="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            @click="toggleTheme"
          >
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>
          <router-link
            :to="isAuthenticated ? dashboardPath : '/login'"
            class="btn btn-primary"
          >
            {{ isAuthenticated ? t('home.dashboard') : t('home.login') }}
          </router-link>
        </div>
      </nav>
    </header>

    <main class="relative z-10 mx-auto w-full max-w-6xl flex-1 px-4 py-10 sm:px-6">
      <div class="grid gap-8 lg:grid-cols-[1fr_360px] lg:items-start">
        <section class="min-w-0">
          <div class="mb-6">
            <!-- 编辑式章节标识：纯视觉分层，不承载文案 -->
            <div class="chapter-mark">
              <span class="chapter-index">01</span>
              <span class="chapter-rule"></span>
              <span class="chapter-label">OPENING</span>
            </div>
            <h1 class="chapter-title text-2xl font-semibold tracking-tight sm:text-3xl">{{ homeHeadline }}</h1>
            <p class="mt-2 max-w-2xl text-sm leading-6 text-ink-600 dark:text-dark-300">
              {{ siteSubtitle }}
            </p>
          </div>

          <div class="motion-stagger grid gap-3 sm:grid-cols-2">
            <!-- 外部链接（如配置的文档站）必须走原生 a[href]，
                 交给 router-link 会被拼成站内相对路径。 -->
            <component
              :is="isExternalEntry(entry.to) ? 'a' : 'router-link'"
              v-for="entry in primaryEntries"
              :key="entry.to"
              v-bind="isExternalEntry(entry.to)
                ? { href: entry.to, target: '_blank', rel: 'noopener noreferrer' }
                : { to: entry.to }"
              class="motion-lift group flex items-start gap-3 rounded-panel border border-borderline-200 bg-white p-4 shadow-card transition-colors hover:border-primary-300 hover:bg-primary-50/30 dark:border-dark-700 dark:bg-dark-800 dark:hover:border-primary-700 dark:hover:bg-primary-950/20"
            >
              <span class="motion-icon flex h-9 w-9 shrink-0 items-center justify-center rounded-control bg-primary-600 text-white">
                <Icon :name="entry.icon" size="md" />
              </span>
              <span class="min-w-0">
                <span class="block text-sm font-semibold">{{ entry.label }}</span>
                <span class="mt-1 block text-xs leading-5 text-ink-500 dark:text-dark-400">
                  {{ entry.description }}
                </span>
              </span>
            </component>
          </div>
        </section>

        <aside class="motion-card rounded-panel border border-borderline-200 bg-white p-5 shadow-card dark:border-dark-700 dark:bg-dark-800">
          <h2 class="panel-title">{{ t('home.quickStartTitle') }}</h2>
          <ol class="mt-4 space-y-4">
            <li v-for="(step, loopIndex) in quickStartSteps" :key="step" class="flex gap-3 text-sm">
              <span class="flex h-6 w-6 shrink-0 items-center justify-center rounded-control bg-surface-200 text-xs font-semibold text-ink-700 dark:bg-dark-700 dark:text-gray-200">
                {{ loopIndex + 1 }}
              </span>
              <span class="min-w-0 text-ink-600 dark:text-dark-300">{{ t('home.quickStart.' + step) }}</span>
            </li>
          </ol>
        </aside>
      </div>
    </main>

    <footer class="relative z-10 border-t border-borderline-200 bg-surface-50 dark:border-dark-800 dark:bg-dark-900">
      <div class="mx-auto flex max-w-6xl flex-col items-center justify-between gap-3 px-4 py-4 text-xs text-ink-500 dark:text-dark-400 sm:flex-row sm:px-6">
        <span>&copy; {{ currentYear }} {{ siteName }}</span>
        <div class="flex items-center gap-4">
          <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer">
            {{ t('home.docs') }}
          </a>
          <a :href="githubUrl" target="_blank" rel="noopener noreferrer">GitHub</a>
        </div>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import { sanitizeUrl } from '@/utils/url'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'

const { t } = useI18n()

const authStore = useAuthStore()
const appStore = useAppStore()

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || '')
const docUrl = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || ''))
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')
const hasHomeContent = computed(() => homeContent.value.trim().length > 0)
const modelPlazaEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.modelPlaza))
const isHomeContentUrl = computed(() => {
  const content = homeContent.value.trim()
  return content.startsWith('http://') || content.startsWith('https://')
})

const isDark = ref(document.documentElement.classList.contains('dark'))
const githubUrl = 'https://github.com/Wei-Shaw/sub2api'

const isAuthenticated = computed(() => authStore.isAuthenticated)
const modelPlazaRequiresAuth = computed(() => appStore.cachedPublicSettings?.model_plaza_require_auth === true)
const showModelPlazaEntry = computed(() => modelPlazaEnabled.value && (isAuthenticated.value || !modelPlazaRequiresAuth.value))
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => isAdmin.value ? '/admin/dashboard' : '/dashboard')
const currentYear = computed(() => new Date().getFullYear())

const homeHeadline = computed(() => {
  if (isAuthenticated.value && isAdmin.value) return t('home.adminHeadline')
  if (isAuthenticated.value) return t('home.userHeadline')
  return siteName.value
})

type EntryIcon = 'key' | 'chart' | 'grid' | 'book' | 'login' | 'userPlus' | 'users'

const primaryEntries = computed(() => {
  const entries: Array<{ to: string; icon: EntryIcon; label: string; description: string }> = []
  if (isAuthenticated.value && isAdmin.value) {
    entries.push(
      { to: '/admin/dashboard', icon: 'chart', label: t('home.adminDashboard'), description: t('home.adminDashboardDesc') },
      { to: '/admin/users', icon: 'users', label: t('home.adminUsers'), description: t('home.adminUsersDesc') },
      { to: '/admin/channels/pricing', icon: 'grid', label: t('home.adminChannels'), description: t('home.adminChannelsDesc') },
      { to: '/keys', icon: 'key', label: t('home.personalKeys'), description: t('home.personalKeysDesc') }
    )
  } else if (isAuthenticated.value) {
    entries.push(
      { to: '/keys', icon: 'key', label: t('home.openConsole'), description: t('home.openConsoleDesc') },
      { to: '/usage', icon: 'chart', label: t('nav.usage'), description: t('home.usageDesc') },
      { to: '/available-channels', icon: 'grid', label: t('nav.availableChannels'), description: t('home.channelsDesc') },
      ...(showModelPlazaEntry.value ? [{ to: '/model-plaza', icon: 'grid' as const, label: t('nav.modelPlaza'), description: t('home.modelPlazaDesc') }] : [])
    )
  } else {
    entries.push(
      { to: '/login', icon: 'login', label: t('home.login'), description: t('home.loginDesc') },
      { to: '/register', icon: 'userPlus', label: t('home.register'), description: t('home.registerDesc') },
      ...(showModelPlazaEntry.value ? [{ to: '/model-plaza', icon: 'grid' as const, label: t('nav.modelPlaza'), description: t('home.modelPlazaDesc') }] : []),
      ...(docUrl.value ? [{ to: docUrl.value, icon: 'book' as const, label: t('home.docs'), description: t('home.docsDesc') }] : [])
    )
  }
  return entries
})

/** 外部地址（http/https/协议相对）需要原生 a[href]，不能交给 router-link。 */
const isExternalEntry = (to: string): boolean =>
  /^(?:https?:)?\/\//i.test(to) || to.startsWith('mailto:') || to.startsWith('tel:')

const quickStartSteps = computed(() => {
  if (isAuthenticated.value && isAdmin.value) return ['adminDashboard', 'adminUsers', 'adminChannels', 'adminUsage']
  if (isAuthenticated.value) return ['createKey', 'viewUsage', 'channelStatus']
  return ['login', 'register', 'modelPlaza']
})

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

function initTheme() {
  const savedTheme = localStorage.getItem('theme')
  if (savedTheme === 'dark' || (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)) {
    isDark.value = true
    document.documentElement.classList.add('dark')
  }
}

onMounted(() => {
  initTheme()
  authStore.checkAuth()
  if (!appStore.publicSettingsLoaded) {
    appStore.fetchPublicSettings()
  }
})
</script>
