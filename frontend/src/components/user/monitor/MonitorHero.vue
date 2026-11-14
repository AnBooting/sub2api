<template>
  <section class="py-3 md:py-4">
   <div class="flex items-center justify-end gap-3 flex-wrap">
   <div
   role="tablist"
   class="inline-flex p-0.5 rounded-xl bg-paper-inset border border-rule/60 /60 text-xs"
   >
   <button
   v-for="opt in windowOptions"
   :key="opt.value"
   type="button"
   role="tab"
   :aria-selected="window === opt.value"
   class="px-3 py-1 rounded-lg transition-colors"
   :class="window === opt.value ? 'bg-white text-ink font-semibold' : 'text-ink-soft hover:text-ink dark:hover:text-ink'"
   @click="emit('update:window', opt.value)"
   >
   {{ opt.label }}
   </button>
   </div>

   <span
   class="inline-flex items-center px-2.5 py-1 rounded-full text-xs font-semibold"
   :class="overallChipClass"
   >
   <span
   class="w-1.5 h-1.5 rounded-full mr-1.5"
   :class="overallDotClass"
   ></span>
   {{ overallLabel }}
   </span>

   <button
   type="button"
   class="h-8 w-8 rounded-lg flex items-center justify-center text-ink-soft hover:text-ink hover:bg-paper-inset dark:hover:text-ink dark:hover:bg-paper-inset transition-colors disabled:opacity-50"
   :disabled="loading"
   :title="t('common.refresh')"
   @click="emit('refresh')"
   >
   <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
   </button>

   <AutoRefreshButton
   v-if="autoRefresh"
   :enabled="autoRefresh.enabled.value"
   :interval-seconds="autoRefresh.intervalSeconds.value"
   :countdown="autoRefresh.countdown.value"
   :intervals="autoRefresh.intervals"
   @update:enabled="autoRefresh.setEnabled"
   @update:interval="autoRefresh.setInterval"
   />
   </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import AutoRefreshButton from '@/components/common/AutoRefreshButton.vue'
export type MonitorWindow = '7d' | '15d' | '30d'
export type OverallStatus = 'operational' | 'degraded'

const props = defineProps<{
  overallStatus: OverallStatus
  intervalSeconds: number
  window: MonitorWindow
  loading: boolean
  autoRefresh?: {
   enabled: { value: boolean }
   intervalSeconds: { value: number }
   countdown: { value: number }
   intervals: readonly number[]
   setEnabled: (v: boolean) => void
   setInterval: (v: number) => void
  }
}>()

const emit = defineEmits<{
  (e: 'update:window', value: MonitorWindow): void
  (e: 'refresh'): void
}>()

const { t } = useI18n()

const windowOptions = computed<{ value: MonitorWindow; label: string }[]>(() => [
  { value: '7d', label: t('channelStatus.windowTab.7d') },
  { value: '15d', label: t('channelStatus.windowTab.15d') },
  { value: '30d', label: t('channelStatus.windowTab.30d') },
])

const overallLabel = computed(() => t(`channelStatus.overall.${props.overallStatus}`))

const overallChipClass = computed(() => {
  switch (props.overallStatus) {
   case 'operational':
   return 'bg-success-100 text-success-700 dark:bg-success-500/15 dark:text-success-300'
   case 'degraded':
   default:
   return 'bg-warning-100 text-warning-700 dark:bg-warning-500/15 dark:text-warning-300'
  }
})

const overallDotClass = computed(() => {
  switch (props.overallStatus) {
   case 'operational':
   return 'bg-success-500 animate-pulse'
   case 'degraded':
   default:
   return 'bg-warning-500 animate-pulse'
  }
})

</script>
