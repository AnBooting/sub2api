<template>
  <div class="table-page-layout" :class="{ 'mobile-mode': isMobile }">
    <!-- 固定区：操作 -->
    <div v-if="$slots.actions" class="layout-section-fixed">
      <slot name="actions" />
    </div>

    <!-- 固定区：搜索与过滤 -->
    <div v-if="$slots.filters" class="layout-section-fixed">
      <slot name="filters" />
    </div>

    <!-- 滚动区：读数册 -->
    <div class="layout-section-scrollable">
      <div class="card table-scroll-container">
        <slot name="table" />
      </div>
    </div>

    <!-- 固定区：分页 -->
    <div v-if="$slots.pagination" class="layout-section-fixed">
      <slot name="pagination" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'

const isMobile = ref(false)

const checkMobile = () => {
  isMobile.value = window.innerWidth < 1024
}

onMounted(() => {
  checkMobile()
  window.addEventListener('resize', checkMobile)
})

onUnmounted(() => {
  window.removeEventListener('resize', checkMobile)
})
</script>

<style scoped>
/* 桌面：整页不滚动，只有表体滚动 —— 表头与分页钉住，读数行在中间走。 */
.table-page-layout {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  /* header 高 3.75rem + 主区上下 padding（lg: py-6 = 1.5rem × 2） */
  height: calc(100dvh - 3.75rem - 3rem);
}

.layout-section-fixed {
  flex-shrink: 0;
}

.layout-section-scrollable {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

/* 读数册容器：1px 痕量线围出，切角 2px，不投影 */
.table-scroll-container {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  height: 100%;
  background-color: rgb(var(--surface));
  border: 1px solid rgb(var(--rule));
  border-radius: var(--radius-cut);
}

.table-scroll-container :deep(.table-wrapper) {
  flex: 1;
  overflow: auto;
  scrollbar-gutter: stable;
  -webkit-overflow-scrolling: touch;
}

.table-scroll-container :deep(table) {
  width: 100%;
  min-width: max-content;
  display: table; /* 标准 table 布局，sticky 列才有参照 */
}

.table-scroll-container :deep(thead) {
  background-color: rgb(var(--paper-inset));
}

.table-scroll-container :deep(th) {
  padding: 0.5rem 0.75rem;
  text-align: left;
  font-family: var(--font-read);
  font-size: 0.6875rem;
  font-weight: 500;
  letter-spacing: var(--track-label);
  color: rgb(var(--ink-faint));
  border-bottom: 1px solid rgb(var(--rule));
  white-space: nowrap;
}

.table-scroll-container :deep(td) {
  padding: 0.5rem 0.75rem;
  font-size: 0.8125rem;
  color: rgb(var(--ink-soft));
  border-bottom: 1px solid rgb(var(--rule));
  vertical-align: middle;
}

/* 移动端：同一本读数册，只是不再锁视口高度 —— 页面跟着内容走，
 * 表格在自己的容器里横向滚动，页面本身绝不横向溢出。 */
.table-page-layout.mobile-mode {
  height: auto;
  min-height: 0;
}

.table-page-layout.mobile-mode .layout-section-scrollable {
  flex: none;
  min-height: 0;
}

.table-page-layout.mobile-mode .table-scroll-container {
  height: auto;
  max-height: none;
}

.table-page-layout.mobile-mode .table-scroll-container :deep(.table-wrapper) {
  flex: none;
  max-height: none;
  overflow-x: auto;
  overflow-y: visible;
}

.table-page-layout.mobile-mode .table-scroll-container :deep(table) {
  display: table;
  min-width: 100%;
}

/* 移动端触控：行更高，行内按钮点击区足够 */
@media (max-width: 1023px) {
  .table-scroll-container :deep(td) {
    padding-top: 0.625rem;
    padding-bottom: 0.625rem;
    font-size: 0.875rem;
  }
  .table-scroll-container :deep(th) {
    padding-top: 0.625rem;
    padding-bottom: 0.625rem;
  }
}
</style>
