<template>
  <header
    class="sticky top-0 z-30 border-b border-borderline-200 bg-surface-50/95 backdrop-blur-sm dark:border-dark-700 dark:bg-dark-900/95"
    role="banner"
  >
    <div class="flex h-14 items-center justify-between gap-2 px-3 sm:px-5 md:px-6">
      <div class="flex min-w-0 shrink items-center gap-2 sm:gap-3">
        <button
          @click="toggleMobileSidebar"
          class="btn-ghost btn-icon lg:hidden"
          :aria-label="t('common.toggleMenu')"
        >
          <Icon name="menu" size="md" />
        </button>

        <div class="min-w-0">
          <h1 class="truncate text-[15px] font-semibold tracking-tight text-ink-900 dark:text-white">
            {{ pageTitle }}
          </h1>
          <p v-if="pageDescription && isAdmin" class="truncate text-xs text-ink-500 dark:text-dark-400">
            {{ pageDescription }}
          </p>
        </div>
      </div>

      <div class="flex min-w-0 shrink-0 items-center gap-1 sm:gap-2">
        <a
          v-if="docUrl && !isAdmin"
          :href="docUrl"
          target="_blank"
          rel="noopener noreferrer"
          class="hidden items-center gap-1.5 rounded-control px-2 py-1.5 text-sm font-medium text-ink-600 hover:bg-surface-200 hover:text-ink-900 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white sm:flex"
        >
          <Icon name="book" size="sm" />
          <span class="hidden lg:inline">{{ t('nav.docs') }}</span>
        </a>

        <router-link
          v-if="user && !isAdmin && modelPlazaEnabled"
          :to="{ path: '/model-plaza', query: { embedded: '1' } }"
          :title="t('nav.modelPlaza')"
          :aria-label="t('nav.modelPlaza')"
          class="hidden items-center gap-1.5 rounded-control px-2 py-1.5 text-sm font-medium text-ink-600 hover:bg-surface-200 hover:text-ink-900 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white sm:flex"
        >
          <Icon name="grid" size="sm" />
          <span class="hidden lg:inline">{{ t('nav.modelPlaza') }}</span>
        </router-link>

        <AnnouncementBell v-if="user" />

        <LocaleSwitcher />

        <SubscriptionProgressMini v-if="user && !isAdmin && subscriptionFeatureEnabled" />

        <div
          v-if="user && !isAdmin"
          class="hidden items-center gap-1.5 rounded-control bg-surface-200 px-2.5 py-1.5 text-sm font-medium text-ink-700 dark:bg-dark-800 dark:text-gray-200 sm:flex"
          :title="balanceSummaryTitle"
        >
          <Icon name="dollar" size="sm" class="text-ink-500 dark:text-dark-400" />
          <span class="font-semibold">{{ formatHeaderMoney(availableBalance) }}</span>
          <span v-if="frozenBalance > 0" class="rounded-control bg-amber-100 px-1.5 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/40 dark:text-amber-200">
            {{ balanceFrozenLabel }}
          </span>
        </div>

        <div v-if="user" class="relative" ref="dropdownRef">
          <button
            @click="toggleDropdown"
            class="flex items-center gap-2 rounded-control p-1 transition-colors hover:bg-surface-200 dark:hover:bg-dark-800"
            :aria-label="t('common.userMenu')"
          >
            <div class="flex h-8 w-8 items-center justify-center overflow-hidden rounded-panel bg-primary-600 text-sm font-medium text-white">
              <img
                v-if="avatarUrl"
                :src="avatarUrl"
                :alt="displayName"
                class="h-full w-full object-cover"
              >
              <span v-else>{{ userInitials }}</span>
            </div>
            <div class="hidden text-left md:block">
              <div class="max-w-32 truncate text-sm font-medium text-ink-900 dark:text-white">
                {{ displayName }}
              </div>
              <div v-if="isAdmin" class="text-xs text-ink-500 dark:text-dark-400">
                {{ t('admin.users.roles.admin') }}
              </div>
            </div>
            <Icon name="chevronDown" size="sm" class="hidden text-ink-400 md:block" />
          </button>

          <transition name="dropdown">
            <div v-if="dropdownOpen" class="dropdown right-0 mt-2 w-56" role="menu">
              <div class="border-b border-borderline-100 px-3.5 py-2.5 dark:border-dark-700">
                <div class="truncate text-sm font-medium text-ink-900 dark:text-white">
                  {{ displayName }}
                </div>
                <div class="truncate text-xs text-ink-500 dark:text-dark-400">{{ user.email }}</div>
              </div>

              <div v-if="user && !isAdmin" class="border-b border-borderline-100 px-3.5 py-2 dark:border-dark-700 sm:hidden">
                <div class="text-xs text-ink-500 dark:text-dark-400">{{ t('common.balance') }}</div>
                <div class="text-sm font-semibold text-primary-600 dark:text-primary-400">
                  {{ formatHeaderMoney(availableBalance) }}
                </div>
                <div v-if="frozenBalance > 0" class="mt-1 text-xs text-amber-600 dark:text-amber-300">
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

                <template v-if="!isAdmin">
                  <router-link v-if="paymentFeatureEnabled" to="/purchase" @click="closeDropdown" class="dropdown-item">
                    <Icon name="creditCard" size="sm" />
                    {{ purchaseLabel }}
                  </router-link>
                  <router-link v-if="subscriptionFeatureEnabled" to="/subscriptions" @click="closeDropdown" class="dropdown-item">
                    <Icon name="grid" size="sm" />
                    {{ t('nav.mySubscriptions') }}
                  </router-link>
                </template>
              </div>

              <div v-if="contactInfo" class="border-t border-borderline-100 px-3.5 py-2.5 dark:border-dark-700">
                <div class="flex items-center gap-2 text-xs text-ink-500 dark:text-gray-400">
                  <Icon name="chat" size="sm" />
                  <span class="truncate">{{ contactInfo }}</span>
                </div>
              </div>

              <div v-if="showOnboardingButton" class="border-t border-borderline-100 py-1 dark:border-dark-700">
                <button @click="handleReplayGuide" class="dropdown-item w-full">
                  <Icon name="questionCircle" size="sm" />
                  {{ $t('onboarding.restartTour') }}
                </button>
              </div>

              <div class="border-t border-borderline-100 py-1 dark:border-dark-700">
                <button
                  @click="handleLogout"
                  class="dropdown-item w-full text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/20"
                >
                  <Icon name="login" size="sm" class="rotate-180" />
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
const isAdmin = computed(() => authStore.isAdmin)
const dropdownOpen = ref(false)
const dropdownRef = ref<HTMLElement | null>(null)
const contactInfo = computed(() => appStore.contactInfo)
const docUrl = computed(() => sanitizeUrl(appStore.docUrl))
const modelPlazaEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.modelPlaza))
const subscriptionFeatureEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.subscription))
const paymentFeatureEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.payment))
const avatarUrl = computed(() => user.value?.avatar_url?.trim() || '')
const availableBalance = computed(() => Number(user.value?.balance || 0))
const frozenBalance = computed(() => Number(user.value?.frozen_balance || 0))
const balanceAvailableText = computed(() => t('common.availableBalance'))
const balanceFrozenText = computed(() => t('common.frozenBalance'))
const balanceFrozenLabel = computed(() => balanceFrozenText.value + ' ' + formatHeaderMoney(frozenBalance.value))

const balanceSummaryTitle = computed(() => {
  const base = balanceAvailableText.value + ': ' + formatHeaderMoney(availableBalance.value)
  if (frozenBalance.value > 0) {
    return base + ' / ' + balanceFrozenText.value + ': ' + formatHeaderMoney(frozenBalance.value)
  }
  return base
})

const purchaseLabel = computed(() => {
  switch (resolveSiteBillingMode(appStore.cachedPublicSettings)) {
    case 'recharge_only':
      return t('nav.recharge')
    case 'subscription_only':
      return t('nav.subscribe')
    default:
      return t('nav.buySubscription')
  }
})

const showOnboardingButton = computed(() => {
  return !authStore.isSimpleMode && isAdmin.value
})

const userInitials = computed(() => {
  if (!user.value) return ''
  if (user.value.username) {
    return user.value.username.substring(0, 2).toUpperCase()
  }
  if (user.value.email) {
    const localPart = user.value.email.split('@')[0]
    return localPart.substring(0, 2).toUpperCase()
  }
  return ''
})

const displayName = computed(() => {
  if (!user.value) return ''
  return user.value.username || user.value.email?.split('@')[0] || ''
})

const routeMetaKeys = computed(() => resolveRouteMetaKeys(route, {
  billingMode: resolveSiteBillingMode(appStore.cachedPublicSettings),
}))

const pageTitle = computed(() => {
  if (route.name === 'CustomPage') {
    const id = route.params.id as string
    const publicItems = appStore.cachedPublicSettings?.custom_menu_items ?? []
    const menuItem = publicItems.find((item) => item.id === id)
      ?? (isAdmin.value ? adminSettingsStore.customMenuItems.find((item) => item.id === id) : undefined)
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
  transition: opacity 0.16s ease, transform 0.16s ease;
}

.dropdown-enter-from,
.dropdown-leave-to {
  opacity: 0;
  transform: scale(0.98) translateY(-3px);
}
</style>
