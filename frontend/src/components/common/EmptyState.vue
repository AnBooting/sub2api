<template>
  <div class="empty-state">
    <!-- 图标不是装饰：一个 1px 边线的方形端口，里面是主题符号 -->
    <div class="mb-4 flex h-11 w-11 items-center justify-center border border-rule bg-paper-inset">
      <slot name="icon">
        <component v-if="icon" :is="icon" class="h-5 w-5 text-ink-faint" aria-hidden="true" />
        <svg
          v-else
          class="h-5 w-5 text-ink-faint"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
          stroke-width="1.5"
          aria-hidden="true"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4"
          />
        </svg>
      </slot>
    </div>

    <h3 class="empty-state-title">
      {{ displayTitle }}
    </h3>

    <p class="empty-state-description">
      {{ description }}
    </p>

    <div v-if="actionText || $slots.action" class="mt-5">
      <slot name="action">
        <component
          :is="actionTo ? 'RouterLink' : 'button'"
          v-if="actionText"
          :to="actionTo"
          :type="actionTo ? undefined : 'button'"
          class="btn btn-primary"
          @click="!actionTo && $emit('action')"
        >
          <Icon v-if="actionIcon" name="plus" size="md" />
          {{ actionText }}
        </component>
      </slot>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Component } from 'vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()

interface Props {
  icon?: Component | string
  title?: string
  description?: string
  actionText?: string
  actionTo?: string | object
  actionIcon?: boolean
  message?: string
}

const props = withDefaults(defineProps<Props>(), {
  description: '',
  actionIcon: true
})

const displayTitle = computed(() => props.title || t('common.noData'))

defineEmits(['action'])
</script>
