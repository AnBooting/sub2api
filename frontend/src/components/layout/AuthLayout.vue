<template>
  <div class="motion-page relative flex min-h-screen items-center justify-center bg-surface-100 p-4 dark:bg-dark-950">
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

      <!-- Card Container -->
      <div class="rounded-panel border border-borderline-200 bg-white p-6 shadow-card dark:border-dark-700 dark:bg-dark-800 sm:p-7">
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
