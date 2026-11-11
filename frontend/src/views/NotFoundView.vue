<template>
  <!-- 404：一张撕下来的纸页，不是「玻璃卡片 + 光斑」。
       旧版有 blur-3xl 光斑、缺色的 from-/to- 渐变、rounded-2xl，
       且主视觉的 404 用的是 gray-100（与纸面几乎同色，等于不可见）。
       这里改成：暖纸面 + 一个巨大的衬线「404」作为版式主体 + 一块切角信号色牌。 -->
  <div class="relative flex min-h-screen items-center justify-center bg-paper px-4 py-16">
    <div class="relative z-10 w-full max-w-md text-center">
      <!-- 主体：衬线巨大数字，压在切角信号色牌后面一层，像书页上的一个暗印 -->
      <div class="relative mb-10 flex items-center justify-center">
        <!-- 这块巨大的「404」是版式主体，不是信息：它是纸面上的一道暗印，
             aria-hidden 且 data-decorative，对比度检查按装饰而非文本处理。 -->
        <span
          aria-hidden="true"
          data-decorative="true"
          class="readout-display select-none text-[9rem] leading-none text-rule sm:text-[12rem]"
          >404</span
        >
        <div
          class="absolute flex h-16 w-16 items-center justify-center border border-signal/25 bg-surface sm:h-20 sm:w-20"
          style="border-radius: var(--radius-cut)"
        >
          <Icon name="exclamationTriangle" size="xl" class="text-signal" />
        </div>
      </div>

      <h1 class="title-block-title mb-3">{{ t('errors.pageNotFound') }}</h1>
      <p class="lead mx-auto max-w-sm">
        {{ t('errors.pageNotFoundDescription') }}
      </p>

      <div class="mt-8 flex flex-col justify-center gap-3 sm:flex-row">
        <button type="button" class="btn btn-secondary" @click="goBack">
          <Icon name="arrowLeft" size="md" />
          {{ t('errors.goBack') }}
        </button>
        <router-link to="/dashboard" class="btn btn-primary">
          <Icon name="home" size="md" />
          {{ t('errors.goToDashboard') }}
        </router-link>
      </div>

      <p class="mt-10 text-sm text-ink-soft">
        {{ t('errors.needHelp') }}
        <a
          :href="supportUrl"
          class="ml-1 inline-flex min-h-[44px] items-center text-signal underline-offset-4 hover:underline"
        >
          {{ t('errors.contactSupport') }}
        </a>
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const router = useRouter()

/** 支持入口由部署方配置；没有配置时不留死链。 */
const supportUrl = computed(() => import.meta.env.VITE_SUPPORT_URL || '/help')

function goBack(): void {
  router.back()
}
</script>
