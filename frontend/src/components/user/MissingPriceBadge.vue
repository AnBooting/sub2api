<template>
  <span
    class="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] font-medium"
    :class="badgeClass"
    :title="t('missingPrice.badgeTooltip')"
    data-testid="missing-price-badge"
  >
    {{ t('missingPrice.badge') }}
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

/** 缺价是「请求会被拒绝」而不是普通错误 → 用警示色。 */
const badgeClass = computed(
  () =>
    'bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-200',
)
</script>
