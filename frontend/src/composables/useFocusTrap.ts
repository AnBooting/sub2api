import { onMounted, onUnmounted } from 'vue'

/**
 * 可聚焦元素选择器。
 * 与弹窗打开时的初始聚焦共用同一份规则，避免两处判断漂移。
 */
const FOCUSABLE_SELECTOR = [
  'a[href]',
  'area[href]',
  'button:not([disabled])',
  'input:not([disabled]):not([type="hidden"])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  'iframe',
  'object',
  'embed',
  '[tabindex]:not([tabindex="-1"])',
  '[contenteditable="true"]'
].join(',')

/**
 * 元素是否可能获得焦点。
 * 只排除明确不可交互的节点：display:none 的元素 focus() 无效，若落在
 * 边界上会让 Tab 看起来「卡住」。visibility:hidden 与祖先隐藏依赖布局
 * 计算，这里不处理，由 focus() 自身兜底——焦点只会停在原地，不会逃出弹窗。
 */
function isFocusCandidate(element: HTMLElement): boolean {
  if (element.hidden || element.hasAttribute('disabled')) return false
  if (element.getAttribute('aria-hidden') === 'true') return false
  try {
    return window.getComputedStyle(element).display !== 'none'
  } catch {
    // 脱离文档的节点在个别环境下无法计算样式，按可聚焦处理
    return true
  }
}

export function getFocusableElements(container: HTMLElement | null): HTMLElement[] {
  if (!container) return []

  return Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)).filter(isFocusCandidate)
}

export interface TabTrapDecision {
  /** 是否阻止浏览器默认的 Tab 移动 */
  preventDefault: boolean
  /** 需要主动聚焦的目标；为空表示只阻止默认行为 */
  target: HTMLElement | null
}

/**
 * 判断 Tab / Shift+Tab 是否越过容器边界。
 * 返回 null 表示不干预，交给浏览器按正常顺序移动。
 * 纯函数，便于直接断言边界规则。
 */
export function resolveTabTrap(
  elements: HTMLElement[],
  activeElement: Element | null,
  shiftKey: boolean
): TabTrapDecision | null {
  if (elements.length === 0) return { preventDefault: true, target: null }

  const first = elements[0]
  const last = elements[elements.length - 1]
  const index = activeElement instanceof HTMLElement ? elements.indexOf(activeElement) : -1

  // 焦点不在容器内（被点击、脚本或浏览器移出）时拉回容器
  if (index === -1) return { preventDefault: true, target: shiftKey ? last : first }
  if (shiftKey && index === 0) return { preventDefault: true, target: last }
  if (!shiftKey && index === elements.length - 1) return { preventDefault: true, target: first }

  return null
}

export interface UseFocusTrapOptions {
  /** 当前弹窗根节点 */
  container: () => HTMLElement | null
  /** 是否生效；多弹窗叠加时只让最上层生效 */
  isActive: () => boolean
}

/**
 * 把 Tab / Shift+Tab 限制在容器内。
 *
 * 监听 document 冒泡阶段：容器内的组件先处理自己的 Tab 行为（例如 Select 收起下拉），
 * 已 preventDefault 的事件不再拦截；只在边界处干预，正常顺序移动交给浏览器。
 */
export function useFocusTrap({ container, isActive }: UseFocusTrapOptions) {
  const onKeydown = (event: KeyboardEvent) => {
    if (event.key !== 'Tab' || event.defaultPrevented) return
    if (!isActive()) return

    const decision = resolveTabTrap(getFocusableElements(container()), document.activeElement, event.shiftKey)
    if (!decision) return

    event.preventDefault()
    decision.target?.focus({ preventScroll: true })
  }

  onMounted(() => document.addEventListener('keydown', onKeydown))
  onUnmounted(() => document.removeEventListener('keydown', onKeydown))

  return { onKeydown }
}
