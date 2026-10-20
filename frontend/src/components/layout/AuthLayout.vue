<template>
  <div class="motion-page relative flex min-h-screen items-center justify-center overflow-hidden bg-surface-100 p-4 dark:bg-dark-950">
    <!-- 电影开场背景：三束暖光 + 极淡胶片颗粒，纯装饰 -->
    <div class="cinema-backdrop" aria-hidden="true">
      <div class="cinema-keylight"></div>
      <div class="cinema-rimlight"></div>
      <div class="cinema-floor"></div>
      <div class="cinema-grain"></div>
    </div>
    <!-- Content Container -->
    <div class="relative z-10 w-full max-w-md">
      <!-- Logo/Brand -->
      <div class="mb-6 text-center">
        <template v-if="settingsLoaded">
          <div class="mb-3 inline-flex h-12 w-12 items-center justify-center overflow-hidden rounded-panel border border-borderline-200 dark:border-dark-700">
            <img :src="siteLogo || '/logo.svg'" alt="Logo" class="h-full w-full object-contain" />
          </div>
          <h1 class="text-xl font-semibold tracking-tight text-ink-900 dark:text-white">
            {{ siteName }}
          </h1>
          <p v-if="siteSubtitle" class="mt-1 text-sm text-ink-500 dark:text-dark-400">
            {{ siteSubtitle }}
          </p>
        </template>
      </div>

      <!-- 编辑式章节标识：纯视觉分层，不承载文案 -->
      <div class="chapter-mark">
        <span class="chapter-index">01</span>
        <span class="chapter-rule"></span>
        <span class="chapter-label">OPENING</span>
      </div>

      <!-- Card Container -->
      <div class="cinema-card p-6 sm:p-7">
        <slot />
      </div>

      <!-- Footer Links -->
      <div class="mt-5 text-center text-sm">
        <slot name="footer" />
      </div>

      <!-- Copyright -->
      <div class="mt-6 text-center text-xs text-ink-400 dark:text-dark-500">
        &copy; {{ currentYear }} {{ siteName }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useAppStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'

const appStore = useAppStore()

const siteName = computed(() => appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || '')
const settingsLoaded = computed(() => appStore.publicSettingsLoaded)

const currentYear = computed(() => new Date().getFullYear())

onMounted(() => {
  appStore.fetchPublicSettings()
})
</script>
