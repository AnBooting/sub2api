<template>
  <div class="flex flex-col gap-3 border-t border-rule px-3 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-4">
    <!-- 桌面：读数 + 每页条数 + 跳页，然后才是页码 -->
    <div class="hidden items-center gap-4 sm:flex">
      <p class="font-read text-[0.8125rem] text-ink-faint">
        <span class="text-ink">{{ fromItem }}</span>–<span class="text-ink">{{ toItem }}</span>
        / {{ total }}
      </p>

      <div v-if="showPageSizeSelector" class="flex items-center gap-2">
        <span class="font-read text-[0.6875rem] text-ink-faint">{{ t('pagination.perPage') }}</span>
        <div class="page-size-select w-20">
          <Select
            :model-value="pageSize"
            :options="pageSizeSelectOptions"
            @update:model-value="handlePageSizeChange"
          />
        </div>
      </div>

      <div v-if="showJump" class="flex items-center gap-2">
        <span class="font-read text-[0.6875rem] text-ink-faint">{{ t('pagination.jumpTo') }}</span>
        <input
          v-model="jumpPage"
          type="number"
          min="1"
          :max="totalPages"
          class="input w-20 text-sm"
          :placeholder="t('pagination.jumpPlaceholder')"
          @keyup.enter="submitJump"
        />
        <button type="button" class="btn btn-secondary btn-sm" @click="submitJump">
          {{ t('pagination.jumpAction') }}
        </button>
      </div>
    </div>

    <!-- 手机：上一页 / 页码读数 / 下一页，全部达到 44px 触控下限 -->
    <div class="flex items-center justify-between sm:hidden">
      <button
        type="button"
        class="btn btn-secondary"
        :disabled="page <= 1"
        :aria-label="t('pagination.previous')"
        @click="goToPage(page - 1)"
      >
        <Icon name="chevronLeft" size="md" />
        <span>{{ t('pagination.previous') }}</span>
      </button>
      <span class="font-read text-[0.8125rem] text-ink-faint">
        {{ page }} / {{ Math.max(totalPages, 1) }}
      </span>
      <button
        type="button"
        class="btn btn-secondary"
        :disabled="page >= totalPages"
        :aria-label="t('pagination.next')"
        @click="goToPage(page + 1)"
      >
        <span>{{ t('pagination.next') }}</span>
        <Icon name="chevronRight" size="md" />
      </button>
    </div>

    <!-- 页码：当前页是信号色实心块，其余是纸面方块，间隙用 1px 线分隔而不是负边距叠线 -->
    <nav
      v-if="totalPages > 1"
      class="flex flex-wrap items-center gap-1"
      :aria-label="t('pagination.pageOf', { page, total: totalPages })"
    >
      <button
        type="button"
        class="btn btn-secondary btn-icon"
        :disabled="page <= 1"
        :aria-label="t('pagination.previous')"
        @click="goToPage(page - 1)"
      >
        <Icon name="chevronLeft" size="md" />
      </button>

      <button
        v-for="(pageNum, index) in visiblePages"
        :key="`${pageNum}-${index}`"
        type="button"
        :disabled="typeof pageNum !== 'number'"
        :class="[
          'btn min-w-9 px-2 font-read',
          pageNum === page ? 'btn-primary' : 'btn-secondary',
          typeof pageNum !== 'number' && 'cursor-default bg-transparent'
        ]"
        :aria-label="typeof pageNum === 'number' ? t('pagination.goToPage', { page: pageNum }) : undefined"
        :aria-current="pageNum === page ? 'page' : undefined"
        @click="typeof pageNum === 'number' && goToPage(pageNum)"
      >
        {{ pageNum }}
      </button>

      <button
        type="button"
        class="btn btn-secondary btn-icon"
        :disabled="page >= totalPages"
        :aria-label="t('pagination.next')"
        @click="goToPage(page + 1)"
      >
        <Icon name="chevronRight" size="md" />
      </button>
    </nav>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import Select from './Select.vue'
import { getConfiguredTablePageSizeOptions, normalizeTablePageSize } from '@/utils/tablePreferences'
import { setPersistedPageSize } from '@/composables/usePersistedPageSize'

const { t } = useI18n()

interface Props {
  total: number
  page: number
  pageSize: number
  pageSizeOptions?: number[]
  showPageSizeSelector?: boolean
  showJump?: boolean
}

interface Emits {
  (e: 'update:page', page: number): void
  (e: 'update:pageSize', pageSize: number): void
}

const props = withDefaults(defineProps<Props>(), {
  pageSizeOptions: () => getConfiguredTablePageSizeOptions(),
  showPageSizeSelector: true,
  showJump: false
})

const emit = defineEmits<Emits>()

const totalPages = computed(() => Math.ceil(props.total / props.pageSize))

const fromItem = computed(() => {
  if (props.total === 0) return 0
  return (props.page - 1) * props.pageSize + 1
})

const toItem = computed(() => {
  const to = props.page * props.pageSize
  return to > props.total ? props.total : to
})

const pageSizeSelectOptions = computed(() => {
  const options = Array.from(
    new Set([
      ...getConfiguredTablePageSizeOptions(),
      normalizeTablePageSize(props.pageSize)
    ])
  ).sort((a, b) => a - b)

  return options.map((size) => ({
    value: size,
    label: String(size)
  }))
})

const jumpPage = ref('')

const visiblePages = computed(() => {
  const pages: (number | string)[] = []
  const maxVisible = 7
  const total = totalPages.value

  if (total <= maxVisible) {
    for (let i = 1; i <= total; i++) {
      pages.push(i)
    }
  } else {
    pages.push(1)

    const start = Math.max(2, props.page - 2)
    const end = Math.min(total - 1, props.page + 2)

    if (start > 2) {
      pages.push('...')
    }

    for (let i = start; i <= end; i++) {
      pages.push(i)
    }

    if (end < total - 1) {
      pages.push('...')
    }

    pages.push(total)
  }

  return pages
})

const goToPage = (newPage: number) => {
  if (newPage >= 1 && newPage <= totalPages.value && newPage !== props.page) {
    emit('update:page', newPage)
  }
}

const handlePageSizeChange = (value: string | number | boolean | null) => {
  if (value === null || typeof value === 'boolean') return
  const newPageSize = normalizeTablePageSize(typeof value === 'string' ? parseInt(value, 10) : value)
  setPersistedPageSize(newPageSize)
  emit('update:pageSize', newPageSize)
}

const submitJump = () => {
  const value = String(jumpPage.value).trim()
  if (!value) return
  const pageNum = Number.parseInt(value, 10)
  if (Number.isNaN(pageNum)) return
  const nextPage = Math.min(Math.max(pageNum, 1), totalPages.value)
  jumpPage.value = ''
  goToPage(nextPage)
}
</script>

<style scoped>
.page-size-select :deep(.select-trigger) {
  @apply px-2 py-1.5 text-sm;
}
</style>
