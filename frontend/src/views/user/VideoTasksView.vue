<template>
  <AppLayout>
    <div class="mx-auto max-w-4xl space-y-4">
      <div class="title-block">
        <div class="min-w-0">
          <h1 class="title-block-title">{{ t('videoTasks.title') }}</h1>
          <p class="lead mt-2">{{ t('videoTasks.subtitle') }}</p>
        </div>
        <button class="btn btn-secondary" :disabled="loading" data-testid="video-tasks-refresh" @click="load">
          <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          {{ t('common.refresh') }}
        </button>
      </div>

      <div
        v-if="errorMessage"
   class="card border border-danger-200 bg-danger-50 p-4 text-sm text-danger-700 dark:border-danger-500/30 dark:bg-danger-500/10 dark:text-danger-200"
        data-testid="video-tasks-error"
      >
        {{ errorMessage }}
      </div>

   <div v-if="loading && !tasks.length" class="card py-12 text-center text-sm text-ink-soft">
        {{ t('videoTasks.loading') }}
      </div>

   <div v-else-if="!tasks.length" class="card py-12 text-center text-sm text-ink-soft" data-testid="video-tasks-empty">
        {{ t('videoTasks.empty') }}
      </div>

      <ul v-else class="space-y-2" data-testid="video-tasks-list">
        <li v-for="task in tasks" :key="task.task_no" class="card p-4" data-testid="video-task-row">
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
   <p class="font-mono text-sm text-ink">{{ task.task_no }}</p>
   <p class="mt-1 text-sm font-medium text-ink">
                {{ task.model || '—' }}
              </p>
   <p class="mt-0.5 text-xs text-ink-soft">
                {{ platformLabel(task.platform || '') }}
                <span v-if="billingModeLabel(task)"> · {{ billingModeLabel(task) }}</span>
              </p>
   <p class="mt-0.5 text-xs text-ink-soft">
                {{ t('videoTasks.columns.createdAt') }}: {{ formatTime(task.created_at) }}
              </p>
   <p v-if="task.settled_at" class="mt-0.5 text-xs text-ink-soft">
                {{ t('videoTasks.columns.settledAt') }}: {{ formatTime(task.settled_at) }}
              </p>
              <p
                v-if="task.last_error"
                class="mt-1 text-xs"
   :class="task.status === 'processing' ? 'text-ink-soft ' : 'text-danger-600 dark:text-danger-300'"
                data-testid="video-task-error"
              >
                {{ t(task.status === 'processing' ? 'videoTasks.processingUpdate' : 'videoTasks.lastError') }}: {{ taskErrorLabel(task) }}
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

      <!-- 游标分页：还有下一页才显示；失败时保留已加载的列表 -->
      <div v-if="nextCursor" class="flex flex-col items-center gap-2">
        <button
          class="btn btn-secondary"
          :disabled="loadingMore"
          data-testid="video-tasks-load-more"
          @click="loadMore"
        >
          {{ loadingMore ? t('videoTasks.loadingMore') : t('videoTasks.loadMore') }}
        </button>
        <p
          v-if="loadMoreError"
   class="text-xs text-danger-600 dark:text-danger-400"
          data-testid="video-tasks-load-more-error"
        >
          {{ loadMoreError }}
        </p>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import {
  VIDEO_TASK_LIST_LIMIT,
  listVideoTasks,
  type VideoTaskStatus,
  type VideoTaskSummary,
} from '@/api/videos'
import { extractApiErrorMessage } from '@/utils/apiError'
import { platformLabel } from '@/utils/platformColors'

const { t } = useI18n()

const tasks = ref<VideoTaskSummary[]>([])
const nextCursor = ref<string | null>(null)
const loading = ref(false)
const loadingMore = ref(false)
const errorMessage = ref('')
const loadMoreError = ref('')

function formatTime(value: string | undefined): string {
  if (!value) return '—'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return '—'
  return parsed.toLocaleString()
}

/**
 * 失败说明：优先按后端给的稳定错误码做 i18n（不按文案匹配），
 * 没有错误码时回落到后端的可公开文案。
 */
function taskErrorLabel(task: VideoTaskSummary): string {
	const code = task.last_error_code
	if (code) {
		const key = `videoTasks.errors.${code}`
		const translated = t(key)
		if (translated && translated !== key) return translated
	}
	return task.last_error || ''
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
   return 'bg-success-100 text-success-700 dark:bg-success-900/40 dark:text-success-300'
    case 'failed':
   return 'bg-danger-100 text-danger-700 dark:bg-danger-900/40 dark:text-danger-300'
    default:
   return 'bg-warning-100 text-warning-700 dark:bg-warning-900/40 dark:text-warning-300'
  }
}

/**
 * 计费模式文案：按量 / 按次 / 按秒。
 *
 * 字段缺失时返回空串（不显示空白标签）；出现未知取值时原样展示，
 * 避免把后端新增的计费模式悄悄吞掉。
 */
function billingModeLabel(task: VideoTaskSummary): string {
  const mode = (task.billing_mode ?? '').trim()
  if (!mode) return ''
  switch (mode) {
    case 'per_token':
      return t('videoTasks.billingMode.per_token')
    case 'per_request':
      return t('videoTasks.billingMode.per_request')
    case 'per_second':
      return t('videoTasks.billingMode.per_second')
    default:
      return mode
  }
}

async function load() {
  loading.value = true
  errorMessage.value = ''
  loadMoreError.value = ''
  try {
    const result = await listVideoTasks({ limit: VIDEO_TASK_LIST_LIMIT })
    tasks.value = result.items
    nextCursor.value = result.next_cursor
  } catch (error: unknown) {
    tasks.value = []
    nextCursor.value = null
    errorMessage.value = extractApiErrorMessage(error, t('videoTasks.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function loadMore() {
  if (!nextCursor.value || loadingMore.value) return
  loadingMore.value = true
  loadMoreError.value = ''
  try {
    const result = await listVideoTasks({ limit: VIDEO_TASK_LIST_LIMIT, cursor: nextCursor.value })
    // 追加而不是替换：分页失败/返回异常时已加载的列表必须保留。
    tasks.value = [...tasks.value, ...result.items]
    nextCursor.value = result.next_cursor
  } catch (error: unknown) {
    loadMoreError.value = extractApiErrorMessage(error, t('videoTasks.loadMoreFailed'))
  } finally {
    loadingMore.value = false
  }
}

onMounted(load)
</script>
