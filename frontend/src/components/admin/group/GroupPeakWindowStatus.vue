<script setup lang="ts">
/**
 * 分组高峰时段（peak_rate_*）当前状态徽标：显示现在处于峰还是谷、当前倍率、
 * 以及下一次切换时间。判定口径与后端 Group.PeakMultiplierAt 一致
 * （左闭右开、不支持跨天、按服务器本地时区 Asia/Shanghai）。
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { peakWindowStatus } from './peakWindow'

const props = withDefaults(
  defineProps<{
    enabled?: boolean
    /** HH:MM */
    start?: string
    /** HH:MM */
    end?: string
    multiplier?: number
    /** 未启用时是否仍然显示徽标（列表里默认不显示，避免噪声）。 */
    showWhenDisabled?: boolean
    /** 注入当前时间，便于测试。 */
    now?: Date
  }>(),
  { enabled: false, start: '', end: '', multiplier: 1, showWhenDisabled: false }
)

const { t } = useI18n()

const status = computed(() =>
  peakWindowStatus(
    {
      enabled: props.enabled,
      start: props.start,
      end: props.end,
      multiplier: props.multiplier
    },
    props.now ?? new Date()
  )
)

const visible = computed(() => props.enabled || props.showWhenDisabled)

const label = computed(() => {
  switch (status.value.state) {
    case 'peak':
      return t('admin.groups.peakWindow.peak', { multiplier: status.value.multiplier })
    case 'valley':
      return t('admin.groups.peakWindow.valley')
    case 'invalid':
      return t('admin.groups.peakWindow.invalid')
    default:
      return t('admin.groups.peakWindow.disabled')
  }
})

const badgeClass = computed(() => {
  switch (status.value.state) {
    case 'peak':
      return 'badge badge-warning'
    case 'valley':
      return 'badge badge-success'
    case 'invalid':
      return 'badge badge-danger'
    default:
      return 'badge badge-gray'
  }
})

const nextSwitchText = computed(() => {
  if (!status.value.nextSwitchAt) return ''
  return t('admin.groups.peakWindow.nextSwitch', {
    time: status.value.nextSwitchAt,
    next:
      status.value.nextState === 'peak'
        ? t('admin.groups.peakWindow.peakWord')
        : t('admin.groups.peakWindow.valleyWord'),
    day:
      status.value.nextSwitchDay === 'tomorrow'
        ? t('admin.groups.peakWindow.tomorrow')
        : t('admin.groups.peakWindow.today')
  })
})

const tooltip = computed(() =>
  [
    t('admin.groups.peakWindow.window', { start: props.start, end: props.end }),
    t('admin.groups.peakWindow.multiplier', { multiplier: props.multiplier }),
    nextSwitchText.value,
    t('admin.groups.peakWindow.hint')
  ]
    .filter(Boolean)
    .join('\n')
)
</script>

<template>
  <span
    v-if="visible"
    class="badge text-xs"
    :class="badgeClass"
    :title="tooltip"
    data-testid="group-peak-window"
    :data-state="status.state"
    >{{ label }}</span
  >
</template>
