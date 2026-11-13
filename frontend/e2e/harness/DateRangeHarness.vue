<template>
  <main>
    <button id="outside" type="button">Outside control</button>

    <DateRangePicker
      :start-date="startDate"
      :end-date="endDate"
      @update:start-date="startDate = $event"
      @update:end-date="endDate = $event"
      @change="onChange"
    />

    <p>
      range: <span id="range-value">{{ startDate }}..{{ endDate }}</span>
    </p>
  </main>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'

const startDate = ref('2026-01-01')
const endDate = ref('2026-01-02')
const changes = ref(0)

function onChange() {
  changes.value += 1
}
</script>
