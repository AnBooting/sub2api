<template>
  <div class="space-y-2" data-test="ha-optin">
    <div class="flex items-center justify-between">
      <label class="input-label mb-0">{{ t('keys.ha.title') }}</label>
      <span v-if="loading" class="text-xs text-gray-400">{{ t('common.loading') }}</span>
    </div>
    <p class="input-hint">{{ t('keys.ha.hint') }}</p>

    <p v-if="!loading && !candidates.length" class="text-xs text-gray-500" data-test="ha-empty">
      {{ t('keys.ha.empty') }}
    </p>

    <ul v-else class="space-y-1">
      <li
        v-for="candidate in candidates"
        :key="candidate.group_id"
        class="flex items-center justify-between rounded border border-gray-200 px-3 py-2 dark:border-dark-600"
        :data-test="`ha-candidate-${candidate.group_id}`"
      >
        <div class="min-w-0">
          <div class="flex items-center gap-2">
            <GroupBadge
              :name="candidate.name"
              :platform="candidate.platform"
              :rate-multiplier="candidate.rate_multiplier"
            />
            <span class="text-xs text-gray-500">{{ candidate.rate_multiplier }}x</span>
          </div>
          <p v-if="!candidate.scope_compatible" class="text-xs text-amber-600" data-test="ha-scope-mismatch">
            {{ t('keys.ha.scopeMismatch') }}
          </p>
        </div>
        <button
          type="button"
          class="ml-3 flex-shrink-0 rounded px-2 py-1 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50"
          :class="candidate.enabled
            ? 'bg-primary-600 text-white hover:bg-primary-700'
            : 'border border-gray-300 text-gray-700 hover:bg-gray-50 dark:border-dark-600 dark:text-gray-200 dark:hover:bg-dark-700'"
          :disabled="disabled || pending === candidate.group_id || !candidate.scope_compatible"
          :data-test="`ha-toggle-${candidate.group_id}`"
          @click="toggle(candidate)"
        >
          {{ candidate.enabled ? t('keys.ha.enabled') : t('keys.ha.enable') }}
        </button>
      </li>
    </ul>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import GroupBadge from '@/components/common/GroupBadge.vue'
import { listKeyHAGroups, setKeyHAGroup, type KeyHAGroupCandidate } from '@/api/keys'

const props = withDefaults(defineProps<{ apiKeyId: number; disabled?: boolean }>(), { disabled: false })
const emit = defineEmits<{ (e: 'changed'): void }>()

const { t } = useI18n()
const candidates = ref<KeyHAGroupCandidate[]>([])
const loading = ref(false)
const pending = ref<number | null>(null)
const error = ref('')

/**
 * Load the candidates for the current key.
 *
 * A failure clears the list rather than keeping stale rows: showing a group that may
 * no longer be an admin-designated fallback would invite a write the server rejects.
 */
const load = async () => {
  if (!props.apiKeyId) {
    candidates.value = []
    return
  }
  loading.value = true
  try {
    candidates.value = await listKeyHAGroups(props.apiKeyId)
    error.value = ''
  } catch (err) {
    console.error('Failed to load HA groups:', err)
    candidates.value = []
  } finally {
    loading.value = false
  }
}

/**
 * Toggle consent for one candidate.
 *
 * The server owns the multiplier and re-validates ownership, the admin flag and the
 * model scope, so the response is authoritative: we adopt its enabled flag instead of
 * assuming the requested value took effect.
 */
const toggle = async (candidate: KeyHAGroupCandidate) => {
  if (pending.value !== null) return
  pending.value = candidate.group_id
  try {
    const result = await setKeyHAGroup(props.apiKeyId, candidate.group_id, !candidate.enabled)
    candidate.enabled = result.enabled
    emit('changed')
  } catch (err) {
    console.error('Failed to set HA group:', err)
    // Re-read so the row reflects server truth instead of the optimistic guess.
    await load()
  } finally {
    pending.value = null
  }
}

onMounted(load)
watch(() => props.apiKeyId, load)
</script>
