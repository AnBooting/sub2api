import DOMPurify from 'dompurify'

export function sanitizeSvg(svg: string): string {
  if (!svg) return ''
  // A controlled SVG parent preserves fragment namespaces while HTML parsing
  // still accepts complete uploaded SVG files with XML declarations or doctypes.
  const sanitized = DOMPurify.sanitize(`<svg xmlns="http://www.w3.org/2000/svg">${svg}</svg>`, {
    USE_PROFILES: { svg: true, svgFilters: true },
    RETURN_DOM: true,
  })
  const root = sanitized instanceof Element ? sanitized.firstElementChild : null
  if (root?.localName !== 'svg' || root.namespaceURI !== 'http://www.w3.org/2000/svg') return ''
  // Only serialize children of the sanitized container, never the original input.
  return root.innerHTML
}
