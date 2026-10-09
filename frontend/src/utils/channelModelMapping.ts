/** Supplement concrete identity mappings while retaining exact/wildcard rules. */
export function supplementIdentityMappings(existing: Record<string, string>, candidates: string[]) {
  const mapping = { ...existing }
  const patterns = Object.keys(existing).map(model => model.trim().toLowerCase())
  let added = 0
  for (const model of candidates.map(model => model.trim()).sort()) {
    if (!model || model.includes('*')) continue
    const key = model.toLowerCase()
    if (patterns.some(pattern => pattern.endsWith('*')
      ? key.startsWith(pattern.slice(0, -1))
      : key === pattern)) continue
    Object.defineProperty(mapping, model, { value: model, enumerable: true, writable: true, configurable: true })
    patterns.push(key)
    added++
  }
  return { mapping, added }
}
