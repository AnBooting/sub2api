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
        <section class="space-y-3 border-t border-gray-100 pt-5 dark:border-dark-700" aria-labelledby="deepseek-calendar-title" data-testid="deepseek-calendar">
          <h3 id="deepseek-calendar-title" class="font-semibold">{{ t('admin.settings.pricing.calendar.title') }}</h3>
          <p class="text-sm text-gray-500">{{ t('admin.settings.pricing.calendar.rules') }}</p>
          <p v-if="!calendarSupported" class="rounded-lg bg-gray-50 p-3 text-sm text-gray-500 dark:bg-dark-700" data-testid="calendar-unsupported">{{ t('admin.settings.pricing.calendar.unsupported') }}</p>
          <template v-else>
            <p v-if="status.deepseek_calendar_year_missing === true" role="status" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-700 dark:bg-amber-900/20 dark:text-amber-300" data-testid="calendar-year-missing">{{ t('admin.settings.pricing.calendar.yearMissing') }}</p>
            <p v-else class="text-sm text-green-600 dark:text-green-400" data-testid="calendar-year-maintained">{{ t('admin.settings.pricing.calendar.yearMaintained') }}</p>
            <label for="deepseek-calendar-json" class="input-label">{{ t('admin.settings.pricing.calendar.editorLabel') }}</label>
            <textarea id="deepseek-calendar-json" v-model="calendarDraft" rows="8" spellcheck="false" class="input font-mono text-xs" :disabled="saving" :aria-invalid="Boolean(calendarError)" aria-describedby="deepseek-calendar-help" data-testid="calendar-draft" @input="calendarError = ''"></textarea>
            <p id="deepseek-calendar-help" class="input-hint">{{ t('admin.settings.pricing.calendar.editorHelp') }}</p>
            <p v-if="calendarDirty" class="text-sm text-amber-600 dark:text-amber-400" data-testid="calendar-unsaved">{{ t('admin.settings.pricing.calendar.unsaved') }}</p>
            <p v-if="calendarError" role="alert" class="text-sm text-red-600" data-testid="calendar-error">{{ calendarError }}</p>
            <div class="flex flex-wrap gap-2">
              <button type="button" class="btn btn-primary" :disabled="saving || !calendarDirty" data-testid="calendar-save" @click="saveCalendar">{{ t('admin.settings.pricing.calendar.save') }}</button>
              <button type="button" class="btn btn-secondary" :disabled="saving || !calendarDirty" data-testid="calendar-reset" @click="resetCalendarDraft">{{ t('admin.settings.pricing.calendar.resetDraft') }}</button>
            </div>
            <details class="text-xs text-gray-500" data-testid="calendar-saved-state">
              <summary class="cursor-pointer font-medium">{{ t('admin.settings.pricing.calendar.savedState') }}</summary>
              <pre class="mt-2 max-h-48 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-gray-50 p-3 dark:bg-dark-700">{{ savedCalendarText }}</pre>
            </details>
          </template>
        </section>
      </template>
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
      <button v-if="!status && !loading" type="button" class="btn btn-secondary" @click="load">{{ t('admin.settings.pricing.retry') }}</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'
import { parseDeepSeekCalendar, serializeDeepSeekCalendar, validateDeepSeekCalendar } from '@/utils/deepseekCalendar'
import { getPricingControls, updatePricingControls, refreshPricing, type PricingStatus, type PricingControls } from '@/api/admin/pricing'

const { t } = useI18n()
const app = useAppStore()
const status = ref<PricingStatus | null>(null)
const saving = ref(false)
const refreshing = ref(false)
const loading = ref(true)
const error = ref('')
const calendarDraft = ref('')
const savedCalendarText = ref('')
const calendarError = ref('')
const calendarDirty = computed(() => calendarDraft.value !== savedCalendarText.value)
const calendarSupported = computed(() => status.value?.deepseek_holidays !== undefined && typeof status.value?.deepseek_calendar_year_missing === 'boolean' && validateDeepSeekCalendar(status.value.deepseek_holidays).ok)
let revision = 0
let disposed = false
const timestamp = (date: string) => date && !date.startsWith('0001-') ? formatDateTime(date) : t('admin.settings.pricing.never')
const message = (err: unknown) => (err as { response?: { data?: { message?: string } }; message?: string }).response?.data?.message || (err as { message?: string }).message || t('admin.settings.pricing.failed')

function applyStatus(next: PricingStatus, replaceDraft = false) {
  const keepDraft = calendarDirty.value && !replaceDraft
  status.value = next
  if (next.deepseek_holidays !== undefined && validateDeepSeekCalendar(next.deepseek_holidays).ok) {
    const serialized = serializeDeepSeekCalendar(next.deepseek_holidays)
    savedCalendarText.value = serialized
    if (!keepDraft) calendarDraft.value = serialized
  }
}
function resetCalendarDraft() { calendarDraft.value = savedCalendarText.value; calendarError.value = '' }
async function load() {
  const expected = revision
  loading.value = true
  error.value = ''
  try { const next = await getPricingControls(); if (!disposed && expected === revision) applyStatus(next) }
  catch (err) { if (!disposed && expected === revision) error.value = message(err) }
  finally { if (!disposed && expected === revision) loading.value = false }
}
async function save(patch: Pick<Partial<PricingControls>, 'remote_enabled' | 'native_cny_enabled'>) {
  if (!status.value || saving.value) return
  saving.value = true
  revision++
  error.value = ''
  try {
    // Deliberately omit the calendar: switch changes cannot replay a stale map
    // or submit an unreviewed JSON draft. The backend preserves its latest map.
    const next = await updatePricingControls({ remote_enabled: status.value.remote_enabled, native_cny_enabled: status.value.native_cny_enabled, ...patch })
    if (disposed) return
    applyStatus(next)
    await app.fetchPublicSettings(true)
    app.showSuccess(t('admin.settings.pricing.saved'))
  } catch (err) { const detail = message(err); await load(); error.value = detail }
  finally { saving.value = false }
}
async function saveCalendar() {
  if (!status.value || !calendarSupported.value || saving.value || !calendarDirty.value) return
  const parsed = parseDeepSeekCalendar(calendarDraft.value)
  if (!parsed.ok) { calendarError.value = t('admin.settings.pricing.calendar.errors.' + parsed.error, { value: parsed.value ?? '' }); return }
  saving.value = true
  revision++
  calendarError.value = ''
  try {
    const next = await updatePricingControls({ remote_enabled: status.value.remote_enabled, native_cny_enabled: status.value.native_cny_enabled, deepseek_holidays: parsed.calendar })
    if (disposed) return
    // A malformed/older response is not proof that the submitted calendar took effect.
    if (next.deepseek_holidays === undefined || typeof next.deepseek_calendar_year_missing !== 'boolean' || !validateDeepSeekCalendar(next.deepseek_holidays).ok) throw new Error(t('admin.settings.pricing.calendar.missingReadback'))
    applyStatus(next, true)
    await app.fetchPublicSettings(true)
    app.showSuccess(t('admin.settings.pricing.calendar.saved'))
  } catch (err) { if (!disposed) calendarError.value = message(err) }
  finally { saving.value = false }
}
async function refresh() {
  if (!status.value?.remote_enabled || saving.value || refreshing.value) return
  refreshing.value = true
  const expected = ++revision
  error.value = ''
  try {
    await refreshPricing()
    if (disposed || expected !== revision) return
    const next = await getPricingControls()
    if (!disposed && expected === revision) applyStatus(next)
  } catch (err) { if (!disposed && expected === revision) error.value = message(err) }
  finally { refreshing.value = false }
}
let poll: ReturnType<typeof setInterval> | undefined
onMounted(async () => {
  await load()
  if (disposed) return
  poll = setInterval(async () => {
    if (saving.value || refreshing.value || !status.value?.remote_enabled || !status.value.native_cny_enabled) return
    const expected = revision
    try { const next = await getPricingControls(); if (!disposed && expected === revision && !saving.value) applyStatus(next) } catch { /* keep the last saved state and draft */ }
  }, 5000)
})
onUnmounted(() => { disposed = true; revision++; if (poll) clearInterval(poll) })
</script>
