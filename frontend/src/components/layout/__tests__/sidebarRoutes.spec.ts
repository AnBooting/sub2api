import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import router from '@/router'

const here = dirname(fileURLToPath(import.meta.url))
// __tests__ 位于 src/components/layout/__tests__，侧栏就在上一级。
const SIDEBAR = resolve(here, '../AppSidebar.vue')

const sidebarSource = () => readFileSync(SIDEBAR, 'utf8')

/**
 * 从 AppSidebar.vue 源码里取出所有导航 path 字面量。
 *
 * 为什么解析源码而不是运行时枚举：侧栏项是 computed + featureFlag 过滤出来的，
 * 运行时通常只渲染出一部分（线上 risk_control_enabled=false 时整个安全审计分组都不渲染），
 * 那样测不到被开关隐藏的项 —— 而「被开关藏起来的死菜单」恰恰最容易漏。
 */
function sidebarPaths(): string[] {
  const src = sidebarSource()
  return [...new Set([...src.matchAll(/path:\s*'([^']+)'/g)].map((m) => m[1]))]
}

/**
 * 侧栏里「带 children 的分组父项」。
 *
 * 逐个 NavItem 字面量解析：以 `path: '<x>'` 开头，同一对象里出现 `children: [`
 * 即视为分组父项（expandOnly 与否由调用方单独判断）。
 */
function sidebarGroupParents(): Array<{ path: string; expandOnly: boolean }> {
  const src = sidebarSource()
  const out: Array<{ path: string; expandOnly: boolean }> = []
  for (const m of src.matchAll(/path:\s*'([^']+)'/g)) {
    const start = m.index ?? 0
    // 取到下一个 path 字面量之前的片段，作为该 NavItem 的近似文本范围。
    const rest = src.slice(start)
    const nextPathIdx = rest.indexOf("path: '", 10)
    const segment = nextPathIdx > 0 ? rest.slice(0, nextPathIdx) : rest
    if (segment.includes('children: [')) {
      out.push({ path: m[1], expandOnly: segment.includes('expandOnly: true') })
    }
  }
  return [...new Set(out.map((o) => JSON.stringify(o)))].map((s) => JSON.parse(s))
}

/** 该 path 是否被 router 解析（不落到 404 catch-all）。 */
function resolves(path: string): boolean {
  const resolved = router.resolve(path)
  if (resolved.name === 'NotFound') return false
  return resolved.matched.length > 0
}

/** 该 path 是否有可直接渲染的组件（区别于纯 redirect 记录）。 */
function hasComponent(path: string): boolean {
  const resolved = router.resolve(path)
  return resolved.matched.some((r) => Boolean(r.components?.default))
}

describe('侧栏菜单项 → 路由 可达性', () => {
  it('收集到非空菜单 path 集合（防止正则失效导致空跑）', () => {
    const paths = sidebarPaths()
    expect(paths.length).toBeGreaterThan(30)
    expect(paths).toContain('/admin/dashboard')
    expect(paths).toContain('/keys')
  })

  it('收集到分组父项（防止父项解析失效导致空跑）', () => {
    const parents = sidebarGroupParents()
    expect(parents.length).toBeGreaterThanOrEqual(4)
    expect(parents.map((p) => p.path)).toContain('/admin/channels')
    expect(parents.filter((p) => p.expandOnly).length).toBeGreaterThanOrEqual(4)
  })

  it('每一条侧栏 path 都能被 router 解析到（不允许落到 404 catch-all）', () => {
    // 这是本测试的核心：一次防住所有死菜单项。
    // expandOnly 父项虽然点击不导航，但它的 path 仍会出现在「直接访问 URL」与
    // 面包屑/预加载等路径上，不能是 404。
    const broken = sidebarPaths()
      .map((path) => ({ path, ok: resolves(path) }))
      .filter((row) => !row.ok)
      .map((row) => row.path)
    expect(
      broken,
      `侧栏存在 router 无法解析的 path（直接访问会 404/白屏）：${broken.join(', ')}`,
    ).toEqual([])
  })

  it('非 expandOnly 的分组父项必须能渲染出组件（点击会 router.push）', () => {
    const broken = sidebarGroupParents()
      .filter((p) => !p.expandOnly)
      .map((p) => p.path)
      .filter((path) => !hasComponent(path))
    expect(broken, `分组父项被 router.push 却无组件：${broken.join(', ')}`).toEqual([])
  })

  it('expandOnly 分组父项要有 redirect 兜底（与仓库既有约定一致）', () => {
    // 约定来源：/admin/channels、/admin/affiliates、/admin/orders 三个 expandOnly 分组
    // 都在 router 里配了 redirect 记录。这样即便用户直接粘贴 URL、或在子页面被
    // 权限拦截后回退，也能落到确定的位置而不是 404。
    const expandOnlyParents = sidebarGroupParents()
      .filter((p) => p.expandOnly)
      .map((p) => p.path)
    expect(expandOnlyParents.length).toBeGreaterThanOrEqual(4)

    const missing = expandOnlyParents.filter((path) => !resolves(path))
    expect(
      missing,
      `这些 expandOnly 分组父项没有 router 记录（直接访问会 404）：${missing.join(', ')}`,
    ).toEqual([])
  })
})
