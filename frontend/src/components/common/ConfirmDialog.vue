<template>
  <BaseDialog :show="show" :title="title" width="narrow" @close="handleCancel">
    <p class="max-w-measure text-sm leading-relaxed text-ink-soft">{{ message }}</p>
    <slot></slot>

    <template #footer>
      <button type="button" class="btn btn-secondary" @click="handleCancel">
        {{ cancelText }}
      </button>
      <button type="button" :class="['btn', danger ? 'btn-danger' : 'btn-primary']" @click="handleConfirm">
        {{ confirmText }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from './BaseDialog.vue'

const { t } = useI18n()

interface Props {
  show: boolean
  title: string
  message: string
  confirmText?: string
  cancelText?: string
  danger?: boolean
}

interface Emits {
  (e: 'confirm'): void
  (e: 'cancel'): void
}

const props = withDefaults(defineProps<Props>(), {
  danger: false
})

const confirmText = computed(() => props.confirmText || t('common.confirm'))
const cancelText = computed(() => props.cancelText || t('common.cancel'))

const emit = defineEmits<Emits>()

const handleConfirm = () => {
  emit('confirm')
}

const handleCancel = () => {
  emit('cancel')
}
</script>
