<template>
  <AppLayout>
    <div class="mx-auto max-w-4xl space-y-6">
      <!-- 步骤指示：选择商品 → 支付中 → 结果/卡密（全程本站内闭环，不跳外部页面） -->
      <ol class="flex items-center gap-2 text-xs" data-testid="shop-steps">
        <li v-for="(step, index) in steps" :key="step.key" class="flex items-center gap-2">
          <span
            class="flex h-6 w-6 items-center justify-center rounded-full font-medium"
            :class="index <= stepIndex ? 'bg-primary-600 text-white' : 'bg-gray-200 text-gray-500 dark:bg-dark-700 dark:text-gray-300'"
          >{{ index + 1 }}</span>
          <span :class="index <= stepIndex ? 'text-gray-900 dark:text-white' : 'text-gray-400 dark:text-gray-500'">{{ step.label }}</span>
          <span v-if="index < steps.length - 1" class="text-gray-300 dark:text-dark-600">/</span>
        </li>
      </ol>

      <!-- 账户卡片 -->
      <div class="card p-5">
        <p class="text-xs font-medium text-gray-400 dark:text-gray-500">{{ t('shop.accountLabel') }}</p>
        <p class="mt-1 text-base font-semibold text-gray-900 dark:text-white">{{ user?.username || '' }}</p>
        <p class="mt-0.5 text-sm font-medium text-green-600 dark:text-green-400">
          {{ t('shop.currentBalance') }}: {{ user?.balance?.toFixed(2) || '0.00' }}
        </p>
      </div>

      <div v-if="errorMessage" class="card border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200" data-testid="shop-error">
        {{ errorMessage }}
      </div>

      <!-- 步骤 1：商品列表 -->
      <template v-if="phase === 'select'">
        <div v-if="loadingCatalog" class="card py-16 text-center text-gray-500 dark:text-gray-400">
          {{ t('shop.catalog.loading') }}
        </div>

        <!-- 未配置：明确展示「未开通」，不展示任何商品（禁止假数据） -->
        <div v-else-if="!catalog.configured" class="card py-16 text-center" data-testid="shop-not-configured">
          <Icon name="gift" size="xl" class="mx-auto mb-3 text-gray-300 dark:text-dark-600" />
          <p class="font-medium text-gray-700 dark:text-gray-200">{{ t('shop.catalog.notConfigured') }}</p>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('shop.catalog.notConfiguredHint') }}</p>
        </div>

        <!-- 同步失败：保留商品展示但暂停购买 -->
        <div v-else-if="!catalog.available" class="card py-16 text-center" data-testid="shop-sync-unavailable">
          <p class="font-medium text-amber-700 dark:text-amber-300">{{ t('shop.catalog.syncFailed') }}</p>
          <p v-if="catalog.synced_at" class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('shop.catalog.lastSyncedAt', { time: formatTime(catalog.synced_at) }) }}
          </p>
        </div>

        <div v-else-if="catalog.products.length === 0" class="card py-16 text-center text-gray-500 dark:text-gray-400" data-testid="shop-empty">
          {{ t('shop.catalog.empty') }}
        </div>

        <div v-else class="space-y-3" data-testid="shop-products">
          <div v-for="product in catalog.products" :key="product.id" class="card flex items-center gap-4 p-5">
            <div class="min-w-0 flex-1">
              <p class="truncate font-semibold text-gray-900 dark:text-white">{{ product.name }}</p>
              <p v-if="product.description" class="mt-1 line-clamp-2 text-sm text-gray-500 dark:text-gray-400">{{ product.description }}</p>
              <div class="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-gray-500 dark:text-gray-400">
                <span>{{ t('shop.product.credits') }}: {{ formatCredits(product) }}</span>
                <span v-if="stockLabel(product)">{{ stockLabel(product) }}</span>
                <span v-else>{{ t('shop.product.stockUnknown') }}</span>
              </div>
            </div>
            <div class="shrink-0 text-right">
              <p class="text-xl font-bold text-primary-600 dark:text-primary-400">¥{{ product.price_cny.toFixed(2) }}</p>
              <p v-if="product.original_price_cny" class="text-xs text-gray-400 line-through dark:text-gray-500">¥{{ product.original_price_cny.toFixed(2) }}</p>
              <button
                class="btn btn-primary mt-2"
                :disabled="!product.purchasable || submitting"
                @click="handleBuy(product)"
              >
                {{ t('shop.product.buy') }}
              </button>
            </div>
          </div>
        </div>
      </template>

      <!-- 步骤 2：支付中 -->
      <template v-else-if="phase === 'paying'">
        <div class="card p-6 text-center" data-testid="shop-paying">
          <p class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('shop.payment.title') }}</p>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            {{ order?.payment?.kind === 'redirect' ? t('shop.payment.redirectHint') : t('shop.payment.qrHint') }}
          </p>

          <img
            v-if="order?.payment?.kind === 'qr_code' && qrImage"
            :src="qrImage"
            alt=""
            class="mx-auto mt-4 h-48 w-48 rounded-lg bg-white p-2"
            data-testid="shop-qr"
          />
          <p v-else-if="order?.payment?.kind === 'redirect'" class="mt-4 text-sm text-gray-500 dark:text-gray-400">
            {{ t('shop.payment.redirectHint') }}
          </p>

          <p v-if="order?.payment?.expires_at" class="mt-3 text-xs text-gray-500 dark:text-gray-400">
            {{ t('shop.payment.expiresAt', { time: formatTime(order?.payment?.expires_at) }) }}
          </p>

          <!-- 支付成功与否**只**由服务端主动查单决定，前端按钮不构成依据 -->
          <p class="mt-4 text-xs text-gray-500 dark:text-gray-400">{{ t('shop.payment.pendingHint') }}</p>
          <button class="btn btn-primary mt-4" :disabled="submitting" @click="handleQuery()">
            {{ submitting ? t('shop.order.querying') : t('shop.order.queryNow') }}
          </button>
        </div>
      </template>

      <!-- 步骤 3：结果与卡密 -->
      <template v-else>
        <div class="card p-6" data-testid="shop-result">
          <div class="flex items-center justify-between">
            <p class="text-lg font-semibold text-gray-900 dark:text-white">{{ statusLabel(order?.display_status) }}</p>
            <span class="badge" :class="statusBadgeClass(order?.display_status)">{{ statusLabel(order?.display_status) }}</span>
          </div>

          <dl class="mt-4 space-y-2 text-sm">
            <div class="flex justify-between">
              <dt class="text-gray-500 dark:text-gray-400">{{ t('shop.order.orderNo') }}</dt>
              <dd class="font-mono text-gray-900 dark:text-white">{{ order?.order_no }}</dd>
            </div>
            <div v-if="order?.product_name" class="flex justify-between">
              <dt class="text-gray-500 dark:text-gray-400">{{ t('shop.tabs.products') }}</dt>
              <dd class="text-gray-900 dark:text-white">{{ order?.product_name }}</dd>
            </div>
            <div class="flex justify-between">
              <dt class="text-gray-500 dark:text-gray-400">{{ t('shop.order.quantity') }}</dt>
              <dd class="text-gray-900 dark:text-white">{{ order?.quantity }}</dd>
            </div>
            <div class="flex justify-between">
              <dt class="text-gray-500 dark:text-gray-400">{{ t('shop.order.amount') }}</dt>
              <dd class="font-semibold text-gray-900 dark:text-white">¥{{ (order?.amount_cny ?? 0).toFixed(2) }}</dd>
            </div>
          </dl>

          <!-- 交付异常：指定文案 + 可追踪订单号 + 人工兜底路径 -->
          <div
            v-if="order?.display_status === 'delivery_pending'"
            class="mt-4 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-100"
            data-testid="shop-delivery-pending"
          >
            <p class="font-medium">{{ t('shop.delivery.pendingTitle') }}</p>
            <p class="mt-1">{{ t('shop.delivery.pendingHint', { orderNo: order?.order_no }) }}</p>
          </div>

          <!-- 卡密就绪：展示 + 复制 + 引导到本站兑换入口 -->
          <div v-if="order?.card_secret" class="mt-4" data-testid="shop-card">
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('shop.order.cardReady') }}</p>
            <div class="mt-2 flex items-center gap-2">
              <code class="min-w-0 flex-1 truncate rounded-lg bg-gray-100 px-3 py-2 font-mono text-sm text-gray-900 dark:bg-dark-700 dark:text-white">{{ order.card_secret }}</code>
              <button class="btn btn-secondary shrink-0" @click="copyText(order?.card_secret, 'card')">
                {{ copied === 'card' ? t('shop.order.copied') : t('shop.order.copyCard') }}
              </button>
            </div>
            <button class="btn btn-primary mt-3 w-full" @click="goRedeem">{{ t('shop.order.redeemNow') }}</button>
          </div>

          <div class="mt-5 flex gap-2">
            <button class="btn btn-secondary flex-1" :disabled="submitting" @click="handleQuery()">
              {{ submitting ? t('shop.order.querying') : t('shop.order.queryNow') }}
            </button>
            <button class="btn btn-secondary flex-1" @click="backToProducts">{{ t('shop.order.back') }}</button>
          </div>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import QRCode from 'qrcode'
import { useAuthStore } from '@/stores/auth'
import { extractApiErrorMessage } from '@/utils/apiError'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { createIdempotencyKey, shopAPI, shopErrorKey } from '@/api/shop'
import type { ShopCatalog, ShopOrder, ShopProduct } from '@/api/shop'

const i18n = useI18n()
const { t } = i18n
const router = useRouter()
const authStore = useAuthStore()
const user = computed(() => authStore.user)

type Phase = 'select' | 'paying' | 'result'

const phase = ref<Phase>('select')
const catalog = ref<ShopCatalog>({ configured: false, available: false, products: [] })
const order = ref<ShopOrder | null>(null)
const loadingCatalog = ref(true)
const submitting = ref(false)
const errorMessage = ref('')
const copied = ref('')
const qrImage = ref('')

const steps = computed(() => [
  { key: 'select', label: t('shop.steps.select') },
  { key: 'paying', label: t('shop.steps.paying') },
  { key: 'result', label: t('shop.steps.result') },
])

const stepIndex = computed(() => {
  if (phase.value === 'select') return 0
  if (phase.value === 'paying') return 1
  return 2
})

function formatTime(value: string | undefined): string {
  if (!value) return ''
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return ''
  return parsed.toLocaleString()
}

function formatCredits(product: ShopProduct): string {
  const credits = product.price_cny * (product.credit_per_cny || 1)
  return credits.toFixed(2)
}

function stockLabel(product: ShopProduct): string {
  if (!product.listed) return t('shop.product.unlisted')
  if (typeof product.stock === 'number') {
    if (product.stock <= 0) return t('shop.product.outOfStock')
    return t('shop.product.stock', { count: product.stock })
  }
  return ''
}

function statusLabel(status: ShopOrder['display_status'] | undefined): string {
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

function statusBadgeClass(status: ShopOrder['display_status'] | undefined): string {
  switch (status) {
    case 'delivered':
      return 'badge-success'
    case 'delivery_pending':
      return 'badge-warning'
    default:
      return 'badge-secondary'
  }
}

// 服务端稳定错误码 → 本地化文案的映射。只映射本站错误码，
// 未命中时回落到通用文案（不把服务端内部信息透到界面）。
const SHOP_ERROR_REASONS = [
  'shop_not_configured',
  'shop_upstream_unavailable',
  'shop_upstream_rejected',
  'shop_upstream_response_invalid',
  'product_sync_unavailable',
  'shop_product_not_found',
  'shop_order_not_found',
  'shop_idempotency_key_required',
  'shop_idempotency_conflict',
  'order_delivery_pending',
  'shop_delivery_unavailable',
] as const

const shopErrorMap = computed<Record<string, string>>(() => {
  const map: Record<string, string> = {}
  for (const reason of SHOP_ERROR_REASONS) {
    map[reason] = t(shopErrorKey(reason))
  }
  return map
})

function localizedError(error: unknown): string {
  return extractApiErrorMessage(error, t('shop.errors.generic'), shopErrorMap.value)
}

async function loadCatalog() {
  loadingCatalog.value = true
  errorMessage.value = ''
  try {
    catalog.value = await shopAPI.getCatalog()
  } catch (error) {
    // 目录读取失败：不猜测状态、不展示商品，只给出可重试的提示。
    catalog.value = { configured: false, available: false, products: [] }
    errorMessage.value = localizedError(error)
  } finally {
    loadingCatalog.value = false
  }
}

async function renderQRCode(content: string) {
  qrImage.value = ''
  if (!content) return
  try {
    qrImage.value = await QRCode.toDataURL(content, { margin: 1, width: 384 })
  } catch {
    // 二维码渲染失败：不伪造二维码，交由文案提示用户。
    qrImage.value = ''
  }
}

watch(() => order.value?.payment?.qr_content, (content) => {
  if (order.value?.payment?.kind === 'qr_code' && content) {
    void renderQRCode(content)
  } else {
    qrImage.value = ''
  }
})

function applyOrder(next: ShopOrder) {
  order.value = next
  if (next.display_status === 'delivered' || next.display_status === 'delivery_pending') {
    phase.value = 'result'
  } else if (next.display_status === 'pending') {
    phase.value = next.payment && next.payment.kind !== 'none' ? 'paying' : 'result'
  } else {
    phase.value = 'result'
  }
}

async function handleBuy(product: ShopProduct) {
  if (submitting.value) return
  submitting.value = true
  errorMessage.value = ''
  try {
    const created = await shopAPI.createOrder({
      product_id: product.id,
      quantity: 1,
      idempotency_key: createIdempotencyKey(),
    })
    applyOrder(created)
  } catch (error) {
    errorMessage.value = localizedError(error)
  } finally {
    submitting.value = false
  }
}

async function handleQuery() {
  if (!order.value || submitting.value) return
  submitting.value = true
  errorMessage.value = ''
  try {
    applyOrder(await shopAPI.queryOrder(order.value.order_no))
  } catch (error) {
    errorMessage.value = localizedError(error)
  } finally {
    submitting.value = false
  }
}

async function copyText(value: string | undefined, tag: string) {
  if (!value) return
  try {
    await navigator.clipboard.writeText(value)
    copied.value = tag
    setTimeout(() => {
      if (copied.value === tag) copied.value = ''
    }, 2000)
  } catch {
    // 剪贴板不可用（非安全上下文）：保留明文可见，用户可手动复制。
  }
}

function goRedeem() {
  const path = order.value?.redeem_path || '/redeem'
  // 兑换入口是本站路由；这里只做站内跳转，绝不跳外部页面。
  void router.push(path.startsWith('/') ? path : '/redeem')
}

function backToProducts() {
  order.value = null
  phase.value = 'select'
  errorMessage.value = ''
  void loadCatalog()
}

onMounted(() => {
  void loadCatalog()
})
</script>
