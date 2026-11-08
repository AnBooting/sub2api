<template>
  <div
    class="card border-amber-200 dark:border-amber-800/60"
    data-testid="smtp-not-configured"
    role="alert"
  >
    <div class="flex items-start gap-3 p-6">
      <Icon
        name="exclamationTriangle"
        size="md"
        class="mt-0.5 flex-shrink-0 text-amber-500 dark:text-amber-400"
      />
      <div class="min-w-0">
        <h3 class="font-medium text-gray-900 dark:text-white">
          {{ t('admin.settings.smtp.notConfiguredTitle') }}
        </h3>
        <p class="mt-1 text-sm text-gray-600 dark:text-gray-300">
          {{ t('admin.settings.smtp.notConfiguredHint') }}
        </p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * SMTP 未配置的阻断提示（I-01 契约）。
 *
 * 判定由 `utils/smtp.ts` 的 `isSmtpConfigured` 给出，本组件只负责渲染：
 * 服务端说不可用时，注册验证码 / 找回密码 / 邮箱验证 / 测试邮件都会失败，
 * 与其让管理员点一次才报错，不如直接在邮件区块前面阻断。
 */
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
</script>
