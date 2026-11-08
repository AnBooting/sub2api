<template>
  <span
    class="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] font-medium"
    :class="badgeClass"
    :title="t(tooltipKey)"
    :data-testid="testId"
  >
    {{ t(labelKey) }}
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { gatewayFailureI18nKey, type GatewayFailureKind } from './gatewayFailure'

const props = defineProps<{ kind: GatewayFailureKind }>()

const { t } = useI18n()

const namespace = computed(() => gatewayFailureI18nKey(props.kind))
const labelKey = computed(() => namespace.value + '.badge')
const tooltipKey = computed(() => namespace.value + '.tooltip')
const testId = computed(() => 'gateway-failure-' + props.kind.replace(/_/g, '-'))

/**
 * 缺价 = 平台侧待办（琥珀警示）；model_not_found = 用户侧选错了（中性灰）。
 * 两者都不能用红色：「红色」在本站已经表示请求真的失败了。
 */
const badgeClass = computed(() =>
  props.kind === 'pricing_unavailable'
    ? 'bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-200'
    : 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300',
)
</script>
