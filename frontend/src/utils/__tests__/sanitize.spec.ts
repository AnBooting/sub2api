import { describe, expect, it } from 'vitest'
import { sanitizeSvg } from '../sanitize'

const svgNamespace = 'http://www.w3.org/2000/svg'

function renderFragment(input: string): SVGSVGElement {
  const svg = document.createElementNS(svgNamespace, 'svg')
  svg.innerHTML = sanitizeSvg(input)
  return svg
}

describe('sanitizeSvg', () => {
  it('returns an empty string for empty input', () => {
    expect(sanitizeSvg('')).toBe('')
  })

  it('preserves multiple SVG fragment nodes without adding an SVG root', () => {
    const svg = renderFragment('<rect x="3" width="18"/><line x1="3" x2="21"/>')
    expect(svg.children).toHaveLength(2)
    expect(svg.querySelector('svg')).toBeNull()
    expect(svg.querySelector('rect')?.getAttribute('width')).toBe('18')
    expect(svg.querySelector('line')?.getAttribute('x2')).toBe('21')
  })

  for (const [name, prefix] of [
    ['no preamble', ''],
    ['XML declaration', '<?xml version="1.0" encoding="UTF-8"?>'],
    ['DOCTYPE', '<!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd">'],
  ]) {
    it('preserves complete SVG for HTML preview containers: ' + name, () => {
      const host = document.createElement('span')
      host.innerHTML = sanitizeSvg(prefix + '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M0 0h1"/></svg>')
      expect(host.children).toHaveLength(1)
      const svg = host.querySelector('svg')
      expect(svg?.namespaceURI).toBe(svgNamespace)
      expect(svg?.getAttribute('viewBox')).toBe('0 0 24 24')
      expect(svg?.querySelector('path')?.getAttribute('d')).toBe('M0 0h1')
    })
  }

  it('preserves SVG filters and case-sensitive attributes', () => {
    const host = document.createElement('span')
    host.innerHTML = sanitizeSvg('<svg><defs><filter id="blur"><feGaussianBlur stdDeviation="2"/></filter></defs><circle r="4" filter="url(#blur)"/></svg>')
    expect(host.querySelector('feGaussianBlur')?.getAttribute('stdDeviation')).toBe('2')
    expect(host.querySelector('circle')?.getAttribute('filter')).toBe('url(#blur)')
  })

  for (const complete of [false, true]) {
    it('removes executable content from ' + (complete ? 'complete SVG' : 'SVG fragments'), () => {
      const dirty = '<path d="M0 0h1" onclick="alert(1)"/>'
        + '<script>alert(1)</script>'
        + '<foreignObject><div xmlns="http://www.w3.org/1999/xhtml">unsafe</div></foreignObject>'
        + '<a href="javascript:alert(1)"><circle r="2"/></a>'
        + '<a xmlns:xlink="http://www.w3.org/1999/xlink" xlink:href="javascript:alert(1)"><path d="M1 1h2"/></a>'
      const svg = renderFragment(complete ? '<svg onload="alert(1)">' + dirty + '</svg>' : dirty)
      expect(svg.querySelector('path')?.getAttribute('d')).toBe('M0 0h1')
      expect(svg.querySelector('script, foreignObject, div')).toBeNull()
      expect(svg.querySelector('[onclick], [onload]')).toBeNull()
      expect(svg.querySelectorAll('a')).toHaveLength(2)
      for (const link of svg.querySelectorAll('a')) {
        expect(link.getAttribute('href')).toBeNull()
        expect(link.getAttributeNS('http://www.w3.org/1999/xlink', 'href')).toBeNull()
      }
    })
  }

  it('preserves a complete SVG without an explicit namespace', () => {
    const host = document.createElement('span')
    host.innerHTML = sanitizeSvg('<svg viewBox="0 0 24 24"><path d="M0 0h1"/></svg>')
    expect(host.querySelector('svg > path')?.namespaceURI).toBe(svgNamespace)
  })

  it('does not return content that escapes the controlled SVG container', () => {
    const host = document.createElement('span')
    host.innerHTML = sanitizeSvg('</svg><img src="x" onerror="alert(1)"><svg onload="alert(1)"><path d="M0 0h1"/></svg>')
    expect(host.querySelector('img, [onerror], [onload]')).toBeNull()
  })

  it('does not recover invalid markup by returning the untrusted original', () => {
    const clean = sanitizeSvg('<path onclick="alert(1)"')
    expect(clean).not.toContain('onclick')
    expect(clean).not.toContain('alert(1)')
  })
})
