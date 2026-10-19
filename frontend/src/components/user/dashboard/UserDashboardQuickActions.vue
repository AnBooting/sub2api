<template>
  <div class="card">
    <div class="card-header flex items-center justify-between">
      <h2 class="panel-title">{{ t('dashboard.quickActions') }}</h2>
    </div>
    <div class="divide-y divide-borderline-100 dark:divide-dark-700">
      <button
        v-for="action in actions"
        :key="action.to"
        type="button"
        class="group flex w-full items-center gap-3 px-4 py-3 text-left transition-colors duration-150 hover:bg-surface-200/70 dark:hover:bg-dark-700/50"
        @click="router.push(action.to)"
      >
        <span
          class="flex h-8 w-8 shrink-0 items-center justify-center rounded-control ring-1 ring-inset"
          :class="action.primary ? 'bg-primary-50 text-primary-600 ring-primary-600/10 dark:bg-primary-500/10 dark:text-primary-400 dark:ring-primary-400/15' : 'bg-surface-200 text-ink-600 ring-ink-900/5 dark:bg-dark-700 dark:text-ink-300 dark:ring-white/5'"
        >
          <Icon :name="action.icon" size="md" />
        </span>
        <span class="min-w-0 flex-1">
          <span class="block truncate text-sm font-medium text-ink-900 dark:text-white">{{ action.title }}</span>
          <span class="mt-0.5 block truncate text-xs text-ink-500 dark:text-ink-300">{{ action.desc }}</span>
        </span>
        <Icon
          name="chevronRight"
          size="sm"
          class="shrink-0 text-ink-300 transition-colors group-hover:text-ink-500 dark:text-dark-500 dark:group-hover:text-dark-300"
        />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useBatchImageAccess } from '@/composables/useBatchImageAccess'

interface QuickAction {
  to: string
  icon: 'key' | 'chart' | 'sparkles' | 'gift'
  title: string
  desc: string
  primary?: boolean
}

const router = useRouter()
const { t } = useI18n()
const { canUseBatchImage, refreshBatchImageAccess } = useBatchImageAccess()

// 色相收敛：仅主操作使用品牌色，其余走中性灰蓝，避免一屏 4 种色相。
const actions = computed<QuickAction[]>(() => {
  const items: QuickAction[] = [
    {
      to: '/keys',
      icon: 'key',
      title: t('dashboard.createApiKey'),
      desc: t('dashboard.generateNewKey'),
      primary: true,
    },
    {
      to: '/usage',
      icon: 'chart',
      title: t('dashboard.viewUsage'),
      desc: t('dashboard.checkDetailedLogs'),
    },
  ]

  if (canUseBatchImage.value) {
    items.push({
      to: '/batch-image',
      icon: 'sparkles',
      title: t('dashboard.batchImageAgent'),
      desc: t('dashboard.batchImageAgentDesc'),
    })
  }

  items.push({
    to: '/redeem',
    icon: 'gift',
    title: t('dashboard.redeemCode'),
    desc: t('dashboard.addBalanceWithCode'),
  })

  return items
})

onMounted(() => {
  void refreshBatchImageAccess()
})
</script>
