<template>
  <div class="space-y-1 text-left">
    <div class="flex flex-wrap items-center gap-1" :aria-label="t('keys.chain.title')">
      <template v-for="(id, index) in ids" :key="id">
        <span v-if="index" class="text-gray-400" aria-hidden="true">→</span>
        <GroupBadge v-if="findGroup(id)" v-bind="badgeProps(findGroup(id)!)" />
        <span v-else class="text-xs text-gray-500">{{ t('keys.chain.unavailable', { id }) }}</span>
      </template>
      <span v-if="!ids.length" class="text-sm text-gray-400">{{ t('keys.noGroup') }}</span>
    </div>
    <div v-if="ha.length" class="text-xs text-gray-500" data-test="ha-chain">
      {{ t('keys.chain.haAuthorized') }}: {{ ha.map((entry) => findGroup(entry.group_id)?.name ?? '#' + entry.group_id).join(' → ') }}
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ApiKey, Group } from '@/types'
import { normalGroupIds } from '@/utils/keyGroupChain'
import GroupBadge from '@/components/common/GroupBadge.vue'
const props = withDefaults(defineProps<{ apiKey: ApiKey; groups: Group[]; userRates?: Record<number, number> }>(), { userRates: () => ({}) })
const { t } = useI18n()
const ids = computed(() => normalGroupIds(props.apiKey))
const ha = computed(() => (props.apiKey.groups ?? []).filter((e) => e.role === 'ha').slice().sort((a, b) => a.position - b.position))
const findGroup = (id: number) => props.groups.find((g) => g.id === id) ?? (props.apiKey.group?.id === id ? props.apiKey.group : undefined)
const badgeProps = (g: Group) => ({ name: g.name, platform: g.platform, subscriptionType: g.subscription_type, rateMultiplier: g.rate_multiplier, userRateMultiplier: props.userRates[g.id], peakRateEnabled: g.peak_rate_enabled, peakStart: g.peak_start, peakEnd: g.peak_end, peakRateMultiplier: g.peak_rate_multiplier })
</script>
