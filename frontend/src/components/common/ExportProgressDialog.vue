<template>
  <BaseDialog :show="show" :title="t('usage.exporting')" width="narrow" @close="handleCancel">
    <div class="space-y-4">
      <p class="text-sm text-ink-soft">
        {{ t('usage.exportingProgress') }}
      </p>
      <div class="flex items-center justify-between font-read text-sm text-ink-soft">
        <span>{{ t('usage.exportedCount', { current, total }) }}</span>
        <span class="font-medium text-ink">{{ normalizedProgress }}%</span>
      </div>
      <div class="progress">
        <div
          role="progressbar"
          :aria-valuenow="normalizedProgress"
          aria-valuemin="0"
          aria-valuemax="100"
          :aria-label="`${t('usage.exportingProgress')}: ${normalizedProgress}%`"
          class="progress-bar"
          :style="{ width: `${normalizedProgress}%` }"
        ></div>
      </div>
      <p v-if="estimatedTime" class="font-read text-[0.6875rem] text-ink-faint" aria-live="polite" aria-atomic="true">
        {{ t('usage.estimatedTime', { time: estimatedTime }) }}
      </p>
    </div>

    <template #footer>
      <button type="button" class="btn btn-secondary" @click="handleCancel">
        {{ t('usage.cancelExport') }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from './BaseDialog.vue'

interface Props {
  show: boolean
  progress: number
  current: number
  total: number
  estimatedTime: string
}

interface Emits {
  (e: 'cancel'): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()
const { t } = useI18n()

const normalizedProgress = computed(() => {
  const value = Number.isFinite(props.progress) ? props.progress : 0
  return Math.min(100, Math.max(0, Math.round(value)))
})

const handleCancel = () => {
  emit('cancel')
}
</script>
