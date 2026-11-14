<template>
  <main>
    <h1>BaseDialog browser harness</h1>
    <p>
      case: <code id="case-name">{{ caseName }}</code>
    </p>
    <button id="open" type="button" @click="open = true">Open dialog</button>

    <UserApiKeysModal v-if="caseName === 'user-keys'" :show="open" :user="testUser" @close="open = false" />
    <BaseDialog
      v-else
      :show="open"
      title="Harness dialog"
      :close-on-escape="closeOnEscape"
      :close-on-click-outside="closeOnClickOutside"
      :show-close-button="showCloseButton"
      @close="open = false"
    >
      <button v-if="caseName !== 'empty'" id="inner-first" type="button">Inner first</button>
      <button id="inner-disabled" type="button" disabled>Inner disabled</button>
      <button v-if="withNested" id="open-nested" type="button" @click="nested = true">Open nested</button>
      <button v-if="caseName !== 'empty'" id="inner-last" type="button">Inner last</button>
      <template v-if="caseName === 'hidden-boundaries'">
        <div style="display: none"><button>Hidden ancestor</button></div>
        <div hidden><button>Hidden attribute</button></div>
        <div style="visibility: hidden"><button>Invisible</button></div>
        <div inert><button>Inert ancestor</button></div>
        <fieldset disabled><button>Disabled fieldset</button></fieldset>
        <button tabindex="-2">Negative tabindex</button>
      </template>

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
import UserApiKeysModal from '@/components/admin/user/UserApiKeysModal.vue'
import type { AdminUser } from '@/types'

const testUser: AdminUser = {
  id: 1, username: 'Review user', email: 'review@example.test', role: 'user',
  balance: 0, concurrency: 1, status: 'active', allowed_groups: null, notes: '',
  balance_notify_enabled: false, balance_notify_threshold: null, balance_notify_extra_emails: [],
  created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
}

const params = new URLSearchParams(window.location.search)
const caseName = params.get('case') ?? 'basic'
const withNested = caseName === 'nested'
const closeOnClickOutside = caseName === 'backdrop'
const closeOnEscape = caseName !== 'no-escape'
const showCloseButton = caseName === 'close-button'

const open = ref(false)
const nested = ref(false)
</script>
