<script setup lang="ts">
/**
 * 分组定价状态徽标：让管理员在分组列表里一眼看到该分组的定价是否可用。
 *
 * 口径（对齐后端 model_pricing_resolver.go / pricing_manual_validation.go）：
 * - complete：规则都填全了 → 按填写的单价计费。
 * - incomplete：有规则缺价字段 → 是否被拒绝取决于该模型有没有官方内置价。
 * - builtin_only：规则只登记模型、没填价格 → 该模型有官方内置价时按官方内置价计费
 *   （官方内置价 = 该模型自身的官方价，不是缺价放行）。
 * - no_rules：没有逐模型规则 → 完全按渠道价/官方内置价（价卡）计费。
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ChannelModelPricing } from '@/api/admin/channels'
import { summarizeGroupPricing } from '@/components/admin/pricing/priceStatus'

const props = withDefaults(
  defineProps<{ rules?: ChannelModelPricing[] | null }>(),
  { rules: () => [] }
)

const { t } = useI18n()

const summary = computed(() => summarizeGroupPricing(props.rules))

const label = computed(() => {
  switch (summary.value.status) {
    case 'complete':
      return t('admin.groups.pricingStatus.complete')
    case 'incomplete':
      return t('admin.groups.pricingStatus.incomplete', {
        count: summary.value.incompleteModels.length
      })
    case 'builtin_only':
      return t('admin.groups.pricingStatus.builtinOnly')
    default:
      return t('admin.groups.pricingStatus.noRules')
  }
})

const badgeClass = computed(() => {
  switch (summary.value.status) {
    case 'complete':
      return 'badge badge-success'
    case 'incomplete':
      return 'badge badge-warning'
    case 'builtin_only':
      return 'badge badge-info'
    default:
      return 'badge badge-gray'
  }
})

const tooltip = computed(() => {
  const base = t(`admin.groups.pricingStatus.tip.${summary.value.status}`)
  if (summary.value.status !== 'incomplete' || summary.value.incompleteModels.length === 0) {
    return base
  }
  return `${base}${t('admin.groups.pricingStatus.tipModels', {
    models: summary.value.incompleteModels.join('、')
  })}`
})
</script>

<template>
  <span
    class="badge text-xs"
    :class="badgeClass"
    :title="tooltip"
    data-testid="group-pricing-status"
    :data-status="summary.status"
    >{{ label }}</span
  >
</template>
