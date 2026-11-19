<template>
  <Teleport to="body">
    <div
      class="pointer-events-none fixed inset-x-3 top-3 z-[9999] flex flex-col items-end gap-2 sm:inset-x-auto sm:right-4 sm:top-4"
      aria-live="polite"
      aria-atomic="true"
    >
      <TransitionGroup
        enter-active-class="transition duration-150 ease-cut"
        enter-from-class="opacity-0 translate-y-1"
        enter-to-class="opacity-100 translate-y-0"
        leave-active-class="transition duration-150 ease-cut"
        leave-from-class="opacity-100 translate-y-0"
        leave-to-class="opacity-0"
      >
        <div
          v-for="toast in toasts"
          :key="toast.id"
          :class="[
            'toast',
            // .toast 语义类自带 fixed 定位（供单条直接使用时）；这里由外层 flex 列
            // 统一排布，所以用 utility 复位成相对定位，避免多条叠在同一点。
            'relative right-auto top-auto w-full min-w-0 sm:w-auto',
            getToneClass(toast.type)
          ]"
        >
          <Icon
            :name="getToastIconName(toast.type)"
            size="md"
            :class="['mt-0.5 shrink-0', getIconColor(toast.type)]"
            aria-hidden="true"
          />

          <div class="min-w-0 flex-1">
            <p v-if="toast.title" class="text-sm font-semibold text-ink">
              {{ toast.title }}
            </p>
            <p :class="['text-sm leading-relaxed', toast.title ? 'mt-0.5 text-ink-soft' : 'text-ink']">
              {{ toast.message }}
            </p>
          </div>

          <button
            type="button"
            class="btn btn-ghost btn-icon -my-1 -mr-1 shrink-0"
            :aria-label="t('common.close')"
            @click="removeToast(toast.id)"
          >
            <Icon name="x" size="sm" />
          </button>

          <!-- 停留进度：一条 1px 痕量线在走完前给出还剩多久的读数 -->
          <div v-if="toast.duration" class="toast-progress-track">
            <div
              :class="['toast-progress', getProgressBarColor(toast.type)]"
              :style="{ animationDuration: `${toast.duration}ms` }"
            ></div>
          </div>
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'

const { t } = useI18n()
const appStore = useAppStore()

const toasts = computed(() => appStore.toasts)

const getToastIconName = (type: string): 'checkCircle' | 'xCircle' | 'exclamationTriangle' | 'infoCircle' => {
  switch (type) {
    case 'success':
      return 'checkCircle'
    case 'error':
      return 'xCircle'
    case 'warning':
      return 'exclamationTriangle'
    case 'info':
    default:
      return 'infoCircle'
  }
}

const getIconColor = (type: string): string => {
  const colors: Record<string, string> = {
    success: 'text-success-600',
    error: 'text-danger-600',
    warning: 'text-warning-600',
    info: 'text-signal'
  }
  return colors[type] || colors.info
}

// 左侧 3px 竖条由语义类给出底色，这里只选语气
const getToneClass = (type: string): string => {
  const tones: Record<string, string> = {
    success: 'toast-success',
    error: 'toast-error',
    warning: 'toast-warning',
    info: 'toast-info'
  }
  return tones[type] || tones.info
}

const getProgressBarColor = (type: string): string => {
  const colors: Record<string, string> = {
    success: 'bg-success-500',
    error: 'bg-danger-500',
    warning: 'bg-warning-500',
    info: 'bg-signal'
  }
  return colors[type] || colors.info
}

const removeToast = (id: string) => {
  appStore.hideToast(id)
}
</script>

<style scoped>
.toast-progress-track {
  position: absolute;
  inset-inline: 0;
  bottom: 0;
  height: 1px;
  background-color: rgb(var(--rule));
}

.toast-progress {
  height: 100%;
  width: 100%;
  animation-name: toast-progress-shrink;
  animation-timing-function: linear;
  animation-fill-mode: forwards;
}

@keyframes toast-progress-shrink {
  from {
    width: 100%;
  }
  to {
    width: 0%;
  }
}

@media (prefers-reduced-motion: reduce) {
  .toast-progress {
    animation: none;
    width: 100%;
  }
}
</style>
