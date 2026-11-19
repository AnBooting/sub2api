<script setup lang="ts">
/**
 * 导航进度条：页面切换时在顶部画一根 1px 信号色痕量线。
 * 它回答"正在换页"这一个动作，不做无限循环的装饰动画；
 * prefers-reduced-motion 下退化为静止的一条实线。
 */
import { computed } from 'vue'
import { useNavigationLoadingState } from '@/composables/useNavigationLoading'

const { isLoading } = useNavigationLoadingState()

const isVisible = computed(() => isLoading.value)
</script>

<template>
  <Transition name="progress-fade">
    <div
      v-show="isVisible"
      class="navigation-progress"
      role="progressbar"
      aria-label="Loading"
      aria-valuenow="0"
      aria-valuemin="0"
      aria-valuemax="100"
    >
      <div class="navigation-progress-bar" />
    </div>
  </Transition>
</template>

<style scoped>
.navigation-progress {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  height: 2px;
  z-index: 9999;
  overflow: hidden;
  background: transparent;
  pointer-events: none;
}

/* 同一信号色的两档之间过渡，总面积远小于 5% —— 令牌契约允许的唯一渐变用法 */
.navigation-progress-bar {
  height: 100%;
  width: 40%;
  background: linear-gradient(
    90deg,
    rgb(var(--signal) / 0),
    rgb(var(--signal)),
    rgb(var(--signal) / 0)
  );
  animation: progress-slide 1.2s var(--ease-cut) infinite;
}

@keyframes progress-slide {
  0% {
    transform: translateX(-100%);
  }
  100% {
    transform: translateX(350%);
  }
}

/* 淡入淡出过渡 */
.progress-fade-enter-active {
  transition: opacity var(--motion-fast) var(--ease-cut);
}

.progress-fade-leave-active {
  transition: opacity var(--motion-base) var(--ease-cut);
}

.progress-fade-enter-from,
.progress-fade-leave-to {
  opacity: 0;
}

/* 减少动效：静止的一条实线，仍然表达"卡在换页中" */
@media (prefers-reduced-motion: reduce) {
  .navigation-progress-bar {
    animation: none;
    width: 100%;
    background: rgb(var(--signal) / 0.6);
  }
}
</style>
