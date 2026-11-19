<template>
  <div class="mt-5 grid grid-cols-2 gap-2">
    <div
   class="rounded-xl p-3 bg-paper-inset/80 /40 border border-rule"
    >
      <div
   class="flex items-center gap-1.5 text-[10px] font-semibold text-ink-faint"
      >
        <Icon :name="primaryIcon" size="xs" />
        <span>{{ primaryLabel }}</span>
      </div>
   <div class="mt-1.5 text-lg font-bold font-mono tabular-nums text-ink">
   {{ primaryValue }}<span class="text-xs font-normal text-ink-faint ml-0.5">{{ primaryUnit }}</span>
      </div>
    </div>
    <div
   class="rounded-xl p-3 bg-paper-inset/80 /40 border border-rule"
    >
      <div
   class="flex items-center gap-1.5 text-[10px] font-semibold text-ink-faint"
      >
        <Icon :name="secondaryIcon" size="xs" />
        <span>{{ secondaryLabel }}</span>
      </div>
   <div class="mt-1.5 text-lg font-bold font-mono tabular-nums text-ink">
   {{ secondaryValue }}<span class="text-xs font-normal text-ink-faint ml-0.5">{{ secondaryUnit }}</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'

defineProps<{
  primaryLabel: string
  primaryValue: string
  primaryUnit: string
  primaryIcon: 'bolt' | 'globe' | 'clock' | 'link'
  secondaryLabel: string
  secondaryValue: string
  secondaryUnit: string
  secondaryIcon: 'bolt' | 'globe' | 'clock' | 'link'
}>()
</script>
