<template>
  <main>
    <button id="outside" type="button">Outside control</button>

    <Select
      id="harness-select"
      v-model="value"
      :options="options"
      :searchable="false"
      aria-label="Harness select"
    />

    <p>
      value: <span id="selected-value">{{ String(value) }}</span>
    </p>
  </main>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import Select from '@/components/common/Select.vue'

const value = ref<string | number | null>(null)

const options = [
  { value: 'alpha', label: 'Alpha' },
  { value: 'beta', label: 'Beta (disabled)', disabled: true },
  { value: 'gamma', label: 'Gamma' },
  { value: 'delta', label: 'Delta' }
]
</script>
