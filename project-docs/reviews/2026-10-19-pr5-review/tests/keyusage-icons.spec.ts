import { readFileSync } from 'node:fs'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { sanitizeSvg } from '@/utils/sanitize'

// Run from frontend/ via the repository's Vitest config.
// Use the actual production template rather than recreating its binding logic.
const source = readFileSync('src/views/KeyUsageView.vue', 'utf8')
const svgTemplate = source.match(
  /<svg\b(?=[^>]*\bv-html="[^"]*row\.iconSvg[^"]*")[^>]*>[\s\S]*?<\/svg>/,
)?.[0]

if (!svgTemplate) {
  throw new Error('Could not locate the production row.iconSvg template; update the regression test.')
}

describe('PR #5: KeyUsageView detail icons remain visible', () => {
  for (const name of ['SHIELD', 'CALENDAR', 'DOLLAR', 'CHECK']) {
    it(`renders actual ICON_${name} child nodes`, () => {
      const icon = source.match(new RegExp(`const ICON_${name} = '([^']+)'`))?.[1]
      if (!icon) throw new Error(`Could not locate production ICON_${name}`)

      const wrapper = mount({
        template: svgTemplate,
        setup: () => ({
          row: { iconSvg: icon, iconColor: 'text-emerald-500' },
          sanitizeSvg,
        }),
      })
      try {
        expect(wrapper.element.tagName.toLowerCase()).toBe('svg')
        expect(wrapper.element.childElementCount).toBeGreaterThan(0)
      } finally {
        wrapper.unmount()
      }
    })
  }
})
