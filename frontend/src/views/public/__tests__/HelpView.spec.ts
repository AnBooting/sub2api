import { describe, expect, it, vi } from 'vitest'

// 生产构建通过 vite.config.ts 的 define 打开 vue-i18n 的 JIT 编译；
// Vitest 不继承该 define，这里补上，否则 t() 无法编译文案。
vi.hoisted(() => {
  vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true)
})

import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

import HelpView from '../HelpView.vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

type HelpLocale = typeof zh.help

function mountHelp(locale: 'zh' | 'en') {
  const i18n = createI18n({
    legacy: false,
    locale,
    fallbackLocale: 'en',
    messages: { zh, en },
  })

  return mount(HelpView, { global: { plugins: [i18n] } })
}

/**
 * 找到包含指定文本的表格。
 * 用唯一行内容定位，而不是按出现顺序取下标——表头文案在不同小节之间可能重复
 * （usage 与 videoTasks 的表头都是「字段/含义」），按下标取会取错表。
 */
function tableContaining(wrapper: ReturnType<typeof mountHelp>, needle: string) {
  const table = wrapper.findAll('table').find((candidate) => candidate.text().includes(needle))
  expect(table, 'no table containing ' + needle).toBeTruthy()
  return table!
}

// 用户篇小节：与 HelpView.vue 中的 userSections 保持一致。
const USER_SECTIONS = [
  'credit',
  'peak',
  'groups',
  'pricingTable',
  'recharge',
  'usage',
  'media',
  'videoTasks',
  'faq',
  'contact',
] as const
// 管理员小节：与 HelpView.vue 中的 adminSections 保持一致。
const ADMIN_SECTIONS = ['pricing', 'scheduling', 'concurrency', 'reconciliation'] as const

describe('HelpView', () => {
  it('renders the page title and subtitle from the active locale', () => {
    const wrapper = mountHelp('zh')

    expect(wrapper.get('h1').text()).toBe(zh.help.title)
    expect(wrapper.text()).toContain(zh.help.subtitle)
    expect(wrapper.text()).toContain(zh.help.userTitle)
  })

  it('renders every user guide section with its heading, intro and body', () => {
    const wrapper = mountHelp('zh')
    // 管理员小节的 h3 在 <details> 内，用户篇 h3 不在，需要区分。
    const headings = wrapper
      .findAll('h3')
      .filter((heading) => !heading.element.closest('details'))
      .map((heading) => heading.text())

    expect(headings).toEqual(USER_SECTIONS.map((section) => zh.help.user[section].title))

    // 每个用户篇小节的导语都要出现（用户篇共 7 节）。
    const text = wrapper.text()
    for (const section of USER_SECTIONS) {
      expect(text, 'missing intro for ' + section).toContain(zh.help.user[section].intro)
    }
  })

  it('renders the FAQ questions and answers as a Q/A list', () => {
    const wrapper = mountHelp('zh')
    const faq = wrapper.findAll('dl')

    expect(faq).toHaveLength(1)

    const questions = faq[0].findAll('dt').map((term) => term.text())
    const answers = faq[0].findAll('dd').map((definition) => definition.text())

    expect(questions).toEqual(Object.values(zh.help.user.faq.items).map((item) => item.q))
    expect(answers).toEqual(Object.values(zh.help.user.faq.items).map((item) => item.a))
    expect(questions).toContain('为什么这次比上次贵？')
    expect(answers[0]).toContain('实际服务分组')
  })

  it('renders the path lookup table with a header and one row per entry', () => {
    const wrapper = mountHelp('zh')
    const pathsTable = tableContaining(wrapper, zh.help.paths.rows.keys.t)
    const pathRows = pathsTable.findAll('tbody tr')

    expect(pathsTable.findAll('th').map((cell) => cell.text())).toEqual([
      zh.help.paths.tableHead.t,
      zh.help.paths.tableHead.d,
    ])
    expect(pathRows).toHaveLength(Object.keys(zh.help.paths.rows).length)
    expect(pathRows.map((row) => row.findAll('td')[1].text())).toEqual(
      Object.values(zh.help.paths.rows).map((row) => row.d)
    )
    expect(wrapper.text()).toContain('/keys')
    expect(wrapper.text()).toContain('/purchase')
    expect(wrapper.text()).toContain('/video-tasks')
  })

  it('renders each rows-based section with a header taken from its own locale entry', () => {
    const wrapper = mountHelp('zh')

    // recharge / usage / videoTasks / contact / paths 都有 rows，表头必须来自 locale 的
    // tableHead，而不是空单元格（空表头说明该小节漏了 tableHead）。
    const cases: Array<[string, { t: string; d: string }]> = [
      [zh.help.user.recharge.rows.purchase.t, zh.help.user.recharge.tableHead],
      [zh.help.user.usage.rows.recordCost.t, zh.help.user.usage.tableHead],
      [zh.help.user.videoTasks.rows.taskNo.t, zh.help.user.videoTasks.tableHead],
      [zh.help.user.contact.rows.topup.t, zh.help.user.contact.tableHead],
      [zh.help.paths.rows.keys.t, zh.help.paths.tableHead],
    ]

    for (const [anchor, head] of cases) {
      const table = tableContaining(wrapper, anchor)
      expect(table.findAll('th').map((cell) => cell.text())).toEqual([head.t, head.d])
    }
  })

  it('renders the peak/off-peak section with the official windows and multiplier', () => {
    const wrapper = mountHelp('zh')
    const text = wrapper.text()

    expect(text).toContain(zh.help.user.peak.title)
    expect(text).toContain(zh.help.user.peak.intro)
    expect(text).toContain(zh.help.user.peak.bullets.window)
    expect(text).toContain(zh.help.user.peak.bullets.holiday)
    expect(text).toContain(zh.help.user.peak.bullets.multiplier)
    expect(text).toContain(zh.help.user.peak.bullets.display)
    expect(text).toContain(zh.help.user.peak.note)
  })

  it('renders the missing-price and price-overview section', () => {
    const wrapper = mountHelp('zh')
    const text = wrapper.text()

    expect(text).toContain(zh.help.user.pricingTable.title)
    expect(text).toContain(zh.help.user.pricingTable.bullets.meaning)
    expect(text).toContain(zh.help.user.pricingTable.bullets.adminTable)
    expect(text).toContain(zh.help.user.pricingTable.bullets.adminMissing)
    expect(text).toContain(zh.help.user.pricingTable.bullets.probe)
  })

  it('renders the video task section with its fields and billing snapshot', () => {
    const wrapper = mountHelp('zh')
    const text = wrapper.text()
    const table = tableContaining(wrapper, zh.help.user.videoTasks.rows.taskNo.t)

    expect(text).toContain(zh.help.user.videoTasks.title)
    expect(text).toContain(zh.help.user.videoTasks.bullets.snapshot)
    expect(text).toContain(zh.help.user.videoTasks.bullets.paging)
    expect(table.findAll('tbody tr')).toHaveLength(Object.keys(zh.help.user.videoTasks.rows).length)
    expect(table.findAll('th').map((cell) => cell.text())).toEqual([
      zh.help.user.videoTasks.tableHead.t,
      zh.help.user.videoTasks.tableHead.d,
    ])
  })

  it('renders the section notes as closing paragraphs', () => {
    const wrapper = mountHelp('zh')
    const text = wrapper.text()

    expect(text).toContain(zh.help.user.credit.note)
    expect(text).toContain(zh.help.user.usage.note)
    expect(text).toContain(zh.help.user.contact.note)
  })

  it('keeps the admin guide collapsed by default while its content stays in the DOM', () => {
    const wrapper = mountHelp('zh')
    const details = wrapper.find('details')

    expect(details.exists()).toBe(true)
    expect(details.attributes('open')).toBeUndefined()
    expect(details.get('summary').text()).toBe(zh.help.adminTitle)

    // 折叠只影响展示：管理员小节标题与文案仍然渲染在 DOM 里。
    const text = wrapper.text()
    expect(text).toContain(zh.help.adminIntro)
    for (const section of ADMIN_SECTIONS) {
      expect(text, 'missing admin section ' + section).toContain(zh.help.admin[section].title)
    }
    expect(text).toContain(zh.help.admin.pricing.bullets.missing)
    expect(text).toContain(zh.help.admin.reconciliation.note)
  })

  it('renders the English copy when the active locale is en', () => {
    const wrapper = mountHelp('en')

    expect(wrapper.get('h1').text()).toBe(en.help.title)
    expect(wrapper.text()).toContain(en.help.user.faq.items.q1.q)
    expect(wrapper.text()).toContain(en.help.paths.rows.keys.d)
    expect(wrapper.text()).toContain(en.help.admin.concurrency.title)
    expect(wrapper.text()).not.toContain(zh.help.user.faq.items.q1.q)
  })
})

describe('help locale structure contract', () => {
  const locales: Array<[string, HelpLocale]> = [
    ['zh', zh.help],
    ['en', en.help],
  ]

  it.each(locales)('%s keeps bullets as plain strings and rows/items as { t, d } / { q, a }', (_name, help) => {
    for (const section of [...USER_SECTIONS, ...ADMIN_SECTIONS]) {
      const isUser = (USER_SECTIONS as readonly string[]).includes(section)
      const node = isUser
        ? (help.user as Record<string, any>)[section]
        : (help.admin as Record<string, any>)[section]

      expect(node.title, section + ' is missing a title').toBeTruthy()
      expect(node.intro, section + ' is missing an intro').toBeTruthy()

      if (node.bullets) {
        for (const [slug, value] of Object.entries(node.bullets)) {
          expect(typeof value, section + '.bullets.' + slug + ' must be a string').toBe('string')
        }
      }

      if (node.rows) {
        for (const [slug, value] of Object.entries(node.rows)) {
          expect(typeof value.t, section + '.rows.' + slug + '.t must be a string').toBe('string')
          expect(typeof value.d, section + '.rows.' + slug + '.d must be a string').toBe('string')
        }
        // rows 小节必须带表头，否则页面上是一行空单元格。
        expect(node.tableHead, section + ' has rows but no tableHead').toBeTruthy()
        expect(typeof node.tableHead.t, section + '.tableHead.t must be a string').toBe('string')
        expect(typeof node.tableHead.d, section + '.tableHead.d must be a string').toBe('string')
      }

      if (node.items) {
        for (const [slug, value] of Object.entries(node.items)) {
          expect(typeof value.q, section + '.items.' + slug + '.q must be a string').toBe('string')
          expect(typeof value.a, section + '.items.' + slug + '.a must be a string').toBe('string')
        }
      }
    }
  })

  it.each(locales)('%s gives the path lookup table a header and one row per entry', (_name, help) => {
    expect(typeof help.paths.tableHead.t).toBe('string')
    expect(typeof help.paths.tableHead.d).toBe('string')
    expect(Object.keys(help.paths.rows).length).toBeGreaterThan(0)
  })
})
