<template>
  <div class="card" data-testid="pricing-settings">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-semibold">{{ t('admin.settings.pricing.title') }}</h2>
      <p class="mt-1 text-sm text-gray-500">{{ t('admin.settings.pricing.description') }}</p>
    </div>
    <div class="space-y-5 p-6">
      <p v-if="loading" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
      <template v-else-if="status">
        <label class="flex items-start justify-between gap-4">
          <span>
            <span class="block font-medium">{{ t('admin.settings.pricing.nativeCNY') }}</span>
            <span class="mt-1 block text-xs text-gray-500">{{ t('admin.settings.pricing.nativeCNYHint') }}</span>
          </span>
          <input type="checkbox" :checked="status.native_cny_enabled" :disabled="saving" data-testid="native-cny-toggle" @change="save({ native_cny_enabled: ($event.target as HTMLInputElement).checked })" />
        </label>
        <label class="flex items-start justify-between gap-4">
          <span>
            <span class="block font-medium">{{ t('admin.settings.pricing.remote') }}</span>
            <span class="mt-1 block text-xs text-gray-500">{{ t('admin.settings.pricing.remoteHint') }}</span>
          </span>
          <input type="checkbox" :checked="status.remote_enabled" :disabled="saving" data-testid="remote-pull-toggle" @change="save({ remote_enabled: ($event.target as HTMLInputElement).checked })" />
        </label>
        <p v-if="!status.remote_enabled" class="rounded bg-gray-100 p-3 text-sm dark:bg-dark-700">{{ t('admin.settings.pricing.offline') }}</p>
        <div class="grid gap-3 text-sm sm:grid-cols-2">
          <div>USD: {{ status.usd_model_count }} · {{ timestamp(status.usd_last_updated) }}</div>
          <div>CNY: {{ status.cny_model_count }} · {{ timestamp(status.cny_last_updated) }}</div>
        </div>
        <p v-if="status.native_cny_enabled && !status.cny_model_count" class="text-sm text-amber-600">{{ t('admin.settings.pricing.noCNY') }}</p>
        <p v-if="status.cny_error" class="break-words text-sm text-amber-600">{{ status.cny_error }}</p>
        <p class="text-xs text-gray-500">{{ t('admin.settings.pricing.sources') }} <a href="https://github.com/basellm/llm-metadata" target="_blank" rel="noopener noreferrer" class="underline">basellm</a> · <a href="https://llmabacus.com" target="_blank" rel="noopener noreferrer" class="underline">LLM Abacus</a></p>
        <button type="button" class="btn btn-secondary" :disabled="saving || refreshing || !status.remote_enabled" data-testid="refresh-pricing" @click="refresh">{{ t('admin.settings.pricing.refresh') }}</button>
      </template>
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
      <button v-if="!status && !loading" type="button" class="btn btn-secondary" @click="load">{{ t('admin.settings.pricing.retry') }}</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'
import { getPricingControls, updatePricingControls, refreshPricing, type PricingStatus, type PricingControls } from '@/api/admin/pricing'

const { t } = useI18n()
const app = useAppStore()
const status = ref<PricingStatus | null>(null)
const saving = ref(false)
const refreshing = ref(false)
const loading = ref(true)
const error = ref('')
let revision = 0
const timestamp = (date: string) => date && !date.startsWith('0001-') ? formatDateTime(date) : t('admin.settings.pricing.never')
const message = (err: unknown) => (err as { response?: { data?: { message?: string } } }).response?.data?.message || t('admin.settings.pricing.failed')

async function load() {
  loading.value = true
  error.value = ''
  try { status.value = await getPricingControls() } catch (err) { error.value = message(err) } finally { loading.value = false }
}
async function save(patch: Partial<PricingControls>) {
  if (!status.value || saving.value) return
  saving.value = true
  revision++
  error.value = ''
  try {
    status.value = await updatePricingControls({ remote_enabled: status.value.remote_enabled, native_cny_enabled: status.value.native_cny_enabled, ...patch })
    await app.fetchPublicSettings(true)
    app.showSuccess(t('admin.settings.pricing.saved'))
  } catch (err) { const detail = message(err); await load(); error.value = detail } finally { saving.value = false }
}
async function refresh() {
  if (!status.value?.remote_enabled || saving.value || refreshing.value) return
  refreshing.value = true
  error.value = ''
  try { await refreshPricing(); status.value = await getPricingControls() } catch (err) { error.value = message(err) } finally { refreshing.value = false }
}
let poll: ReturnType<typeof setInterval> | undefined
onMounted(async () => {
  await load()
  poll = setInterval(async () => {
    if (saving.value || refreshing.value || !status.value?.remote_enabled || !status.value.native_cny_enabled) return
    const expected = revision
    try { const next = await getPricingControls(); if (expected === revision && !saving.value) status.value = next } catch { /* keep the last status */ }
  }, 5000)
})
onUnmounted(() => { if (poll) clearInterval(poll) })
</script>
