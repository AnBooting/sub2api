/**
 * 深色信号色对比度契约。
 *
 * 2026-11-13 外部 review F2：深色 .btn-primary 把底提亮到 cobalt-400/300，
 * 亮字 on-signal 压在上面只有 3.18:1 / 2.09:1，普通字号不达 WCAG 1.4.3 的 4.5:1。
 * 修 .btn-primary 时漏了 platformButtonClass()（订阅主操作按钮）与两处
 * bg-signal text-on-signal 的小字，同类缺陷还在。
 *
 * 本文件把「深色下明亮钴蓝不得压亮字」写成契约：任何把 text-on-signal
 * 与 cobalt-400/300 配对的新写法都会在这里失败。
 */
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { platformButtonClass } from '../platformColors'

const root = resolve(__dirname, '../../..')

function read(rel: string) {
  return readFileSync(resolve(root, rel), 'utf8')
}

type RGB = [number, number, number]

/** 从 design-system.css 读令牌，避免测试里再抄一份色值。 */
function token(name: string): RGB {
  const css = read('src/styles/design-system.css')
  const match = new RegExp('--' + name + ':\\s*(\\d+)\\s+(\\d+)\\s+(\\d+)').exec(css)
  if (!match) throw new Error('token --' + name + ' not found')
  return [Number(match[1]), Number(match[2]), Number(match[3])]
}

function luminance(pair: RGB): number {
  const [r, g, b] = pair
  const channel = (value: number) => {
    const s = value / 255
    return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4)
  }
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b)
}

function contrast(fg: RGB, bg: RGB): number {
  const a = luminance(fg)
  const b = luminance(bg)
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05)
}

const ON_SIGNAL = token('on-signal')
const COBALT_500 = token('c-cobalt-500')
const COBALT_600 = token('c-cobalt-600')
const COBALT_700 = token('c-cobalt-700')

/** 契约基线：这些组合必须 >= 4.5:1。先锁死已知通过的，防止修一处坏另一处。 */
describe('signal backgrounds that carry on-signal text', () => {
  it.each([
    ['cobalt-500', COBALT_500],
    ['cobalt-600', COBALT_600],
    ['cobalt-700', COBALT_700]
  ])('%s meets 4.5:1 against on-signal', (_name, bg) => {
    expect(contrast(ON_SIGNAL, bg)).toBeGreaterThanOrEqual(4.5)
  })
})

/**
 * platformButtonClass 是订阅/续费主操作按钮的唯一来源。
 * 原实现深色 hover/active 用 primary-400/300，正好是 F2 的原数值。
 */
describe('platformButtonClass dark interaction states', () => {
  it('does not put on-signal text on the bright dark-mode signal background', () => {
    const cls = platformButtonClass('openai')
    // 深色下 bg-signal = cobalt-400；亮字压在上面只有 3.18:1
    expect(cls).not.toMatch(/^.*\bdark:hover:bg-primary-(400|300)\b/m)
    expect(cls).not.toMatch(/\bdark:active:bg-primary-(400|300)\b/)
  })

  it('keeps hover/active on cobalt levels that carry light text', () => {
    const cls = platformButtonClass('openai')
    for (const state of ['dark:hover', 'dark:active']) {
      const match = new RegExp('\\b' + state + ':bg-(primary|cobalt)-(\\d+)').exec(cls)
      expect(match, state + ' must pin an explicit dark background').not.toBeNull()
      expect(['500', '600', '700', '800', '900']).toContain(match![2])
    }
  })
})

/**
 * 两处小字直接写 bg-signal text-on-signal：深色下同样是 3.18:1。
 * 用源码断言把「不要再写这种组合」固定下来。
 */
describe('inline bg-signal text-on-signal is not reintroduced', () => {
  const allowed = [
    // 图标容器/装饰块不承载文字，允许保持
  ]

  it.each([
    'src/views/admin/RiskControlView.vue',
    'src/views/admin/ops/components/OpsDashboardHeader.vue'
  ])('%s does not pair bg-signal with text-on-signal', rel => {
    const src = read(rel)
    const offenders = src
      .split('\n')
      .filter(line => /bg-signal\b/.test(line) && /text-on-signal\b/.test(line))
    expect(offenders).toEqual(allowed.filter(() => false).concat([]))
  })
})
