<template>
  <div>
    <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
      {{ t('payment.paymentMethod') }}
    </label>
    <div
      data-testid="payment-method-grid"
      class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4"
    >
      <button
        v-for="method in sortedMethods"
        :key="method.type"
        type="button"
        :title="methodLabel(method)"
        :disabled="!method.available"
        :class="[
          'relative flex h-[60px] min-w-0 flex-col items-center justify-center rounded-lg border px-3 transition-all',
          !method.available
            ? 'cursor-not-allowed border-rule bg-paper-inset opacity-50'
            : selected === method.type
              ? methodSelectedClass(method.type)
              : 'border-rule-strong bg-surface text-ink-soft hover:border-ink-faint',
        ]"
        :style="selected === method.type ? methodSelectedStyle(method.type) : undefined"
        @click="method.available && emit('select', method.type)"
      >
        <span class="flex w-full min-w-0 items-center justify-center gap-2">
          <img :src="methodIcon(method.type)" :alt="methodLabel(method)" class="h-7 w-7 shrink-0 object-contain" />
          <span class="flex min-w-0 flex-col items-start leading-none">
            <span data-testid="payment-method-label" class="block w-full truncate text-base font-semibold">
              {{ methodLabel(method) }}
            </span>
            <span
              v-if="method.fee_rate > 0"
              class="text-[10px] tracking-wide text-gray-500 dark:text-dark-400"
            >
              {{ t('payment.fee') }} {{ method.fee_rate }}%
            </span>
          </span>
        </span>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { METHOD_ORDER, isBuiltInAlipayMethod, isBuiltInWxpayMethod } from './providerConfig'
import { PAYMENT_BRAND_COLOR } from './brandColors'
import alipayIcon from '@/assets/icons/alipay.svg'
import wxpayIcon from '@/assets/icons/wxpay.svg'
import stripeIcon from '@/assets/icons/stripe.svg'
import airwallexIcon from '@/assets/icons/airwallex.svg'
import paymentIcon from '@/assets/icons/payment.svg'

export interface PaymentMethodOption {
  type: string
  display_name?: string
  fee_rate: number
  available: boolean
}

const props = defineProps<{
  methods: PaymentMethodOption[]
  selected: string
}>()

const emit = defineEmits<{
  select: [type: string]
}>()

const { t } = useI18n()

const METHOD_ICONS: Record<string, string> = {
  alipay: alipayIcon,
  wxpay: wxpayIcon,
  stripe: stripeIcon,
  airwallex: airwallexIcon,
  credit_card: paymentIcon,
}

const sortedMethods = computed(() => {
  const order: readonly string[] = METHOD_ORDER
  return [...props.methods].sort((a, b) => {
    const ai = order.indexOf(a.type)
    const bi = order.indexOf(b.type)
    return (ai === -1 ? 999 : ai) - (bi === -1 ? 999 : bi)
  })
})

function methodIcon(type: string): string {
  if (isBuiltInAlipayMethod(type)) return METHOD_ICONS.alipay
  if (isBuiltInWxpayMethod(type)) return METHOD_ICONS.wxpay
  if (type === 'airwallex') return METHOD_ICONS.airwallex
  return METHOD_ICONS[type] || paymentIcon
}

function methodLabel(method: PaymentMethodOption): string {
  return method.display_name || t(`payment.methods.${method.type}`, method.type)
}

function methodSelectedClass(type: string): string {
  // 选中态 = 信号色边框；渠道品牌识别由图标承载，不再给整块卡片刷品牌底色。
  if (isBuiltInAlipayMethod(type)) return 'bg-primary-50 text-ink dark:bg-primary-950'
  if (isBuiltInWxpayMethod(type)) return 'bg-success-50 text-ink dark:bg-success-950'
  if (type === 'stripe') return 'bg-primary-50 text-ink dark:bg-primary-950'
  if (type === 'airwallex') return 'bg-warning-50 text-ink dark:bg-warning-950'
  return 'border-signal bg-primary-50 text-ink dark:bg-primary-950'
}

/** 渠道品牌色只用于选中态的 1px 边框（见 brandColors.ts 的例外说明）。 */
function methodSelectedStyle(type: string): Record<string, string> {
  if (isBuiltInAlipayMethod(type)) return { borderColor: PAYMENT_BRAND_COLOR.alipay }
  if (isBuiltInWxpayMethod(type)) return { borderColor: PAYMENT_BRAND_COLOR.wxpay }
  if (type === 'stripe') return { borderColor: PAYMENT_BRAND_COLOR.stripe }
  if (type === 'airwallex') return { borderColor: PAYMENT_BRAND_COLOR.airwallex }
  return {}
}
</script>
