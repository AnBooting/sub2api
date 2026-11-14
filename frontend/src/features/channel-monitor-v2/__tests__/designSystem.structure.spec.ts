/**
 * Structure contracts: channel-monitor-v2 + studio shells must use project
 * design-system utility classes rather than isolated flat RGB skins.
 *
 * 视觉契约见 project-docs/design/01-token-contract.md：
 *   - 层次靠 1px 痕量线（border-rule）+ 留白，不靠阴影 => 不得出现装饰性 shadow-sm/md/lg
 *   - 圆角最大 6px（rounded-2xl 及以上一律不得出现），转角是被切出来的，不是磨圆的
 *   - 面板/卡片走 .panel / .card 语义类，并带 1px 痕量线
 * 因此本文件对「旧皮肤特征」（rounded-3xl / ring-1 ring-gray-900/5 / shadow-sm）
 * 一律做**反向断言**：它们出现即为违反契约。
 */
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const root = resolve(__dirname, '../../..')

function read(rel: string) {
  return readFileSync(resolve(root, rel), 'utf8')
}

/**
 * 旧皮肤三项特征（圆角膨胀 / 环形描边假层次 / 装饰性投影）都已被令牌契约禁止。
 * 抽成一处断言，避免每个组件各写一遍、将来又漂回去。
 */
function expectNoLegacySkin(src: string) {
  // 圆角最大 6px：rounded-2xl 以上的视觉膨胀一律禁止（映射已压到 6px，写错也不会正确表达层次）。
  expect(src).not.toContain('rounded-3xl')
  // ring-1 ring-gray-900/5 是用 1px 环描边伪造层次，替代品是 border-rule 痕量线。
  expect(src).not.toContain('ring-1 ring-gray-900/5')
  // 装饰性投影禁止；真正浮起的层只允许 shadow-float / shadow-float-sm。
  expect(src).not.toMatch(/\bshadow-(sm|md|lg|xl|2xl)\b/)
}

describe('channel-monitor-v2 design system structure', () => {
  it('user ChannelStatus V2 shell uses page-header, card, btn, tabs utilities', () => {
    // Route wrapper may switch V1/V2; design chrome lives on the V2 implementation.
    const src = read('views/user/ChannelStatusV2View.vue')
    expect(src).toContain('page-header')
    expect(src).toContain('page-title')
    expect(src).toContain('class="card')
    expect(src).toContain('btn btn-secondary')
    expect(src).toContain('class="tab')
    expect(src).toContain('tab-active')
    expect(src).toContain('badge badge-warning')
    // Compact single-row toolbar
    expect(src).toContain('monitor-toolbar')
    expect(src).toContain('clearFilters')
    expect(src).toContain('healthModeOptions')
    expect(src).toContain("'cache'")
    // 层次靠 1px 痕量线而不是圆角膨胀 + 环形投影
    expectNoLegacySkin(src)
    // Overview-first KPI strip before primary viz
    expect(src.indexOf('summaryAria')).toBeLessThan(src.indexOf('MonitorTrendChart'))
    // No page-level fixed min-width that forces viewport horizontal scroll
    expect(src).not.toMatch(/min-width:\s*980px/)
    expect(src).not.toMatch(/min-w-\[980px\]/)
    // Dense tables scroll internally
    expect(src).toMatch(/max-h-\[min\(52vh/)
    expect(src).toContain('overflow-auto')
    // Trend view toggle (pulse matrix / line chart) + default platform/group dimension
    expect(src).toContain("trendView")
    expect(src).toContain("'platform_group'")
    expect(src).toContain('MonitorTrendChart')
  })

  it('RelayPulseMatrix uses card chrome, matrix scroll, and hover tooltips (no click modal)', () => {
    const src = read('features/channel-monitor-v2/RelayPulseMatrix.vue')
    expect(src).toContain('class="card')
    expect(src).toContain('card-header')
    expect(src).toContain('card-body')
    expect(src).toContain('matrix-scroll')
    expect(src).toMatch(/max-h-\[min\(42vh/)
    expect(src).toContain('overflow-auto')
    expect(src).toContain('pulse-tooltip')
    expectNoLegacySkin(src)
    expect(src).not.toContain('modal-overlay')
    expect(src).not.toContain('modal-content')
  })

  it('MetricCell uses stat-card utility', () => {
    const src = read('features/channel-monitor-v2/MetricCell.vue')
    expect(src).toContain('stat-card')
    expect(src).toContain('stat-label')
    expect(src).toContain('stat-value')
    expectNoLegacySkin(src)
  })

  it('MonitorTrendChart uses Ops chart shell tokens', () => {
    const src = read('features/channel-monitor-v2/MonitorTrendChart.vue')
    expect(src).toContain('class="card')
    expectNoLegacySkin(src)
    expect(src).toContain('EmptyState')
    expect(src).toContain('min-h-[360px]')
  })

  it('FilterMultiSelect uses rounded-xl input chrome and dropdown utility', () => {
    const src = read('features/channel-monitor-v2/FilterMultiSelect.vue')
    // rounded-xl 在新令牌层映射为 5px（<=6px 上限），类名继续存在是合规的。
    expect(src).toContain('rounded-xl')
    expect(src).toContain('dropdown')
    expect(src).toContain('dropdown-item')
  })

  it('MonitorSettingsPanel uses page-header, card, btn-primary, tabs', () => {
    const src = read('features/channel-monitor-v2/MonitorSettingsPanel.vue')
    expect(src).toContain('page-header')
    expect(src).toContain('btn btn-primary')
    expect(src).toContain('class="card')
    expect(src).toContain('tab-active')
    expect(src).toMatch(/max-h-\[min\(40vh/)
    // 全局视觉契约（01-token-contract 第 2 条）：层次靠 1px 痕量线，不靠圆角膨胀与投影。
    expectNoLegacySkin(src)
  })

  it('admin ChannelMonitorView V2 tab chrome uses project tabs', () => {
    const src = read('views/admin/ChannelMonitorView.vue')
    expect(src).toContain('page-header')
    expect(src).toContain('page-title')
    expect(src).toContain('class="tabs')
    expect(src).toContain('tab-active')
    expect(src).toContain('MonitorSettingsPanel')
    // 同上：管理端外壳与用户端共用同一套纸与线。
    expectNoLegacySkin(src)
  })
})
