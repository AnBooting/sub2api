<template>
  <div
    :class="['spinner', sizeClasses, colorClass]"
    role="status"
    :aria-label="t('common.loading')"
  >
    <span class="sr-only">{{ t('common.loading') }}</span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

type SpinnerSize = 'sm' | 'md' | 'lg' | 'xl'
type SpinnerColor = 'primary' | 'secondary' | 'white' | 'gray'

interface Props {
  size?: SpinnerSize
  color?: SpinnerColor
}

const props = withDefaults(defineProps<Props>(), {
  size: 'md',
  color: 'primary'
})

const sizeClasses = computed(() => {
  const sizes: Record<SpinnerSize, string> = {
    sm: 'h-4 w-4 border-2',
    md: 'h-6 w-6 border-2',
    lg: 'h-9 w-9 border-2',
    xl: 'h-12 w-12 border-2'
  }
  return sizes[props.size]
})

// 只允许三种读数色 + 纸面白；不用调色板之外的颜色。
const colorClass = computed(() => {
  const colors: Record<SpinnerColor, string> = {
    primary: 'text-signal',
    secondary: 'text-ink-soft',
    white: 'text-on-signal',
    gray: 'text-ink-faint'
  }
  return colors[props.color]
})
</script>

<style scoped>
.spinner {
  @apply inline-block shrink-0 rounded-full border-solid border-current border-r-transparent;
  animation: spin 0.75s linear infinite;
}

@media (prefers-reduced-motion: reduce) {
  .spinner {
    animation-duration: 1ms;
  }
}

@keyframes spin {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}
</style>
