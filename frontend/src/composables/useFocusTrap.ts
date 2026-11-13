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
 * 只收集能参与顺序键盘导航的控件，包括其祖先的可见性和惰性状态。
 * 不能依赖 focus() 失败兜底：错误的末项会让浏览器直接把 Tab 放行到背景。
 */
function isFocusCandidate(element: HTMLElement): boolean {
  if (element.tabIndex < 0 || element.matches(':disabled')) return false
  if (element.closest('[hidden], [inert], [aria-hidden="true"]')) return false
  const visibility = window.getComputedStyle(element).visibility
  if (visibility === 'hidden' || visibility === 'collapse') return false
  for (let node: HTMLElement | null = element; node; node = node.parentElement) {
    const style = window.getComputedStyle(node)
    if (style.display === 'none' || style.contentVisibility === 'hidden') return false
  }
  // 浏览器进一步排除未渲染节点（例如闭合 details）；jsdom 没有布局 API。
  return !element.checkVisibility || element.checkVisibility()
}

export function getFocusableElements(container: HTMLElement | null): HTMLElement[] {
  if (!container) return []

  return Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)).filter(isFocusCandidate)
}

/** body portal 通过触发控件继承所属弹窗，不改变定位或堆叠上下文。 */
export function getDialogFocusScope(element: HTMLElement | null | undefined): string | undefined {
  return element?.closest<HTMLElement>('[data-dialog-focus-scope]')?.dataset.dialogFocusScope
}

function getFocusRoots(container: HTMLElement): HTMLElement[] {
  const scope = container.dataset.dialogFocusScope
  if (!scope) return [container]
  return [container, ...Array.from(document.querySelectorAll<HTMLElement>('[data-dialog-focus-scope]'))
    .filter(root => root !== container && root.dataset.dialogFocusScope === scope)]
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

    const panel = container()
    if (!panel) return
    const roots = getFocusRoots(panel)
    const elements = roots.flatMap(getFocusableElements)
    const active = document.activeElement
    let decision = resolveTabTrap(elements, active, event.shiftKey)
    if (!decision && active instanceof HTMLElement) {
      const next = elements[elements.indexOf(active) + (event.shiftKey ? -1 : 1)]
      // portal 与面板在 DOM 中可能隔着背景控件，跨根移动必须显式接续。
      if (!roots.some(root => root.contains(active) && root.contains(next))) {
        decision = { preventDefault: true, target: next }
      }
    }
    if (!decision) return

    event.preventDefault()
    ;(decision.target ?? panel).focus({ preventScroll: true })
  }

  onMounted(() => document.addEventListener('keydown', onKeydown))
  onUnmounted(() => document.removeEventListener('keydown', onKeydown))

  return { onKeydown }
}
