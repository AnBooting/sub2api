<template>
  <AppLayout>
    <div class="mx-auto max-w-4xl space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('videoTasks.title') }}</h1>
          <p class="mt-0.5 text-sm text-gray-500 dark:text-gray-400">{{ t('videoTasks.subtitle') }}</p>
        </div>
        <button class="btn btn-secondary" :disabled="loading" data-testid="video-tasks-refresh" @click="load">
          <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          {{ t('common.refresh') }}
        </button>
      </div>

      <div
        v-if="errorMessage"
        class="card border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200"
        data-testid="video-tasks-error"
      >
        {{ errorMessage }}
      </div>

      <div v-if="loading && !tasks.length" class="card py-12 text-center text-sm text-gray-500 dark:text-gray-400">
        {{ t('videoTasks.loading') }}
      </div>

      <div v-else-if="!tasks.length" class="card py-12 text-center text-sm text-gray-500 dark:text-gray-400" data-testid="video-tasks-empty">
        {{ t('videoTasks.empty') }}
      </div>

      <ul v-else class="space-y-2" data-testid="video-tasks-list">
        <li v-for="task in tasks" :key="task.task_no" class="card p-4" data-testid="video-task-row">
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <p class="font-mono text-sm text-gray-900 dark:text-white">{{ task.task_no }}</p>
              <p class="mt-1 text-sm font-medium text-gray-900 dark:text-white">
                {{ task.model || '—' }}
              </p>
              <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                {{ platformLabel(task.platform || '') }}
                <span v-if="task.billing_mode"> · {{ t('videoTasks.columns.billingMode') }}: {{ task.billing_mode }}</span>
              </p>
              <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t('videoTasks.columns.createdAt') }}: {{ formatTime(task.created_at) }}
              </p>
              <p v-if="task.settled_at" class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t('videoTasks.columns.settledAt') }}: {{ formatTime(task.settled_at) }}
              </p>
              <p
                v-if="task.last_error"
                class="mt-1 text-xs text-red-600 dark:text-red-400"
                data-testid="video-task-error"
              >
                {{ t('videoTasks.lastError') }}: {{ task.last_error }}
              </p>
            </div>
            <span
              class="shrink-0 rounded-full px-2.5 py-0.5 text-xs font-medium"
              :class="statusClass(task.status)"
              data-testid="video-task-status"
            >
              {{ statusLabel(task.status) }}
            </span>
          </div>
        </li>
      </ul>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { videosAPI, type VideoTaskStatus, type VideoTaskSummary } from '@/api/videos'
import { extractApiErrorMessage } from '@/utils/apiError'
import { platformLabel } from '@/utils/platformColors'

const { t } = useI18n()

const tasks = ref<VideoTaskSummary[]>([])
const loading = ref(false)
const errorMessage = ref('')

function formatTime(value: string | undefined): string {
  if (!value) return '—'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return '—'
  return parsed.toLocaleString()
}

/** 状态只有三种：处理中 / 已完成 / 失败。 */
function statusLabel(status: VideoTaskStatus | undefined): string {
  switch (status) {
    case 'processing':
      return t('videoTasks.status.processing')
    case 'completed':
      return t('videoTasks.status.completed')
    case 'failed':
      return t('videoTasks.status.failed')
    default:
      return '—'
  }
}

function statusClass(status: VideoTaskStatus | undefined): string {
  switch (status) {
    case 'completed':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
    case 'failed':
      return 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
    default:
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
  }
}

async function load() {
  loading.value = true
  errorMessage.value = ''
  try {
    tasks.value = await videosAPI.listTasks()
  } catch (error: unknown) {
    tasks.value = []
    errorMessage.value = extractApiErrorMessage(error, t('videoTasks.loadFailed'))
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>
