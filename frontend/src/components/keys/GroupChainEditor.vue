<template>
  <fieldset :disabled="disabled" class="space-y-3">
    <legend class="input-label">{{ t('keys.chain.title') }}</legend>
    <p class="input-hint">{{ t('keys.chain.hint') }}</p>
    <ol class="space-y-2" :aria-label="t('keys.chain.title')">
      <li v-for="(id, index) in modelValue" :key="id" class="flex flex-wrap items-center gap-2 rounded-lg border border-gray-200 p-2 dark:border-dark-600" :data-group-id="id">
        <span class="w-5 text-center text-sm text-gray-500">{{ index + 1 }}</span>
        <div class="min-w-0 flex-1">
          <template v-if="groupById(id)">
            <GroupBadge v-bind="badgeProps(groupById(id)!)" />
            <span v-if="groupById(id)?.status === 'inactive'" class="text-xs text-gray-500">{{ t('keys.chain.unavailable', { id }) }}</span>
          </template>
          <span v-else class="text-sm text-gray-500">{{ t('keys.chain.unavailable', { id }) }}</span>
        </div>
        <div class="flex items-center gap-1">
          <button type="button" class="btn btn-secondary px-2 py-1" :disabled="disabled || index === 0" :aria-label="t('keys.chain.moveUp', { name: groupById(id)?.name ?? id })" data-action="up" @click="move(index, -1)">↑</button>
          <button type="button" class="btn btn-secondary px-2 py-1" :disabled="disabled || index === modelValue.length - 1" :aria-label="t('keys.chain.moveDown', { name: groupById(id)?.name ?? id })" data-action="down" @click="move(index, 1)">↓</button>
          <button type="button" class="btn btn-secondary px-2 py-1" :disabled="disabled" :aria-label="t('keys.chain.remove', { name: groupById(id)?.name ?? id })" data-action="remove" @click="remove(id)">×</button>
        </div>
      </li>
    </ol>
    <p v-if="!modelValue.length" class="input-hint" role="status">{{ t('keys.chain.empty') }}</p>
    <label class="block">
      <span class="input-label">{{ t('keys.chain.add') }}</span>
      <select class="input" :value="''" :disabled="disabled || !availableGroups.length" :aria-label="t('keys.chain.add')" data-test="chain-add" @change="add($event)">
        <option value="">{{ t('keys.selectGroup') }}</option>
        <option v-for="group in availableGroups" :key="group.id" :value="group.id">{{ group.name }} · {{ userRates[group.id] ?? group.rate_multiplier }}x</option>
      </select>
    </label>
    <p v-if="!availableGroups.length && !modelValue.length" class="input-hint">{{ t('common.noGroupsAvailable') }}</p>
    <p class="sr-only" aria-live="polite">{{ modelValue.map((id) => groupById(id)?.name ?? id).join(' → ') }}</p>
  </fieldset>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Group } from '@/types'
import GroupBadge from '@/components/common/GroupBadge.vue'
const props = withDefaults(defineProps<{ modelValue: number[]; groups: Group[]; userRates?: Record<number, number>; disabled?: boolean }>(), { userRates: () => ({}), disabled: false })
const emit = defineEmits<{ 'update:modelValue': [ids: number[]] }>()
const { t } = useI18n()
const availableGroups = computed(() => props.groups.filter((g) => g.status !== 'inactive' && !props.modelValue.includes(g.id)))
const groupById = (id: number) => props.groups.find((g) => g.id === id)
const badgeProps = (group: Group) => ({ name: group.name, platform: group.platform, subscriptionType: group.subscription_type, rateMultiplier: group.rate_multiplier, userRateMultiplier: props.userRates[group.id], peakRateEnabled: group.peak_rate_enabled, peakStart: group.peak_start, peakEnd: group.peak_end, peakRateMultiplier: group.peak_rate_multiplier })
const add = (event: Event) => {
  const input = event.target as HTMLSelectElement
  const id = Number(input.value)
  if (!props.disabled && availableGroups.value.some((g) => g.id === id)) emit('update:modelValue', [...props.modelValue, id])
  input.value = ''
}
const remove = (id: number) => { if (!props.disabled) emit('update:modelValue', props.modelValue.filter((value) => value !== id)) }
const move = (index: number, delta: number) => {
  const target = index + delta
  if (props.disabled || target < 0 || target >= props.modelValue.length) return
  const ids = [...props.modelValue]
  ;[ids[index], ids[target]] = [ids[target], ids[index]]
  emit('update:modelValue', ids)
}
</script>
