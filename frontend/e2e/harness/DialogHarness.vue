<template>
  <main>
    <h1>BaseDialog browser harness</h1>
    <p>
      case: <code id="case-name">{{ caseName }}</code>
    </p>
    <button id="open" type="button" @click="open = true">Open dialog</button>

    <BaseDialog
      :show="open"
      title="Harness dialog"
      :close-on-escape="closeOnEscape"
      :close-on-click-outside="closeOnClickOutside"
      :show-close-button="showCloseButton"
      @close="open = false"
    >
      <button id="inner-first" type="button">Inner first</button>
      <button id="inner-disabled" type="button" disabled>Inner disabled</button>
      <button v-if="withNested" id="open-nested" type="button" @click="nested = true">Open nested</button>
      <button id="inner-last" type="button">Inner last</button>

      <BaseDialog
        v-if="withNested"
        :show="nested"
        title="Nested dialog"
        :show-close-button="false"
        @close="nested = false"
      >
        <button id="nested-first" type="button">Nested first</button>
        <button id="nested-last" type="button">Nested last</button>
      </BaseDialog>
    </BaseDialog>
  </main>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'

const params = new URLSearchParams(window.location.search)
const caseName = params.get('case') ?? 'basic'
const withNested = caseName === 'nested'
const closeOnClickOutside = caseName === 'backdrop'
const closeOnEscape = caseName !== 'no-escape'
const showCloseButton = caseName === 'close-button'

const open = ref(false)
const nested = ref(false)
</script>
