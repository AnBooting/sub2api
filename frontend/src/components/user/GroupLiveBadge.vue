<template>
  <span
    v-if="allowLive || realtimePrice !== null"
    class="inline-flex flex-wrap items-center gap-1.5 text-[11px]"
    data-testid="group-live-info"
  >
    <span
      v-if="allowLive"
      class="rounded bg-emerald-100 px-1.5 py-0.5 font-medium text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300"
      data-testid="group-live-badge"
    >
      {{ t('groupLive.badge') }}
    </span>
    <span
      v-if="realtimePrice !== null"
      class="text-gray-500 dark:text-gray-400"
      data-testid="group-live-price"
    >
      {{ t('groupLive.realtimePrice', { price: formattedPrice }) }}
    </span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Group } from '@/types'

const props = defineProps<{
  group?: Pick<Group, 'allow_live' | 'audio_realtime_price_per_min'> | null
}>()

const { t } = useI18n()

const allowLive = computed(() => props.group?.allow_live === true)

const realtimePrice = computed(() => {
  const value = props.group?.audio_realtime_price_per_min
  if (value == null) return null
  const numeric = Number(value)
  return Number.isFinite(numeric) ? numeric : null
})

/**
 * 分组上的 Realtime 单价是**源币种**（USD）价目表，不是账户余额。
 * 因此保留 $ 符号、补单位后缀，并标注「源价」，不与 Credit 余额口径混排。
 */
const formattedPrice = computed(() => {
  if (realtimePrice.value === null) return ''
  const trimmed = realtimePrice.value.toFixed(4).replace(/0+$/, '')
  // 去掉尾随的「.」（例如 1.0000 → 1.），不用正则避免转义歧义。
  const withoutDot = trimmed.endsWith('.') ? trimmed.slice(0, -1) : trimmed
  return `$${withoutDot}`
})
</script>
