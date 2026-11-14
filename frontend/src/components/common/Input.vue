<template>
  <div class="w-full">
    <label v-if="label" :for="id" class="input-label">
      {{ label }}
      <span v-if="required" class="text-danger-600" aria-hidden="true">*</span>
    </label>
    <div class="relative">
      <!-- Prefix slot（图标/货币前缀）：不参与指针事件 -->
      <div
        v-if="$slots.prefix"
        class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3 text-ink-faint"
      >
        <slot name="prefix"></slot>
      </div>

      <input
        :id="id"
        ref="inputRef"
        :type="type"
        :value="modelValue"
        :disabled="disabled"
        :required="required"
        :placeholder="placeholderText"
        :autocomplete="autocomplete"
        :readonly="readonly"
        :class="[
          'input w-full',
          $slots.prefix ? 'pl-10' : '',
          $slots.suffix ? 'pr-10' : '',
          error ? 'input-error' : ''
        ]"
        @input="onInput"
        @change="$emit('change', ($event.target as HTMLInputElement).value)"
        @blur="$emit('blur', $event)"
        @focus="$emit('focus', $event)"
        @keyup.enter="$emit('enter', $event)"
      />

      <!-- Suffix slot（密码可见性 / 清除按钮）：调用方负责按钮自身触控尺寸 -->
      <div
        v-if="$slots.suffix"
        class="absolute inset-y-0 right-0 flex items-center pr-1 text-ink-faint"
      >
        <slot name="suffix"></slot>
      </div>
    </div>
    <p v-if="error" class="input-error-text">
      {{ error }}
    </p>
    <p v-else-if="hint" class="input-hint">
      {{ hint }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'

interface Props {
  modelValue: string | number | null | undefined
  type?: string
  label?: string
  placeholder?: string
  disabled?: boolean
  required?: boolean
  readonly?: boolean
  error?: string
  hint?: string
  id?: string
  autocomplete?: string
}

const props = withDefaults(defineProps<Props>(), {
  type: 'text',
  disabled: false,
  required: false,
  readonly: false
})

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
  (e: 'change', value: string): void
  (e: 'blur', event: FocusEvent): void
  (e: 'focus', event: FocusEvent): void
  (e: 'enter', event: KeyboardEvent): void
}>()

const inputRef = ref<HTMLInputElement | null>(null)
const placeholderText = computed(() => props.placeholder || '')

const onInput = (event: Event) => {
  const value = (event.target as HTMLInputElement).value
  emit('update:modelValue', value)
}

// Expose focus method
defineExpose({
  focus: () => inputRef.value?.focus(),
  select: () => inputRef.value?.select()
})
</script>
