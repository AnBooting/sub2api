<template>
  <div class="relative w-full">
    <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3">
      <Icon name="search" size="md" class="text-ink-faint" />
    </div>
    <input
      v-model="searchValue"
      type="text"
      class="input pl-10"
      :placeholder="placeholder"
      :aria-label="placeholder"
    />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import Icon from '@/components/icons/Icon.vue'

const props = withDefaults(defineProps<{
  modelValue: string
  placeholder?: string
  debounceMs?: number
}>(), {
  placeholder: 'Search...',
  debounceMs: 300
})

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
  (e: 'search', value: string): void
}>()

const debouncedEmitSearch = useDebounceFn((value: string) => {
  emit('search', value)
}, props.debounceMs)

// 用 v-model 而不是手写 input 监听：Vue 的 vModelText 会在 IME 组合期间
// 抑制 input 事件，等 compositionend 再写回，避免把拼音中间态发出去。
const searchValue = computed({
  get: () => props.modelValue,
  set: (value: string) => {
    emit('update:modelValue', value)
    debouncedEmitSearch(value)
  }
})
</script>
