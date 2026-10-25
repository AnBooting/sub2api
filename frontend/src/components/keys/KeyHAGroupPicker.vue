<template>
  <fieldset :disabled="disabled" class="space-y-2" data-test="ha-picker">
    <legend class="input-label mb-0">{{ t('keys.ha.title') }}</legend>
    <p class="input-hint">{{ t('keys.ha.createHint') }}</p>

    <p v-if="!candidates.length" class="text-xs text-gray-500" data-test="ha-picker-empty">
      {{ t('keys.ha.empty') }}
    </p>

    <ul v-else class="space-y-1">
      <li
        v-for="candidate in candidates"
        :key="candidate.id"
        class="flex items-center justify-between rounded border border-gray-200 px-3 py-2 dark:border-dark-600"
      >
        <label class="flex min-w-0 items-center gap-2 text-sm">
          <input
            type="checkbox"
            class="h-4 w-4"
            :checked="modelValue.includes(candidate.id)"
            :data-test="'ha-picker-' + candidate.id"
            @change="toggle(candidate.id)"
          />
          <span class="truncate text-gray-700 dark:text-gray-200">{{ candidate.name }}</span>
          <span class="text-xs text-gray-500">{{ candidate.rate_multiplier }}x</span>
        </label>
      </li>
    </ul>
  </fieldset>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Group } from '@/types'

const props = withDefaults(defineProps<{ modelValue: number[]; groups: Group[]; disabled?: boolean }>(), { disabled: false })
const emit = defineEmits<{ 'update:modelValue': [ids: number[]] }>()
const { t } = useI18n()

/**
 * 可选兜底分组 = 管理员已标记 allow_high_availability 且分组仍在用的那些。
 *
 * 服务端在真正写入授权时会再校验一遍（标记、模型范围、专属分组授权），
 * 这里只是把「哪些分组可以选」如实列出来。
 */
const candidates = computed(() => props.groups.filter((group) => group.allow_high_availability && group.status !== 'inactive'))

function toggle(id: number) {
  if (props.disabled) return
  const next = props.modelValue.includes(id)
    ? props.modelValue.filter((value) => value !== id)
    : [...props.modelValue, id]
  emit('update:modelValue', next)
}
</script>
