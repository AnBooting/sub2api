<template>
  <div
    class="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200"
    data-testid="missing-price-notice"
  >
    <div class="flex items-start gap-2">
      <Icon name="infoCircle" size="sm" class="mt-0.5 h-4 w-4 shrink-0" />
      <div class="min-w-0">
        <p class="font-medium">{{ title || t('missingPrice.title') }}</p>
        <p class="mt-0.5 text-xs leading-relaxed">
          {{ description || t('missingPrice.description') }}
        </p>
        <RouterLink
          v-if="showPriceCardLink && priceCardEnabled"
          to="/model-plaza"
          class="mt-1.5 inline-flex items-center gap-1 text-xs font-medium underline underline-offset-2"
          data-testid="missing-price-price-card-link"
        >
          {{ t('missingPrice.viewPriceCard') }}
        </RouterLink>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'

withDefaults(
  defineProps<{
    title?: string
    description?: string
    /** 是否显示「去查全站价卡」入口（模型广场关闭时不显示）。 */
    showPriceCardLink?: boolean
  }>(),
  { title: undefined, description: undefined, showPriceCardLink: true },
)

const { t } = useI18n()

/** 模型广场（=全站价卡）被后台关闭时不给出入口，避免点了打不开。 */
const priceCardEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.modelPlaza))
</script>
