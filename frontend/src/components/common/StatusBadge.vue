<template>
  <span class="inline-flex items-center gap-1.5 align-middle">
    <span
      :class="['inline-block h-1.5 w-1.5 shrink-0 rounded-full', variantClass]"
      aria-hidden="true"
    ></span>
    <span class="text-[0.8125rem] leading-none text-ink-soft">
      {{ label }}
    </span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  status: string
  label: string
}>()

// 圆点只表达"这条线通没通"：通路=moss，提醒=ochre，断线=oxide，未知=痕量线灰。
const variantClass = computed(() => {
  switch (props.status) {
    case 'active':
    case 'success':
      return 'bg-success-500'
    case 'disabled':
    case 'inactive':
    case 'warning':
      return 'bg-warning-500'
    case 'error':
    case 'danger':
      return 'bg-danger-500'
    default:
      return 'bg-ink-faint'
  }
})
</script>
