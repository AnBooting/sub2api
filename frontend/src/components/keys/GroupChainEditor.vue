<template>
  <fieldset :disabled="disabled" class="space-y-3">
    <legend class="input-label">{{ t('keys.chain.title') }}</legend>
    <p class="input-hint">{{ t('keys.chain.hint') }}</p>
    <VueDraggable
      v-model="localIds"
      tag="ol"
      handle=".drag-handle"
      :animation="150"
      :disabled="disabled"
      class="space-y-2"
      :aria-label="t('keys.chain.title')"
      @end="onDragEnd"
    >
      <li
        v-for="(id, index) in localIds"
        :key="id"
        class="flex flex-wrap items-center gap-2 rounded-lg border border-gray-200 p-2 dark:border-dark-600"
        :data-group-id="id"
      >
        <span
          class="drag-handle flex cursor-grab items-center text-gray-300 hover:text-gray-500 active:cursor-grabbing dark:text-dark-600 dark:hover:text-dark-400"
          :aria-label="t('keys.chain.dragHandle')"
          :title="t('keys.chain.dragHandle')"
          data-action="drag"
        >
          <svg class="h-4 w-4" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
            <path d="M7 2a2 2 0 1 0 0 4 2 2 0 0 0 0-4zM13 2a2 2 0 1 0 0 4 2 2 0 0 0 0-4zM7 8a2 2 0 1 0 0 4 2 2 0 0 0 0-4zM13 8a2 2 0 1 0 0 4 2 2 0 0 0 0-4zM7 14a2 2 0 1 0 0 4 2 2 0 0 0 0-4zM13 14a2 2 0 1 0 0 4 2 2 0 0 0 0-4z" />
          </svg>
        </span>
        <span class="w-5 text-center text-sm text-gray-500">{{ index + 1 }}</span>
        <div class="min-w-0 flex-1">
          <template v-if="groupById(id)">
            <GroupBadge v-bind="badgeProps(groupById(id)!)" />
            <span v-if="groupById(id)?.status === 'inactive'" class="text-xs text-gray-500">{{ t('keys.chain.unavailable', { id }) }}</span>
          </template>
          <span v-else class="text-sm text-gray-500">{{ t('keys.chain.unavailable', { id }) }}</span>
          <!-- Live 开关与 Realtime 每分钟单价（分组 DTO 已下发） -->
          <GroupLiveBadge :group="groupById(id)" />
          <!-- DeepSeek 峰谷计价状态（分组 DTO 已下发，字段缺失时自动隐藏） -->
          <GroupPricingStateBadge :group="groupById(id)" />
        </div>
        <div class="flex items-center gap-1">
          <button type="button" class="btn btn-secondary px-2 py-1" :disabled="disabled || index === 0" :aria-label="t('keys.chain.moveUp', { name: groupById(id)?.name ?? id })" data-action="up" @click="move(index, -1)">↑</button>
          <button type="button" class="btn btn-secondary px-2 py-1" :disabled="disabled || index === localIds.length - 1" :aria-label="t('keys.chain.moveDown', { name: groupById(id)?.name ?? id })" data-action="down" @click="move(index, 1)">↓</button>
          <button type="button" class="btn btn-secondary px-2 py-1" :disabled="disabled" :aria-label="t('keys.chain.remove', { name: groupById(id)?.name ?? id })" data-action="remove" @click="remove(id)">×</button>
        </div>
      </li>
    </VueDraggable>
    <p v-if="!localIds.length" class="input-hint" role="status">{{ t('keys.chain.empty') }}</p>
    <label class="block">
      <span class="input-label">{{ t('keys.chain.add') }}</span>
      <select class="input" :value="''" :disabled="disabled || !availableGroups.length" :aria-label="t('keys.chain.add')" data-test="chain-add" @change="add($event)">
        <option value="">{{ t('keys.selectGroup') }}</option>
        <option v-for="group in availableGroups" :key="group.id" :value="group.id">{{ group.name }} · {{ userRates[group.id] ?? group.rate_multiplier }}x</option>
      </select>
    </label>
    <p v-if="!availableGroups.length && !localIds.length" class="input-hint">{{ t('common.noGroupsAvailable') }}</p>
    <p class="sr-only" aria-live="polite">{{ localIds.map((id) => groupById(id)?.name ?? id).join(' → ') }}</p>
  </fieldset>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { VueDraggable } from 'vue-draggable-plus'
import type { Group } from '@/types'
import GroupBadge from '@/components/common/GroupBadge.vue'
import GroupLiveBadge from '@/components/user/GroupLiveBadge.vue'
import GroupPricingStateBadge from '@/components/user/GroupPricingStateBadge.vue'
import { applyDraggedOrder, moveChainItem } from './groupChainOrder'

const props = withDefaults(defineProps<{ modelValue: number[]; groups: Group[]; userRates?: Record<number, number>; disabled?: boolean }>(), { userRates: () => ({}), disabled: false })
const emit = defineEmits<{ 'update:modelValue': [ids: number[]] }>()
const { t } = useI18n()

/**
 * 拖拽用的本地副本。
 *
 * 拖拽过程中不能直接改 props（父组件未必立刻回写），因此先落在本地，
 * 拖拽结束再把新顺序发出去；父组件回写后 watch 会重新同步。
 */
const localIds = ref<number[]>([...props.modelValue])

watch(
  () => props.modelValue,
  (ids) => {
    localIds.value = [...ids]
  },
)

const availableGroups = computed(() => props.groups.filter((g) => g.status !== 'inactive' && !localIds.value.includes(g.id)))
const groupById = (id: number) => props.groups.find((g) => g.id === id)
const badgeProps = (group: Group) => ({ name: group.name, platform: group.platform, subscriptionType: group.subscription_type, rateMultiplier: group.rate_multiplier, userRateMultiplier: props.userRates[group.id], peakRateEnabled: group.peak_rate_enabled, peakStart: group.peak_start, peakEnd: group.peak_end, peakRateMultiplier: group.peak_rate_multiplier })

const add = (event: Event) => {
  const input = event.target as HTMLSelectElement
  const id = Number(input.value)
  if (!props.disabled && availableGroups.value.some((g) => g.id === id)) {
    localIds.value = [...localIds.value, id]
    emit('update:modelValue', [...localIds.value])
  }
  input.value = ''
}
const remove = (id: number) => {
  if (props.disabled) return
  localIds.value = localIds.value.filter((value) => value !== id)
  emit('update:modelValue', [...localIds.value])
}
const move = (index: number, delta: number) => {
  if (props.disabled) return
  const next = moveChainItem(localIds.value, index, delta)
  if (next.every((id, i) => id === localIds.value[i])) return
  localIds.value = next
  emit('update:modelValue', [...next])
}

/** 拖拽结束：只接受成员集合不变的顺序调整。 */
const onDragEnd = () => {
  if (props.disabled) return
  const next = applyDraggedOrder(props.modelValue, localIds.value)
  localIds.value = [...next]
  emit('update:modelValue', [...next])
}
</script>
