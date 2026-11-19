<template>
  <Teleport to="body">
    <Transition name="popup-fade">
      <div
        v-if="displayedAnnouncement"
        class="fixed inset-0 z-[120] flex items-end justify-center overflow-y-auto bg-ink/55 sm:items-start sm:p-4 sm:pt-[8vh]"
        @click.self="preview && emit('close')"
      >
        <div class="modal-content max-w-[680px] max-h-[92dvh] overflow-y-auto" @click.stop>
          <!-- 图签：1px 横线 + 标题 + 读数，没有装饰渐变 -->
          <div class="relative border-b border-rule px-4 py-4 sm:px-8 sm:py-5">
            <div class="relative z-10">
              <div class="mb-3 flex items-center gap-2">
                <div class="flex h-9 w-9 items-center justify-center border border-warning-300 bg-warning-50 text-warning-700">
                  <Icon name="bell" size="md" />
                </div>
                <span v-if="!preview" class="badge badge-warning">
                  {{ t('announcements.unread') }}
                </span>
              </div>

              <h2 class="mb-2 text-display-sm font-medium leading-tight text-ink">
                {{ displayedAnnouncement.title }}
              </h2>

              <div class="flex items-center gap-1.5 font-read text-[0.6875rem] text-ink-faint">
                <Icon name="clock" size="sm" />
                <time>{{ formatRelativeWithDateTime(displayedAnnouncement.created_at) }}</time>
              </div>
            </div>
          </div>

          <!-- 正文：左侧 2px 赭石导轨，提示"这是需要你读的一条" -->
          <div class="max-h-[50vh] overflow-y-auto px-4 py-5 sm:px-8 sm:py-6">
            <div class="border-l-2 border-warning-400 pl-4 sm:pl-6">
              <div
                class="markdown-body prose prose-sm max-w-none dark:prose-invert"
                v-html="renderedContent"
              ></div>
            </div>
          </div>

          <div class="modal-footer">
            <button
              type="button"
              @click="handleDismiss"
              data-testid="announcement-popup-dismiss"
              class="btn btn-primary"
            >
              <Icon :name="preview ? 'x' : 'check'" size="sm" />
              {{ preview ? t('common.close') : t('announcements.markRead') }}
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { useAnnouncementStore } from '@/stores/announcements'
import { formatRelativeWithDateTime } from '@/utils/format'
import type { Announcement, UserAnnouncement } from '@/types'
import Icon from '@/components/icons/Icon.vue'
import '@/styles/announcement-markdown.css'

type PreviewAnnouncement = Pick<Announcement | UserAnnouncement, 'title' | 'content' | 'created_at'>

const props = withDefaults(defineProps<{
  announcement?: PreviewAnnouncement | null
  preview?: boolean
}>(), {
  announcement: null,
  preview: false,
})

const emit = defineEmits<{
  close: []
}>()

const { t } = useI18n()
const announcementStore = useAnnouncementStore()
const displayedAnnouncement = computed(() => (
  props.preview ? props.announcement : announcementStore.currentPopup
))

marked.setOptions({
  breaks: true,
  gfm: true,
})

const renderedContent = computed(() => {
  const content = displayedAnnouncement.value?.content
  if (!content) return ''
  const html = marked.parse(content) as string
  return DOMPurify.sanitize(html)
})

function handleDismiss() {
  if (props.preview) {
    emit('close')
    return
  }
  announcementStore.dismissPopup()
}

// Manage body overflow — only set, never unset (bell component handles restore)
watch(
  displayedAnnouncement,
  (popup) => {
    if (popup) {
      document.body.style.overflow = 'hidden'
    } else if (props.preview) {
      document.body.style.overflow = ''
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  if (props.preview) {
    document.body.style.overflow = ''
  }
})
</script>

<style scoped>
/* 浮层出现只回答"被打开"这一个动作：≤200ms 淡入 + 上移 8px，不缩放 */
.popup-fade-enter-active,
.popup-fade-leave-active {
  transition: opacity var(--motion-base) var(--ease-cut);
}

.popup-fade-enter-from,
.popup-fade-leave-to {
  opacity: 0;
}

.popup-fade-enter-active > div,
.popup-fade-leave-active > div {
  transition: transform var(--motion-base) var(--ease-cut);
}

.popup-fade-enter-from > div,
.popup-fade-leave-to > div {
  transform: translateY(8px);
}

@media (prefers-reduced-motion: reduce) {
  .popup-fade-enter-active,
  .popup-fade-leave-active,
  .popup-fade-enter-active > div,
  .popup-fade-leave-active > div {
    transition-duration: 1ms;
  }
  .popup-fade-enter-from > div,
  .popup-fade-leave-to > div {
    transform: none;
  }
}
</style>
