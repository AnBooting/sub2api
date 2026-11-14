<template>
  <!--
    轨道本身只有 20px 高，但按钮盒是 44px：视觉不动，触控面够大。
    颜色只有两种读数：通电 = 钴蓝（signal），断电 = 痕量线灰。
  -->
  <button
    type="button"
    role="switch"
    :aria-checked="modelValue"
    class="inline-flex h-11 w-11 shrink-0 items-center justify-center focus:outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-signal"
    @click="toggle"
  >
    <span :class="['switch', modelValue && 'switch-active']">
      <span class="switch-thumb" />
    </span>
  </button>
</template>

<script setup lang="ts">
const props = defineProps<{
  modelValue: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

function toggle() {
  emit('update:modelValue', !props.modelValue)
}
</script>
