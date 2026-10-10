<template>
  <section class="rounded-xl border border-indigo-200 bg-indigo-50/60 p-4 dark:border-indigo-800/70 dark:bg-indigo-950/20">
    <label class="input-label">国内厂商</label>
    <Select v-model="selected" :options="providerOptions" />
    <div class="mt-3 flex items-center gap-2 text-sm text-indigo-800 dark:text-indigo-200">
      <ModelIcon v-if="provider?.iconModel" :model="provider.iconModel" size="18px" />
      <Icon v-else name="globe" size="sm" />
      <span class="font-semibold">{{ provider?.label }}</span>
      <span class="text-xs text-indigo-600/80 dark:text-indigo-300/80">{{ provider?.company }}</span>
    </div>
    <p class="mt-1 text-xs text-indigo-700/80 dark:text-indigo-300/80">
      底层仍使用 OpenAI 兼容路由；下列为国内官方标准价（1元 = 1美元，无汇率换算），最终价 = 基础价 × 分组倍率 × 用户专属倍率。
    </p>
    <p v-if="peakSchedule" class="mt-1 text-xs text-indigo-700/80 dark:text-indigo-300/80">
      表内价格按“谷 / 峰”展示；峰价时段：{{ peakSchedule }}。
    </p>

    <div class="mt-3 overflow-hidden rounded-lg border border-indigo-100 bg-white/80 dark:border-indigo-900 dark:bg-dark-800/80">
      <div v-if="loading" class="px-3 py-4 text-center text-xs text-gray-500">正在加载价格目录…</div>
      <div v-else-if="providerEntries.length" class="max-h-44 overflow-auto">
        <table class="w-full text-left text-xs">
          <thead class="sticky top-0 bg-indigo-50 text-indigo-700 dark:bg-indigo-950 dark:text-indigo-300">
            <tr>
              <th class="px-3 py-2 font-medium">模型</th>
              <th class="px-3 py-2 text-right font-medium">输入 $/1M{{ peakSchedule ? '（谷 / 峰）' : '' }}</th>
              <th class="px-3 py-2 text-right font-medium">输出 $/1M{{ peakSchedule ? '（谷 / 峰）' : '' }}</th>
              <th class="px-3 py-2 text-right font-medium">缓存读 $/1M{{ peakSchedule ? '（谷 / 峰）' : '' }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-for="entry in providerEntries" :key="entry.model">
              <td class="px-3 py-2 font-mono text-gray-700 dark:text-gray-200">{{ entry.model }}</td>
              <template v-if="entry.status === 'active'">
                <td class="px-3 py-2 text-right">{{ perMillionWithPeak(entry, entry.input_price_per_token) }}</td>
                <td class="px-3 py-2 text-right">{{ perMillionWithPeak(entry, entry.output_price_per_token) }}</td>
                <td class="px-3 py-2 text-right">{{ perMillionWithPeak(entry, entry.cache_read_price_per_token) }}</td>
              </template>
              <td v-else colspan="3" class="px-3 py-2 text-right font-medium text-amber-600 dark:text-amber-400">待定价</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else class="px-3 py-4 text-center text-xs font-medium text-amber-600 dark:text-amber-400">
        暂无已定价模型；未命中目录的请求将标记“待定价”并停止计费，不使用 OpenAI 基准价。
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import Select from '@/components/common/Select.vue'
import ModelIcon from '@/components/common/ModelIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import {
  DOMESTIC_PROVIDERS,
  getDomesticProvider,
  type DomesticProviderId
} from '@/utils/domesticProviders'
import type { DomesticPricingCatalogEntry } from '@/api/admin/domesticPricing'

const props = withDefaults(defineProps<{
  modelValue: DomesticProviderId
  catalog?: DomesticPricingCatalogEntry[]
  loading?: boolean
}>(), {
  catalog: () => [],
  loading: false
})

const emit = defineEmits<{ 'update:modelValue': [value: DomesticProviderId] }>()

const selected = computed({
  get: () => props.modelValue,
  set: (value: DomesticProviderId) => emit('update:modelValue', value)
})
const providerOptions = DOMESTIC_PROVIDERS.map((item) => ({
  value: item.id,
  label: `${item.label} · ${item.company}`
}))
const provider = computed(() => getDomesticProvider(props.modelValue))
const providerEntries = computed(() =>
  props.catalog.filter((entry) => entry.provider === props.modelValue)
)
const peakSchedule = computed(() =>
  providerEntries.value.find((entry) => entry.peak_schedule)?.peak_schedule ?? ''
)

const perMillion = (value?: number | null) => {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) return '待定价'
  return (value * 1_000_000).toLocaleString('en-US', { maximumFractionDigits: 6 })
}

const perMillionWithPeak = (entry: DomesticPricingCatalogEntry, value?: number | null) => {
  const base = perMillion(value)
  const multiplier = entry.peak_rate_multiplier
  if (base === '待定价' || typeof multiplier !== 'number' || multiplier <= 1 || value == null) return base
  return `${base} / ${perMillion(value * multiplier)}`
}
</script>
