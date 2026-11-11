<template>
  <div class="min-h-screen bg-paper text-ink">
    <!-- 侧栏（桌面为固定导轨，移动端为抽屉） -->
    <AppSidebar />

    <!-- 主区：与侧栏同一条痕量线分隔 -->
    <div
      class="relative min-h-screen transition-[margin] duration-200 ease-cut"
      :class="[sidebarCollapsed ? 'lg:ml-[76px]' : 'lg:ml-60']"
    >
      <AppHeader />

      <!-- 内容区：底部为移动端工具坞留出让位 -->
      <main class="px-3 py-4 pb-dock sm:px-5 sm:py-5 lg:px-7 lg:py-6">
        <slot />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import '@/styles/onboarding.css'
import { computed, onMounted } from 'vue'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { useOnboardingTour } from '@/composables/useOnboardingTour'
import { useOnboardingStore } from '@/stores/onboarding'
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'

const appStore = useAppStore()
const authStore = useAuthStore()
const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
const isAdmin = computed(() => authStore.user?.role === 'admin')

const { replayTour } = useOnboardingTour({
  storageKey: isAdmin.value ? 'admin_guide' : 'user_guide',
  autoStart: true
})

const onboardingStore = useOnboardingStore()

onMounted(() => {
  onboardingStore.setReplayCallback(replayTour)
})

defineExpose({ replayTour })
</script>
