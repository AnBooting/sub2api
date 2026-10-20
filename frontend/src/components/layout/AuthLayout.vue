<template>
  <div class="relative flex min-h-screen items-center justify-center overflow-hidden p-4">
    <!-- Warm cinematic backdrop -->
    <div
      class="absolute inset-0 bg-gradient-to-br from-[#faf6f0] via-[#f6e9d8] to-[#efe1d1] dark:from-[#110d0a] dark:via-[#1f160e] dark:to-[#0c0907]"
    ></div>

    <!-- Cinematic opening scene + editorial index marks -->
    <div class="pointer-events-none absolute inset-0 overflow-hidden" aria-hidden="true">
      <div class="cinema-keylight"></div>
      <div class="cinema-rimlight"></div>
      <div class="cinema-floor"></div>
      <div class="cinema-index cinema-index-left">01 / OPENING SCENE</div>
      <div class="cinema-index cinema-index-right">02 / ENTER</div>
      <div class="cinema-frame-line"></div>
    </div>

    <!-- Content Container -->
    <div class="relative z-10 w-full max-w-md">
      <!-- Logo/Brand -->
      <div class="mb-8 text-center">
        <!-- Custom Logo or Default Logo -->
        <template v-if="settingsLoaded">
          <div
            class="mb-4 inline-flex h-16 w-16 items-center justify-center overflow-hidden rounded-2xl shadow-lg shadow-primary-500/30"
          >
            <img :src="siteLogo || '/logo.svg'" alt="Logo" class="h-full w-full object-contain" />
          </div>
          <h1 class="text-gradient mb-2 text-3xl font-bold">
            {{ siteName }}
          </h1>
          <p class="text-sm text-gray-500 dark:text-dark-400">
            {{ siteSubtitle }}
          </p>
        </template>
      </div>

      <!-- Editorial chapter mark: 片头章节标识，位于卡片之上而非之内 -->
      <div class="chapter-mark">
        <span class="chapter-index">01</span>
        <span class="chapter-rule"></span>
        <span class="chapter-label">OPENING</span>
      </div>

      <!-- Card Container -->
      <div class="cinema-card rounded-2xl p-8 shadow-glass">
        <slot />
      </div>

      <!-- Footer Links -->
      <div class="mt-6 text-center text-sm">
        <slot name="footer" />
      </div>

      <!-- Copyright -->
      <div class="mt-8 text-center text-xs text-gray-400 dark:text-dark-500">
        &copy; {{ currentYear }} {{ siteName }}. All rights reserved.
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
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'Subscription to API Conversion Platform')
const settingsLoaded = computed(() => appStore.publicSettingsLoaded)

const currentYear = computed(() => new Date().getFullYear())

onMounted(() => {
  appStore.fetchPublicSettings()
})
</script>


<style scoped>
.text-gradient {
  @apply bg-gradient-to-r from-primary-600 to-primary-500 bg-clip-text text-transparent;
}

/* ============================================================================
   Cinematic opening + editorial layout
   ============================================================================
   "电影开场"不等于做旧：用大光区、深阴影和低机位纵深感构建片头；
   "编辑感"来自角落序号、细线、克制字体与留白。
   ============================================================================ */
.cinema-keylight {
  position: absolute;
  left: -10%;
  top: -38%;
  width: 120%;
  height: 62%;
  background:
    radial-gradient(ellipse 68% 56% at 52% 48%, rgba(201, 118, 47, 0.20), transparent 70%),
    radial-gradient(ellipse 52% 44% at 38% 34%, rgba(236, 186, 132, 0.14), transparent 68%);
  filter: blur(10px);
  transform: rotate(-2deg);
}

.cinema-rimlight {
  position: absolute;
  right: -18%;
  top: 4%;
  width: 48%;
  height: 76%;
  background: linear-gradient(175deg, rgba(236, 186, 132, 0.12), transparent 58%),
    radial-gradient(ellipse 52% 46% at 72% 42%, rgba(201, 118, 47, 0.10), transparent 72%);
  filter: blur(12px);
  transform: skewX(-7deg);
}

.cinema-floor {
  position: absolute;
  left: 6%;
  bottom: -24%;
  width: 88%;
  height: 46%;
  background: radial-gradient(ellipse 58% 48% at 50% 36%, rgba(168, 92, 34, 0.14), transparent 74%);
  filter: blur(18px);
}

.cinema-index {
  position: absolute;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 10px;
  letter-spacing: 0.22em;
  color: rgba(112, 78, 45, 0.55);
  /* vertical-rl 本身即从上到下；不要叠加 rotate(180deg)，否则字形会倒置。 */
  writing-mode: vertical-rl;
  text-orientation: mixed;
}

.cinema-index-left {
  left: 28px;
  top: 96px;
  opacity: 0.9;
}

.cinema-index-right {
  right: 28px;
  bottom: 96px;
  opacity: 0.9;
}

.dark .cinema-index {
  color: rgba(214, 170, 112, 0.5);
}

.cinema-frame-line {
  position: absolute;
  left: 50%;
  top: 8vh;
  bottom: 8vh;
  width: 1px;
  background: linear-gradient(180deg, transparent, rgba(168, 92, 34, 0.16), transparent);
}

.cinema-card {
  position: relative;
  border-radius: 18px;
  background:
    linear-gradient(160deg, rgba(255, 252, 247, 0.90), rgba(255, 246, 232, 0.82));
  border: 1px solid rgba(168, 92, 34, 0.16);
  box-shadow:
    0 36px 90px -36px rgba(55, 28, 10, 0.48),
    0 4px 20px -12px rgba(55, 28, 10, 0.20),
    inset 0 1px 0 rgba(255, 255, 255, 0.8);
}

.dark .cinema-card {
  background:
    linear-gradient(160deg, rgba(34, 23, 13, 0.90), rgba(24, 16, 9, 0.86));
  border-color: rgba(190, 135, 75, 0.20);
  box-shadow:
    0 40px 100px -40px rgba(0, 0, 0, 0.68),
    0 6px 26px -14px rgba(0, 0, 0, 0.55),
    inset 0 1px 0 rgba(255, 220, 180, 0.08);
}

.cinema-card::before {
  content: "";
  position: absolute;
  left: 22px;
  right: 22px;
  top: 0;
  height: 2px;
  background: linear-gradient(90deg, rgba(201, 118, 47, 0.68), rgba(236, 186, 132, 0.18));
  opacity: 0.7;
}

/* 章节标识：片头感的来源。放在卡片之上，形成"片名 → 章节 → 内容"的阅读层次。 */
.chapter-mark {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 14px;
  padding-left: 2px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11px;
  letter-spacing: 0.24em;
  color: rgba(168, 92, 34, 0.78);
}

.dark .chapter-mark {
  color: rgba(214, 170, 112, 0.72);
}

.chapter-index {
  font-weight: 600;
}

.chapter-rule {
  display: inline-block;
  width: 42px;
  height: 1px;
  background: linear-gradient(90deg, rgba(201, 118, 47, 0.7), rgba(201, 118, 47, 0.1));
}

.chapter-label {
  letter-spacing: 0.3em;
  opacity: 0.85;
}
</style>
