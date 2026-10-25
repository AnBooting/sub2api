<template>
  <div class="space-y-4" data-testid="shop-orders-panel">
    <!-- 订单详情（刷新后按 URL / 本地快照回查原订单就落在这里） -->
    <div v-if="detail" class="card p-5" data-testid="shop-order-detail">
      <div class="flex items-center justify-between gap-2">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
          {{ t('shop.orders.detailTitle') }}
        </h3>
        <button class="btn btn-secondary btn-sm" data-testid="shop-order-back" @click="backToList">
          {{ t('shop.orders.backToList') }}
        </button>
      </div>

      <dl class="mt-3 space-y-2 text-sm">
        <div class="flex justify-between gap-3">
          <dt class="text-gray-500 dark:text-gray-400">{{ t('shop.order.orderNo') }}</dt>
          <dd class="font-mono text-gray-900 dark:text-white">{{ detail.order_no }}</dd>
        </div>
        <div v-if="detail.product_name" class="flex justify-between gap-3">
          <dt class="text-gray-500 dark:text-gray-400">{{ t('shop.tabs.products') }}</dt>
          <dd class="text-right text-gray-900 dark:text-white">{{ detail.product_name }}</dd>
        </div>
        <div class="flex justify-between gap-3">
          <dt class="text-gray-500 dark:text-gray-400">{{ t('shop.order.quantity') }}</dt>
          <dd class="text-gray-900 dark:text-white">{{ detail.quantity }}</dd>
        </div>
        <div class="flex justify-between gap-3">
          <dt class="text-gray-500 dark:text-gray-400">{{ t('shop.order.amount') }}</dt>
          <dd class="font-semibold text-gray-900 dark:text-white">
            {{ formatOrderAmount(detail.amount_cny) }}
          </dd>
        </div>
        <div class="flex justify-between gap-3">
          <dt class="text-gray-500 dark:text-gray-400">{{ t('shop.order.status') }}</dt>
          <dd>
            <span class="badge" :class="statusBadgeClass(detail.display_status)" data-testid="shop-order-status">
              {{ statusLabel(detail.display_status) }}
            </span>
          </dd>
        </div>
        <div v-if="detail.created_at" class="flex justify-between gap-3">
          <dt class="text-gray-500 dark:text-gray-400">{{ t('shop.order.createdAt') }}</dt>
          <dd class="text-right text-gray-900 dark:text-white">{{ formatTime(detail.created_at) }}</dd>
        </div>
        <div v-if="detail.paid_at" class="flex justify-between gap-3">
          <dt class="text-gray-500 dark:text-gray-400">{{ t('shop.orders.paidAt') }}</dt>
          <dd class="text-right text-gray-900 dark:text-white">{{ formatTime(detail.paid_at) }}</dd>
        </div>
      </dl>

      <!-- 交付异常：可追踪订单号 + 人工兜底路径 -->
      <div
        v-if="detail.display_status === 'delivery_pending'"
        class="mt-4 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-100"
        data-testid="shop-order-delivery-pending"
      >
        <p class="font-medium">{{ t('shop.delivery.pendingTitle') }}</p>
        <p class="mt-1">{{ t('shop.delivery.pendingHint', { orderNo: detail.order_no }) }}</p>
      </div>

      <!-- 卡密：仅本人可见 -->
      <div v-if="detail.card_secret" class="mt-4" data-testid="shop-order-card">
        <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('shop.order.cardReady') }}</p>
        <div class="mt-2 flex items-center gap-2">
          <code class="min-w-0 flex-1 truncate rounded-lg bg-gray-100 px-3 py-2 font-mono text-sm text-gray-900 dark:bg-dark-700 dark:text-white">
            {{ detail.card_secret }}
          </code>
          <button class="btn btn-secondary shrink-0" @click="copyCard">
            {{ copied ? t('shop.order.copied') : t('shop.order.copyCard') }}
          </button>
        </div>
        <button class="btn btn-primary mt-3 w-full" @click="goRedeem">{{ t('shop.order.redeemNow') }}</button>
      </div>

      <div class="mt-5 flex gap-2">
        <button
          class="btn btn-secondary flex-1"
          :disabled="querying"
          data-testid="shop-order-query"
          @click="queryDetail"
        >
          {{ querying ? t('shop.order.querying') : t('shop.order.queryNow') }}
        </button>
      </div>
    </div>

    <!-- 订单列表 -->
    <template v-else>
      <div class="flex items-center justify-between gap-2">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('shop.orders.title') }}</h3>
        <button
          class="btn btn-secondary btn-sm"
          :disabled="loading"
          data-testid="shop-orders-refresh"
          @click="loadOrders"
        >
          {{ t('shop.order.refresh') }}
        </button>
      </div>

      <p v-if="errorMessage" class="card border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200" data-testid="shop-orders-error">
        {{ errorMessage }}
      </p>

      <div v-if="loading && !orders.length" class="card py-10 text-center text-sm text-gray-500 dark:text-gray-400">
        {{ t('shop.orders.loading') }}
      </div>

      <div v-else-if="!orders.length" class="card py-10 text-center text-sm text-gray-500 dark:text-gray-400" data-testid="shop-orders-empty">
        {{ t('shop.orders.empty') }}
      </div>

      <ul v-else class="space-y-2" data-testid="shop-orders-list">
        <li v-for="row in orders" :key="row.order_no">
          <button
            type="button"
            class="card flex w-full items-center justify-between gap-3 p-4 text-left transition-colors hover:bg-gray-50 dark:hover:bg-dark-800"
            :data-testid="'shop-order-row-' + row.order_no"
            @click="openDetail(row.order_no)"
          >
            <span class="min-w-0">
              <span class="block truncate font-mono text-sm text-gray-900 dark:text-white">{{ row.order_no }}</span>
              <span class="mt-0.5 block truncate text-xs text-gray-500 dark:text-gray-400">
                {{ row.product_name || row.product_id }}
                <template v-if="row.created_at"> · {{ formatTime(row.created_at) }}</template>
              </span>
            </span>
            <span class="flex shrink-0 items-center gap-2">
              <span class="text-sm font-semibold text-gray-900 dark:text-white">{{ formatOrderAmount(row.amount_cny) }}</span>
              <span class="badge" :class="statusBadgeClass(row.display_status)">{{ statusLabel(row.display_status) }}</span>
            </span>
          </button>
        </li>
      </ul>
    </template>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { shopAPI } from '@/api/shop'
import type { ShopOrder, ShopOrderDisplayStatus } from '@/api/shop'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ initialOrderNo?: string | null }>()
const emit = defineEmits<{
  (e: 'selected', orderNo: string | null): void
}>()

const { t } = useI18n()
const router = useRouter()

const orders = ref<ShopOrder[]>([])
const detail = ref<ShopOrder | null>(null)
const loading = ref(false)
const querying = ref(false)
const errorMessage = ref('')
const copied = ref(false)

function formatTime(value: string | undefined): string {
  if (!value) return ''
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return ''
  return parsed.toLocaleString()
}

/** 实付金额是人民币价格（不是账户 Credit），因此保留货币符号而不是 Credit 单位。 */
function formatOrderAmount(amount: number | undefined): string {
  return `¥${(amount ?? 0).toFixed(2)}`
}

function statusLabel(status: ShopOrderDisplayStatus | undefined): string {
  switch (status) {
    case 'pending':
      return t('shop.status.pending')
    case 'paid':
      return t('shop.status.paid')
    case 'delivery_pending':
      return t('shop.status.delivery_pending')
    case 'delivered':
      return t('shop.status.delivered')
    default:
      return ''
  }
}

function statusBadgeClass(status: ShopOrderDisplayStatus | undefined): string {
  switch (status) {
    case 'delivered':
      return 'badge-success'
    case 'delivery_pending':
      return 'badge-warning'
    default:
      return 'badge-secondary'
  }
}

function localizedError(error: unknown): string {
  return extractApiErrorMessage(error, t('shop.errors.generic'))
}

async function loadOrders() {
  loading.value = true
  errorMessage.value = ''
  try {
    orders.value = await shopAPI.listOrders()
  } catch (error) {
    orders.value = []
    errorMessage.value = localizedError(error)
  } finally {
    loading.value = false
  }
}

async function openDetail(orderNo: string) {
  if (!orderNo) return
  loading.value = true
  errorMessage.value = ''
  try {
    detail.value = await shopAPI.getOrder(orderNo)
    emit('selected', detail.value.order_no)
  } catch (error) {
    detail.value = null
    errorMessage.value = localizedError(error)
  } finally {
    loading.value = false
  }
}

async function queryDetail() {
  if (!detail.value || querying.value) return
  querying.value = true
  errorMessage.value = ''
  try {
    detail.value = await shopAPI.queryOrder(detail.value.order_no)
    emit('selected', detail.value.order_no)
  } catch (error) {
    errorMessage.value = localizedError(error)
  } finally {
    querying.value = false
  }
}

function backToList() {
  detail.value = null
  emit('selected', null)
}

async function copyCard() {
  const secret = detail.value?.card_secret
  if (!secret) return
  try {
    await navigator.clipboard.writeText(secret)
    copied.value = true
    setTimeout(() => {
      copied.value = false
    }, 2000)
  } catch {
    // 剪贴板不可用（非安全上下文）：保留明文可见，用户可手动复制。
  }
}

function goRedeem() {
  const path = detail.value?.redeem_path || '/redeem'
  void router.push(path.startsWith('/') ? path : '/redeem')
}

onMounted(async () => {
  await loadOrders()
  // 刷新后回查：URL 参数优先，其次是本地快照。
  const target = props.initialOrderNo?.trim()
  if (target) {
    await openDetail(target)
  }
})
</script>
