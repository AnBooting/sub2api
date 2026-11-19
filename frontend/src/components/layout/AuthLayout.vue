<template>
  <div class="flex min-h-screen flex-col bg-paper text-ink">
    <main class="flex min-w-0 flex-1 items-start justify-center px-gutter py-[clamp(1.5rem,6vh,4.5rem)]">
      <div class="w-full min-w-0 max-w-[26rem]">
        <!-- 品牌区：图签式排版。品牌名用编辑体衬线，这是全页唯一一处衬线 -->
        <div v-if="settingsLoaded" class="mb-5 min-w-0">
          <div class="flex min-w-0 items-center gap-2.5">
            <img
              :src="siteLogo || '/logo.svg'"
              alt=""
              class="h-7 w-7 shrink-0 object-contain"
            />
            <h1 class="brand-name min-w-0 truncate">
              {{ siteName }}
            </h1>
          </div>
          <p
            v-if="siteSubtitle"
            class="mt-1.5 text-[0.8125rem] leading-relaxed text-ink-soft [overflow-wrap:anywhere]"
          >
            {{ siteSubtitle }}
          </p>
        </div>

        <!-- 单一面板：1px 痕量线围出，切角 -->
        <div class="panel min-w-0 p-[clamp(1.125rem,4.5vw,1.75rem)]">
          <slot />
        </div>

        <!-- 页脚链接 -->
        <div class="mt-4 min-w-0 text-center text-[0.8125rem] text-ink-soft">
          <slot name="footer" />
        </div>

        <!-- 版权：可换行 -->
        <p class="mt-6 text-center text-[0.6875rem] leading-relaxed text-ink-faint [overflow-wrap:anywhere]">
          &copy; {{ currentYear }} {{ siteName }}
        </p>
      </div>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useAppStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'

const appStore = useAppStore()

const siteName = computed(() => appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'Subscription to API Conversion Platform')
const settingsLoaded = computed(() => appStore.publicSettingsLoaded)

const currentYear = computed(() => new Date().getFullYear())

onMounted(() => {
  appStore.fetchPublicSettings()
})
</script>

<style scoped>
/* 品牌名：编辑体衬线，不带字重覆盖（衬线由样式层给 500） */
.brand-name {
  font-family: var(--font-display);
  font-size: 1.125rem;
  line-height: 1.2;
  letter-spacing: var(--track-display);
  color: rgb(var(--ink));
}
</style>
