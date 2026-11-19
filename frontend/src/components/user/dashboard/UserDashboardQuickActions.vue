<template>
  <div class="card">
   <div class="border-b border-rule px-6 py-4">
   <h2 class="text-ink">{{ t('dashboard.quickActions') }}</h2>
    </div>
    <div class="space-y-3 p-4">
   <button @click="router.push('/keys')" class="group flex w-full items-center gap-4 rounded-xl bg-paper-inset p-4 text-left transition-all duration-200 hover:bg-paper-inset /50 dark:hover:bg-surface">
   <div class="flex h-12 w-12 flex-shrink-0 items-center justify-center rounded-xl bg-primary-100 transition-transform dark:bg-primary-900/30">
          <Icon name="key" size="lg" class="text-primary-600 dark:text-primary-400" />
        </div>
        <div class="min-w-0 flex-1">
   <p class="text-sm font-medium text-ink">{{ t('dashboard.createApiKey') }}</p>
   <p class="text-xs text-ink-soft">{{ t('dashboard.generateNewKey') }}</p>
        </div>
        <Icon
          name="chevronRight"
          size="md"
   class="text-ink-faint transition-colors group-hover:text-primary-500"
        />
      </button>

   <button @click="router.push('/usage')" class="group flex w-full items-center gap-4 rounded-xl bg-paper-inset p-4 text-left transition-all duration-200 hover:bg-paper-inset /50 dark:hover:bg-surface">
   <div class="flex h-12 w-12 flex-shrink-0 items-center justify-center rounded-xl bg-success-100 transition-transform dark:bg-success-900/30">
   <Icon name="chart" size="lg" class="text-success-600 dark:text-success-400" />
        </div>
        <div class="min-w-0 flex-1">
   <p class="text-sm font-medium text-ink">{{ t('dashboard.viewUsage') }}</p>
   <p class="text-xs text-ink-soft">{{ t('dashboard.checkDetailedLogs') }}</p>
        </div>
        <Icon
          name="chevronRight"
          size="md"
   class="text-ink-faint transition-colors group-hover:text-success-500"
        />
      </button>

   <button v-if="canUseBatchImage" @click="router.push('/batch-image')" class="group flex w-full items-center gap-4 rounded-xl bg-paper-inset p-4 text-left transition-all duration-200 hover:bg-paper-inset /50 dark:hover:bg-surface">
   <div class="flex h-12 w-12 flex-shrink-0 items-center justify-center rounded-xl bg-primary-100 transition-transform dark:bg-primary-900/30">
   <Icon name="sparkles" size="lg" class="text-primary-600 dark:text-primary-400" />
        </div>
        <div class="min-w-0 flex-1">
   <p class="text-sm font-medium text-ink">{{ t('dashboard.batchImageAgent') }}</p>
   <p class="text-xs text-ink-soft">{{ t('dashboard.batchImageAgentDesc') }}</p>
        </div>
        <Icon
          name="chevronRight"
          size="md"
   class="text-ink-faint transition-colors group-hover:text-primary-500"
        />
      </button>

   <button @click="router.push('/redeem')" class="group flex w-full items-center gap-4 rounded-xl bg-paper-inset p-4 text-left transition-all duration-200 hover:bg-paper-inset /50 dark:hover:bg-surface">
   <div class="flex h-12 w-12 flex-shrink-0 items-center justify-center rounded-xl bg-warning-100 transition-transform dark:bg-warning-900/30">
   <Icon name="gift" size="lg" class="text-warning-600 dark:text-warning-400" />
        </div>
        <div class="min-w-0 flex-1">
   <p class="text-sm font-medium text-ink">{{ t('dashboard.redeemCode') }}</p>
   <p class="text-xs text-ink-soft">{{ t('dashboard.addBalanceWithCode') }}</p>
        </div>
        <Icon
          name="chevronRight"
          size="md"
   class="text-ink-faint transition-colors group-hover:text-warning-500"
        />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useBatchImageAccess } from '@/composables/useBatchImageAccess'
const router = useRouter()
const { t } = useI18n()
const { canUseBatchImage, refreshBatchImageAccess } = useBatchImageAccess()

onMounted(() => {
  void refreshBatchImageAccess()
})
</script>
