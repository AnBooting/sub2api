<template>
  <header class="sticky top-0 z-30 border-b border-rule bg-surface/95 backdrop-blur-sm">
    <div class="flex h-14 items-center justify-between gap-2 px-3 sm:h-[3.75rem] sm:px-5 lg:px-7">
      <!-- 左：图签。移动端只留一行业务标题，不重复侧栏已表达的信息 -->
      <div class="flex min-w-0 shrink items-center gap-2">
        <button
          @click="toggleMobileSidebar"
          class="btn btn-ghost btn-icon lg:hidden"
          :aria-label="t('common.toggleMenu')"
        >
          <Icon name="menu" size="md" />
        </button>

        <div class="min-w-0">
          <h1 class="truncate text-[0.9375rem] font-semibold text-ink sm:text-base">
            {{ pageTitle }}
          </h1>
          <p
            v-if="pageDescription"
            class="hidden truncate text-xs text-ink-soft sm:block"
          >
            {{ pageDescription }}
          </p>
        </div>
      </div>

      <!-- 右：读数与操作。按重要度从左到右：文档 → 语言 → 订阅 → 余额 → 用户 -->
      <div class="flex min-w-0 items-center gap-0.5 sm:gap-1.5">
        <AnnouncementBell v-if="user" />

        <a
          v-if="docUrl"
          :href="docUrl"
          target="_blank"
          rel="noopener noreferrer"
          class="btn btn-ghost hidden sm:inline-flex"
          :title="t('nav.docs')"
        >
          <Icon name="book" size="sm" />
          <span class="hidden md:inline">{{ t('nav.docs') }}</span>
        </a>

        <router-link
          v-if="user && modelPlazaEnabled"
          :to="{ path: '/model-plaza', query: { embedded: '1' } }"
          :title="t('nav.modelPlaza')"
          :aria-label="t('nav.modelPlaza')"
          class="btn btn-ghost hidden sm:inline-flex"
        >
          <Icon name="grid" size="sm" />
          <span class="hidden md:inline">{{ t('nav.modelPlaza') }}</span>
        </router-link>

        <LocaleSwitcher />

        <SubscriptionProgressMini v-if="user && subscriptionFeatureEnabled" />

        <!-- 余额：读数，不是装饰。等宽数字 + 痕量线框 -->
        <div
          v-if="user"
          class="group relative hidden items-center gap-1.5 border border-rule px-2.5 py-1 sm:flex"
          style="border-radius: var(--radius-cut)"
        >
          <span class="font-read text-[0.6875rem] text-ink-faint">{{ balanceAvailableText }}</span>
          <span class="font-read text-[0.8125rem] font-semibold text-ink">
            {{ formatHeaderMoney(availableBalance) }}
          </span>
          <span
            v-if="frozenBalance > 0"
            class="badge badge-warning"
          >
            {{ balanceFrozenLabel }}
          </span>
          <div
            class="pointer-events-none absolute right-0 top-full z-50 mt-1.5 hidden w-56 border border-rule-strong bg-surface p-3 text-xs group-hover:block"
            style="border-radius: var(--radius-cut); box-shadow: 0 18px 40px -24px rgb(0 0 0 / 0.45)"
          >
            <div class="flex items-center justify-between gap-3">
              <span class="text-ink-soft">{{ balanceAvailableText }}</span>
              <span class="font-read font-medium text-ink">{{ formatHeaderMoney(availableBalance) }}</span>
            </div>
            <div class="mt-2 flex items-center justify-between gap-3">
              <span class="text-ink-soft">{{ balanceFrozenText }}</span>
              <span class="font-read font-medium text-warning-600">{{ formatHeaderMoney(frozenBalance) }}</span>
            </div>
            <div class="mt-2 flex items-center justify-between gap-3 border-t border-rule pt-2">
              <span class="text-ink-soft">{{ balanceTotalText }}</span>
              <span class="font-read font-semibold text-ink">{{ formatHeaderMoney(totalBalance) }}</span>
            </div>
          </div>
        </div>

        <!-- 用户菜单 -->
        <div v-if="user" class="relative" ref="dropdownRef">
          <button
            @click="toggleDropdown"
            class="flex items-center gap-2 p-1 transition-colors hover:bg-paper-inset"
            style="border-radius: var(--radius-cut)"
            :aria-label="t('common.userMenu')"
            :aria-expanded="dropdownOpen"
          >
            <span class="flex h-7 w-7 items-center justify-center overflow-hidden bg-primary-600 font-read text-[0.6875rem] font-semibold text-on-signal" style="border-radius: var(--radius-cut)">
              <img
                v-if="avatarUrl"
                :src="avatarUrl"
                :alt="displayName"
                class="h-full w-full object-cover"
              >
              <span v-else>{{ userInitials }}</span>
            </span>
            <span class="hidden text-left md:block">
              <span class="block max-w-[9rem] truncate text-[0.8125rem] font-medium text-ink">
                {{ displayName }}
              </span>
            </span>
            <Icon name="chevronDown" size="sm" class="hidden text-ink-faint md:block" />
          </button>

          <transition name="dropdown">
            <div v-if="dropdownOpen" class="dropdown right-0 mt-1.5 w-60">
              <div class="border-b border-rule px-3 py-2.5">
                <div class="truncate text-[0.8125rem] font-medium text-ink">{{ displayName }}</div>
                <div class="truncate font-read text-[0.6875rem] text-ink-faint">{{ user.email }}</div>
                <div class="mt-1 font-read text-[0.6875rem] text-ink-soft">
                  {{ t('admin.users.roles.' + user.role) }}
                </div>
              </div>

              <!-- 移动端：余额在这里也可见 -->
              <div class="border-b border-rule px-3 py-2 sm:hidden">
                <div class="font-read text-[0.6875rem] text-ink-faint">{{ balanceAvailableText }}</div>
                <div class="font-read text-[0.9375rem] font-semibold text-ink">
                  {{ formatHeaderMoney(availableBalance) }}
                </div>
                <div v-if="frozenBalance > 0" class="mt-0.5 font-read text-[0.6875rem] text-warning-600">
                  {{ balanceFrozenText }} {{ formatHeaderMoney(frozenBalance) }}
                </div>
              </div>

              <div class="py-1">
                <router-link to="/profile" @click="closeDropdown" class="dropdown-item">
                  <Icon name="user" size="sm" />
                  {{ t('nav.profile') }}
                </router-link>

                <router-link to="/keys" @click="closeDropdown" class="dropdown-item">
                  <Icon name="key" size="sm" />
                  {{ t('nav.apiKeys') }}
                </router-link>

                <a
                  v-if="authStore.isAdmin"
                  href="https://github.com/Wei-Shaw/sub2api"
                  target="_blank"
                  rel="noopener noreferrer"
                  @click="closeDropdown"
                  class="dropdown-item"
                >
                  <svg class="h-4 w-4" fill="currentColor" viewBox="0 0 24 24">
                    <path
                      fill-rule="evenodd"
                      clip-rule="evenodd"
                      d="M12 2C6.477 2 2 6.477 2 12c0 4.42 2.865 8.17 6.839 9.49.5.092.682-.217.682-.482 0-.237-.008-.866-.013-1.7-2.782.604-3.369-1.34-3.369-1.34-.454-1.156-1.11-1.464-1.11-1.464-.908-.62.069-.608.069-.608 1.003.07 1.531 1.03 1.531 1.03.892 1.529 2.341 1.087 2.91.831.092-.646.35-1.086.636-1.336-2.22-.253-4.555-1.11-4.555-4.943 0-1.091.39-1.984 1.029-2.683-.103-.253-.446-1.27.098-2.647 0 0 .84-.269 2.75 1.025A9.578 9.578 0 0112 6.836c.85.004 1.705.114 2.504.336 1.909-1.294 2.747-1.025 2.747-1.025.546 1.377.203 2.394.1 2.647.64.699 1.028 1.592 1.028 2.683 0 3.842-2.339 4.687-4.566 4.935.359.309.678.919.678 1.852 0 1.336-.012 2.415-.012 2.743 0 .267.18.578.688.48C19.138 20.167 22 16.418 22 12c0-5.523-4.477-10-10-10z"
                    />
                  </svg>
                  {{ t('nav.github') }}
                </a>
              </div>

              <div
                v-if="contactInfo"
                class="border-t border-rule px-3 py-2"
              >
                <div class="flex items-start gap-2 text-[0.6875rem] text-ink-soft">
                  <span class="shrink-0">{{ t('common.contactSupport') }}</span>
                  <span class="min-w-0 break-all font-medium text-ink">{{ contactInfo }}</span>
                </div>
              </div>

              <div v-if="showOnboardingButton" class="border-t border-rule py-1">
                <button @click="handleReplayGuide" class="dropdown-item w-full">
                  <Icon name="questionCircle" size="sm" />
                  {{ $t('onboarding.restartTour') }}
                </button>
              </div>

              <div class="border-t border-rule py-1">
                <button
                  @click="handleLogout"
                  class="dropdown-item w-full text-danger-600 hover:bg-danger-50 dark:hover:bg-danger-950/40"
                >
                  <Icon name="login" size="sm" />
                  {{ t('nav.logout') }}
                </button>
              </div>
            </div>
          </transition>
        </div>
      </div>
    </div>
  </header>
</template>
<script setup lang="ts">
import { formatCustomerCredit } from '@/utils/billingCredits'
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore, useOnboardingStore } from '@/stores'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import SubscriptionProgressMini from '@/components/common/SubscriptionProgressMini.vue'
import AnnouncementBell from '@/components/common/AnnouncementBell.vue'
import Icon from '@/components/icons/Icon.vue'
import { sanitizeUrl } from '@/utils/url'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import { resolveRouteMetaKeys } from '@/router/title'
import { resolveSiteBillingMode } from '@/utils/siteBillingMode'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const adminSettingsStore = useAdminSettingsStore()
const onboardingStore = useOnboardingStore()

const user = computed(() => authStore.user)
const dropdownOpen = ref(false)
const dropdownRef = ref<HTMLElement | null>(null)
const contactInfo = computed(() => appStore.contactInfo)
const docUrl = computed(() => sanitizeUrl(appStore.docUrl))
const modelPlazaEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.modelPlaza))
const avatarUrl = computed(() => user.value?.avatar_url?.trim() || '')
const availableBalance = computed(() => Number(user.value?.balance || 0))
const frozenBalance = computed(() => Number(user.value?.frozen_balance || 0))
const totalBalance = computed(() => availableBalance.value + frozenBalance.value)
const balanceAvailableText = computed(() => t('common.availableBalance') === 'common.availableBalance' ? '可用余额' : t('common.availableBalance'))
const balanceFrozenText = computed(() => t('common.frozenBalance') === 'common.frozenBalance' ? '冻结金额' : t('common.frozenBalance'))
const balanceTotalText = computed(() => t('common.totalBalance') === 'common.totalBalance' ? '总余额' : t('common.totalBalance'))
const balanceFrozenLabel = computed(() => `${balanceFrozenText.value} ${formatHeaderMoney(frozenBalance.value)}`)

// 只在标准模式的管理员下显示新手引导按钮
const showOnboardingButton = computed(() => {
  return !authStore.isSimpleMode && user.value?.role === 'admin'
})

const userInitials = computed(() => {
  if (!user.value) return ''
  // Prefer username, fallback to email
  if (user.value.username) {
    return user.value.username.substring(0, 2).toUpperCase()
  }
  if (user.value.email) {
    // Get the part before @ and take first 2 chars
    const localPart = user.value.email.split('@')[0]
    return localPart.substring(0, 2).toUpperCase()
  }
  return ''
})

const displayName = computed(() => {
  if (!user.value) return ''
  return user.value.username || user.value.email?.split('@')[0] || ''
})

// 订阅功能关闭时不挂载顶栏订阅徽章（组件 onMounted 会拉取订阅接口）。
const subscriptionFeatureEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.subscription))

// /purchase 的标题/描述随站点计费模式切换，与 document.title 共用同一解析。
const routeMetaKeys = computed(() => resolveRouteMetaKeys(route, {
  billingMode: resolveSiteBillingMode(appStore.cachedPublicSettings),
}))

const pageTitle = computed(() => {
  // For custom pages, use the menu item's label instead of generic "自定义页面"
  if (route.name === 'CustomPage') {
    const id = route.params.id as string
    const publicItems = appStore.cachedPublicSettings?.custom_menu_items ?? []
    const menuItem = publicItems.find((item) => item.id === id)
      ?? (authStore.isAdmin ? adminSettingsStore.customMenuItems.find((item) => item.id === id) : undefined)
    if (menuItem?.label) return menuItem.label
  }
  const titleKey = routeMetaKeys.value.titleKey
  if (titleKey) {
    return t(titleKey)
  }
  return (route.meta.title as string) || ''
})

const pageDescription = computed(() => {
  const descKey = routeMetaKeys.value.descriptionKey
  if (descKey) {
    return t(descKey)
  }
  return (route.meta.description as string) || ''
})

function toggleMobileSidebar() {
  appStore.toggleMobileSidebar()
}

function toggleDropdown() {
  dropdownOpen.value = !dropdownOpen.value
}

function closeDropdown() {
  dropdownOpen.value = false
}

async function handleLogout() {
  closeDropdown()
  try {
    await authStore.logout()
  } catch (error) {
    // Ignore logout errors - still redirect to login
    console.error('Logout error:', error)
  }
  await router.push('/login')
}

function handleReplayGuide() {
  closeDropdown()
  onboardingStore.replay()
}

function formatHeaderMoney(value: number) {
  return formatCustomerCredit(value)
}

function handleClickOutside(event: MouseEvent) {
  if (dropdownRef.value && !dropdownRef.value.contains(event.target as Node)) {
    closeDropdown()
  }
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
})
</script>

<style scoped>
.dropdown-enter-active,
.dropdown-leave-active {
  transition: all 0.2s ease;
}

.dropdown-enter-from,
.dropdown-leave-to {
  opacity: 0;
  transform: scale(0.95) translateY(-4px);
}
</style>
