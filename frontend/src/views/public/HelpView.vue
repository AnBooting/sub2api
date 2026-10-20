<template>
  <div class="mx-auto w-full max-w-3xl px-4 py-8">
    <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('help.title') }}</h1>
    <p class="mt-2 text-sm text-gray-600 dark:text-gray-400">{{ t('help.subtitle') }}</p>

    <h2 class="mt-8 text-lg font-semibold text-gray-900 dark:text-white">
      {{ t('help.userTitle') }}
    </h2>
    <section v-for="section in userSections" :key="section" class="mt-6">
      <h3 class="text-base font-semibold text-gray-900 dark:text-white">
        {{ t(`help.user.${section}.title`) }}
      </h3>
      <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
        {{ t(`help.user.${section}.intro`) }}
      </p>

      <ul v-if="bulletsOf(section).length" class="mt-2 list-disc space-y-1 pl-5 text-sm text-gray-700 dark:text-gray-300">
        <li v-for="(line, key) in bulletsOf(section)" :key="key">{{ line }}</li>
      </ul>

      <table
        v-if="rowsOf(section).length"
        class="mt-3 w-full border-collapse text-left text-sm"
      >
        <thead>
          <tr class="border-b border-gray-200 text-gray-500 dark:border-dark-700">
            <th class="py-1.5 pr-3 font-medium">{{ headOf(section).t }}</th>
            <th class="py-1.5 font-medium">{{ headOf(section).d }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(row, key) in rowsOf(section)"
            :key="key"
            class="border-b border-gray-100 dark:border-dark-800"
          >
            <td class="py-1.5 pr-3 align-top text-gray-700 dark:text-gray-300">{{ row.t }}</td>
            <td class="py-1.5 align-top text-gray-600 dark:text-gray-400">{{ row.d }}</td>
          </tr>
        </tbody>
      </table>

      <dl v-if="itemsOf(section).length" class="mt-3 space-y-2 text-sm">
        <div v-for="(item, key) in itemsOf(section)" :key="key">
          <dt class="font-medium text-gray-900 dark:text-white">{{ item.q }}</dt>
          <dd class="text-gray-600 dark:text-gray-400">{{ item.a }}</dd>
        </div>
      </dl>

      <p
        v-if="noteOf(section)"
        class="mt-2 text-sm text-gray-500 dark:text-gray-500"
      >
        {{ noteOf(section) }}
      </p>
    </section>

    <h2 class="mt-8 text-lg font-semibold text-gray-900 dark:text-white">
      {{ t('help.paths.title') }}
    </h2>
    <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">{{ t('help.paths.intro') }}</p>
    <table class="mt-3 w-full border-collapse text-left text-sm">
      <thead>
        <tr class="border-b border-gray-200 text-gray-500 dark:border-dark-700">
          <th class="py-1.5 pr-3 font-medium">{{ t('help.paths.tableHead.t') }}</th>
          <th class="py-1.5 font-medium">{{ t('help.paths.tableHead.d') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="(row, key) in pathsRows()"
          :key="key"
          class="border-b border-gray-100 dark:border-dark-800"
        >
          <td class="py-1.5 pr-3 align-top text-gray-700 dark:text-gray-300">{{ row.t }}</td>
          <td class="py-1.5 align-top font-mono text-gray-600 dark:text-gray-400">{{ row.d }}</td>
        </tr>
      </tbody>
    </table>

    <details class="mt-8 rounded-lg border border-gray-200 dark:border-dark-700">
      <summary class="cursor-pointer px-4 py-3 text-sm font-semibold text-gray-900 dark:text-white">
        {{ t('help.adminTitle') }}
      </summary>
      <div class="border-t border-gray-200 px-4 py-3 dark:border-dark-700">
        <p class="text-sm text-gray-600 dark:text-gray-400">{{ t('help.adminIntro') }}</p>
        <section v-for="section in adminSections" :key="section" class="mt-5">
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">
            {{ t(`help.admin.${section}.title`) }}
          </h3>
          <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
            {{ t(`help.admin.${section}.intro`) }}
          </p>
          <ul
            v-if="adminBulletsOf(section).length"
            class="mt-2 list-disc space-y-1 pl-5 text-sm text-gray-700 dark:text-gray-300"
          >
            <li v-for="(line, key) in adminBulletsOf(section)" :key="key">{{ line }}</li>
          </ul>
          <table
            v-if="adminRowsOf(section).length"
            class="mt-3 w-full border-collapse text-left text-sm"
          >
            <thead>
              <tr class="border-b border-gray-200 text-gray-500 dark:border-dark-700">
                <th class="py-1.5 pr-3 font-medium">{{ adminHeadOf(section).t }}</th>
                <th class="py-1.5 font-medium">{{ adminHeadOf(section).d }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="(row, key) in adminRowsOf(section)"
                :key="key"
                class="border-b border-gray-100 dark:border-dark-800"
              >
                <td class="py-1.5 pr-3 align-top text-gray-700 dark:text-gray-300">{{ row.t }}</td>
                <td class="py-1.5 align-top text-gray-600 dark:text-gray-400">{{ row.d }}</td>
              </tr>
            </tbody>
          </table>
          <p
            v-if="adminNoteOf(section)"
            class="mt-2 text-sm text-gray-500 dark:text-gray-500"
          >
            {{ adminNoteOf(section) }}
          </p>
        </section>
      </div>
    </details>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { t, tm } = useI18n()

type Row = { t: string; d: string }
type QA = { q: string; a: string }

// 章节顺序与 i18n/locales/{zh,en}/help.ts 的结构保持一致。
const userSections = ['credit', 'groups', 'recharge', 'usage', 'media', 'faq', 'contact'] as const
const adminSections = ['pricing', 'scheduling', 'concurrency', 'reconciliation'] as const

function message(path: string): Record<string, unknown> {
  const raw = tm(path)
  if (raw && typeof raw === 'object' && !Array.isArray(raw)) {
    return raw as Record<string, unknown>
  }
  return {}
}

function stringsOf(path: string): string[] {
  return Object.values(message(path)).filter((value): value is string => typeof value === 'string')
}

function rowsFrom(path: string): Row[] {
  return Object.values(message(path)).filter(
    (value): value is Row =>
      !!value && typeof value === 'object' && typeof (value as Row).t === 'string'
  )
}

function itemsFrom(path: string): QA[] {
  return Object.values(message(path)).filter(
    (value): value is QA =>
      !!value && typeof value === 'object' && typeof (value as QA).q === 'string'
  )
}

function bulletsOf(section: string): string[] {
  return stringsOf(`help.user.${section}.bullets`)
}

function rowsOf(section: string): Row[] {
  return rowsFrom(`help.user.${section}.rows`)
}

function itemsOf(section: string): QA[] {
  return itemsFrom(`help.user.${section}.items`)
}

// 界面路径速查挂在 help.paths 下，与 help.user.* 平级，不在 help.user 里。
function pathsRows(): Row[] {
  return rowsFrom('help.paths.rows')
}

function headOf(section: string): Row {
  const head = message(`help.user.${section}.tableHead`) as unknown as Row
  return head && typeof head.t === 'string' ? head : { t: '', d: '' }
}

// note 是叶子字符串，tm() 直接返回该字符串本身，取不到 .note 字段。
// 注意：tm() 的泛型在模板字面量实参下会触发 TS2589，这里统一走 string 形参。
function leafOf(path: string): string {
  const raw = tm(path)
  return typeof raw === 'string' ? raw : ''
}

function noteOf(section: string): string {
  return leafOf(`help.user.${section}.note`)
}

function adminBulletsOf(section: string): string[] {
  return stringsOf(`help.admin.${section}.bullets`)
}

function adminRowsOf(section: string): Row[] {
  return rowsFrom(`help.admin.${section}.rows`)
}

function adminHeadOf(section: string): Row {
  const head = message(`help.admin.${section}.tableHead`) as unknown as Row
  return head && typeof head.t === 'string' ? head : { t: '', d: '' }
}

function adminNoteOf(section: string): string {
  return leafOf(`help.admin.${section}.note`)
}
</script>
