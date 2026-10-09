import { describe, expect, it } from 'vitest'
import { supplementIdentityMappings } from '../channelModelMapping'

describe('channel identity mapping import', () => {
  it('keeps custom and wildcard mappings and ignores duplicate/invalid names', () => {
    const existing = { 'GPT-A': 'custom-target', 'claude-*': 'custom-claude' }
    const result = supplementIdentityMappings(existing, ['gpt-a', 'claude-sonnet', ' new-model ', 'new-model', '', 'wild-*'])
    expect(result).toEqual({ mapping: { ...existing, 'new-model': 'new-model' }, added: 1 })
    expect(existing).toEqual({ 'GPT-A': 'custom-target', 'claude-*': 'custom-claude' })
    expect(supplementIdentityMappings(result.mapping, ['new-model']).added).toBe(0)
  })

  it('does not add models covered by a passthrough wildcard', () => {
    expect(supplementIdentityMappings({ '*': '*' }, ['gpt-a', 'deepseek-chat']).added).toBe(0)
  })

  it('treats prototype-like model IDs as ordinary keys', () => {
    const { mapping, added } = supplementIdentityMappings({}, ['__proto__', 'constructor'])
    expect(added).toBe(2)
    expect(Object.getPrototypeOf(mapping)).toBe(Object.prototype)
    expect(Object.prototype.hasOwnProperty.call(mapping, '__proto__')).toBe(true)
    expect(mapping.__proto__).toBe('__proto__')
  })
})
