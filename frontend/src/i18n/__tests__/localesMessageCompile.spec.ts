import { afterAll, describe, expect, it, vi } from 'vitest'

// Both Vite and Vitest alias vue-i18n to its runtime-only bundle. Production
// enables this JIT flag in vite.config.ts; Vitest does not inherit that define.
// Set it before importing the actual runtime so v9.14.5 registers its own
// compiler (AST/JIT, no custom translation function or replacement messages).
vi.hoisted(() => { vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true) })
afterAll(() => vi.unstubAllGlobals())
import { baseCompile } from '@intlify/message-compiler'
import { createI18n } from 'vue-i18n'

import en from '../locales/en'
import zh from '../locales/zh'

// vue-i18n 在运行时才编译消息：文案里未转义的花括号（如内嵌 JSON 示例
// "{\"user-agent\": ...}"）会在渲染时抛 "Invalid token in placeholder"，
// 直接炸掉整个组件树，且构建期完全无感。本测试把全部文案预编译一遍，
// 将该类问题固化为显式失败。字面量花括号请用 {'{'} / {'}'} 转义，
// 或将语言中立的示例文本（如 JSON）移出 i18n。
function collectCompileErrors(node: unknown, path: string, out: string[]): void {
  if (typeof node === 'string') {
    baseCompile(node, {
      onError: (err) => {
        out.push(`${path}: ${err.message}`)
      }
    })
    return
  }
  if (Array.isArray(node)) {
    node.forEach((item, index) => collectCompileErrors(item, `${path}[${index}]`, out))
    return
  }
  if (node && typeof node === 'object') {
    for (const [key, value] of Object.entries(node as Record<string, unknown>)) {
      collectCompileErrors(value, path ? `${path}.${key}` : key, out)
    }
  }
}

describe('locale messages compile', () => {
  it.each([['zh', zh], ['en', en]] as const)('%s calendar clear help renders a visible literal empty object with the real installed message compiler', (locale, messages) => {
    const instance = createI18n({ legacy: false, locale, messages: { [locale]: messages } })
    expect(instance.global.te('admin.settings.pricing.calendar.editorHelp')).toBe(true)
    const rendered = instance.global.t('admin.settings.pricing.calendar.editorHelp')
    expect(rendered).toContain('{}')
    expect(rendered).not.toContain("{'{'}")
    expect(rendered).not.toContain("{'}'}")
  })
  it.each([
    ['zh', zh],
    ['en', en]
  ] as const)('%s messages all compile without placeholder errors', (locale, messages) => {
    const errors: string[] = []
    collectCompileErrors(messages, locale, errors)
    expect(errors).toEqual([])
  })
})
