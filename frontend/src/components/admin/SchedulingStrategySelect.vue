<template>
  <div>
    <label class="block">
      <span class="input-label">{{ t('admin.settings.scheduling.strategy') }}</span>
      <select class="input" :value="modelValue" :disabled="disabled" :aria-label="t('admin.settings.scheduling.strategy')" data-test="scheduling-strategy" @change="change">
        <option v-if="allowInherit" value="inherit">{{ t('admin.settings.scheduling.strategies.inherit') }}</option>
        <option value="stable">{{ t('admin.settings.scheduling.strategies.stable') }}</option>
        <option value="balanced">{{ t('admin.settings.scheduling.strategies.balanced') }}</option>
        <option value="latency">{{ t('admin.settings.scheduling.strategies.latency') }}</option>
      </select>
    </label>
    <p class="input-hint">{{ t('admin.settings.scheduling.strategyHint') }}</p>
    <p v-if="effectiveStrategy" class="input-hint" data-test="effective-strategy">{{ t('admin.settings.scheduling.effectiveStrategy') }}: {{ t('admin.settings.scheduling.strategies.' + effectiveStrategy) }}</p>
  </div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { GroupSchedulingStrategy, SchedulingStrategy } from '@/types'
const props = defineProps<{ modelValue: GroupSchedulingStrategy; allowInherit?: boolean; disabled?: boolean; effectiveStrategy?: SchedulingStrategy }>()
const emit = defineEmits<{ 'update:modelValue': [strategy: GroupSchedulingStrategy] }>()
const { t } = useI18n()
const change = (event: Event) => {
  const value = (event.target as HTMLSelectElement).value
  if (!props.disabled && (value === 'stable' || value === 'balanced' || value === 'latency' || (props.allowInherit && value === 'inherit'))) emit('update:modelValue', value)
}
</script>
