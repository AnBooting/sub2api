import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { beforeAll, afterAll, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import postcss from 'postcss'
import tailwindcss from 'tailwindcss'
import tailwindConfig from '../tailwind.config.js'
import HomeView from './views/HomeView.vue'
import PlazaFilterBar from './components/modelPlaza/PlazaFilterBar.vue'

const { appStore, authStore } = vi.hoisted(() => ({
  appStore: {
    cachedPublicSettings: { doc_url: 'https://docs.example.com/guide' },
    siteName: 'Test', siteLogo: '', docUrl: '', publicSettingsLoaded: true,
    fetchPublicSettings: vi.fn(),
  },
  authStore: { isAuthenticated: false, isAdmin: false, user: null, checkAuth: vi.fn() },
}))
vi.mock('@/stores', () => ({ useAppStore: () => appStore, useAuthStore: () => authStore }))
vi.mock('@/stores/app', () => ({ useAppStore: () => appStore }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

const read = (path: string) => readFileSync(resolve(path), 'utf8')
const sidebar = read('src/components/layout/AppSidebar.vue')
const layout = read('src/components/layout/AppLayout.vue')
const filter = read('src/components/modelPlaza/PlazaFilterBar.vue')
let style: HTMLStyleElement

beforeAll(async () => {
  const css = await postcss([tailwindcss({
    ...tailwindConfig,
    content: [{ raw: [sidebar, layout, filter].join('\n'), extension: 'html' }],
  })]).process(read('src/style.css'), { from: undefined })
  const root = postcss.parse(css.css)
  // jsdom does not evaluate responsive media queries. Apply desktop media
  // rules at 1280px, preserving their production order and selectors.
  root.walkAtRules('media', (rule) => {
    const min = rule.params.match(/min-width:\s*(\d+)px/)
    if (min && Number(min[1]) <= 1280) rule.replaceWith(rule.nodes)
    else rule.remove()
  })
  style = document.createElement('style')
  style.textContent = root.toString()
  document.head.append(style)
})
afterAll(() => style?.remove())

describe('PR #7 production UI regressions', () => {
  it('keeps the configured docs card an external URL', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes: [
      { path: '/:pathMatch(.*)*', component: { template: '<div />' } },
    ] })
    await router.push('/home')
    await router.isReady()
    const wrapper = mount(HomeView, { global: { plugins: [router], stubs: {
      LocaleSwitcher: true, Icon: true,
    } } })
    try {
      const docs = wrapper.findAll('a').filter((link) =>
        link.text().includes('home.docs') || link.attributes('title') === 'home.viewDocs')
      expect(docs.length, 'expected configured documentation links').toBeGreaterThan(0)
      for (const link of docs) expect(link.attributes('href')).toBe('https://docs.example.com/guide')
    } finally { wrapper.unmount() }
  })

  it('aligns desktop content with the expanded sidebar width', () => {
    const asideTemplate = sidebar.match(/<aside\b[^>]*>/)![0] + '</aside>'
    const layoutTemplate = layout.match(/<template>([\s\S]*?)<\/template>/)![1]
    const SidebarShell = { template: asideTemplate, setup: () => ({ sidebarCollapsed: false, mobileOpen: true }) }
    const wrapper = mount({ template: layoutTemplate, setup: () => ({ sidebarCollapsed: false }) }, {
      attachTo: document.body,
      global: { components: { AppSidebar: SidebarShell, AppHeader: { template: '<header />' } } },
    })
    try {
      const aside = wrapper.get('aside').element
      const content = wrapper.get('main').element.parentElement!
      expect(getComputedStyle(content).marginLeft).toBe(getComputedStyle(aside).width)
    } finally { wrapper.unmount() }
  })

  it('gives selected model-plaza filters a contrasting background', () => {
    const wrapper = mount(PlazaFilterBar, { attachTo: document.body, props: {
      platforms: ['openai'], groups: [{ id: 1, name: 'Test', platform: 'openai', rate: 1 }],
      rates: [1], platform: 'all', groupId: 'all', rate: 'all', search: '',
    }, global: { stubs: { PlatformIcon: true, Icon: true } } })
    try {
      const selected = wrapper.findAll('button').filter((button) => button.classes().includes('text-white'))
      expect(selected.length).toBeGreaterThan(0)
      for (const button of selected) {
        const computed = getComputedStyle(button.element)
        if (computed.backgroundImage === 'none' || !computed.backgroundImage) {
          expect(computed.backgroundColor).not.toBe('rgba(0, 0, 0, 0)')
        }
      }
    } finally { wrapper.unmount() }
  })
})
