import source from '../KeyUsageView.vue?raw'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { sanitizeSvg } from '@/utils/sanitize'

// Exercise the production binding and constants, not a copy of their logic.
const svgTemplate = source.match(
  /<svg\b(?=[^>]*\bv-html="[^"]*row\.iconSvg[^"]*")[^>]*>[\s\S]*?<\/svg>/,
)?.[0]
if (!svgTemplate) throw new Error('Could not locate the production detail icon template')

const icons = [
  ['SHIELD', ['path']],
  ['CALENDAR', ['rect', 'line', 'line', 'line']],
  ['DOLLAR', ['line', 'path']],
  ['CHECK', ['polyline']],
] as const

describe('KeyUsageView detail icons', () => {
  for (const [name, tags] of icons) {
    it('renders the production ICON_' + name + ' in SVG namespace', () => {
      const icon = source.match(new RegExp("const ICON_" + name + " = '([^']+)'"))?.[1]
      if (!icon) throw new Error('Could not locate production ICON_' + name)
      const wrapper = mount({
        template: svgTemplate,
        setup: () => ({ row: { iconSvg: icon, iconColor: 'text-emerald-500' }, sanitizeSvg }),
      })
      try {
        expect(wrapper.element.tagName.toLowerCase()).toBe('svg')
        const children = Array.from(wrapper.element.children)
        expect(children.map((node) => node.tagName.toLowerCase())).toEqual(tags)
        for (const child of children) {
          expect(child.namespaceURI).toBe('http://www.w3.org/2000/svg')
        }
      } finally {
        wrapper.unmount()
      }
    })
  }

  it('keeps the production binding sanitized for untrusted input', () => {
    const wrapper = mount({
      template: svgTemplate,
      setup: () => ({
        row: { iconSvg: '<path d="M0 0h1" onclick="alert(1)"/><script>alert(1)</script>' },
        sanitizeSvg,
      }),
    })
    try {
      expect(wrapper.find('script').exists()).toBe(false)
      expect(wrapper.get('path').attributes('onclick')).toBeUndefined()
    } finally {
      wrapper.unmount()
    }
  })
})
