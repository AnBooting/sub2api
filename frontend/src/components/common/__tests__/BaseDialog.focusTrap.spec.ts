import { afterEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import BaseDialog from '../BaseDialog.vue'

enableAutoUnmount(afterEach)

interface MountOptions {
  show?: boolean
  title?: string
  showCloseButton?: boolean
  html?: string
}

function mountDialog(options: MountOptions = {}) {
  return mount(BaseDialog, {
    props: {
      show: options.show ?? true,
      title: options.title ?? 'Details',
      showCloseButton: options.showCloseButton ?? false
    },
    slots: { default: options.html ?? '' },
    global: { stubs: { Icon: true } }
  })
}

const settle = async () => {
  await nextTick()
  await nextTick()
}

const byId = (id: string) => document.getElementById(id) as HTMLButtonElement | null

function pressTab(target: Element | null, shiftKey = false) {
  const event = new KeyboardEvent('keydown', { key: 'Tab', shiftKey, bubbles: true, cancelable: true })
  ;(target ?? document).dispatchEvent(event)
  return event
}

const twoButtons = '<button id="first">First</button><button id="last">Last</button>'

describe('BaseDialog focus trap', () => {
  afterEach(() => {
    document.querySelectorAll('[data-outside-tab-target]').forEach(node => node.remove())
  })

  it('wraps from the last focusable back to the first', async () => {
    mountDialog({ html: twoButtons })
    await settle()

    expect(document.activeElement).toBe(byId('first'))

    byId('last')!.focus()
    const event = pressTab(byId('last'))

    expect(event.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(byId('first'))
  })

  it('wraps backwards from the first focusable to the last', async () => {
    mountDialog({ html: twoButtons })
    await settle()

    expect(document.activeElement).toBe(byId('first'))
    const event = pressTab(byId('first'), true)

    expect(event.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(byId('last'))
  })

  it('pulls focus back when it escaped to the page behind', async () => {
    mountDialog({ html: twoButtons })
    await settle()

    const outside = document.createElement('button')
    outside.setAttribute('data-outside-tab-target', '')
    document.body.appendChild(outside)
    outside.focus()
    expect(document.activeElement).toBe(outside)

    const event = pressTab(outside)

    expect(event.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(byId('first'))
  })

  it('leaves Tab inside the dialog to the browser', async () => {
    mountDialog({ html: '<button id="first">First</button><button id="mid">Mid</button><button id="last">Last</button>' })
    await settle()

    byId('first')!.focus()
    const event = pressTab(byId('first'))

    expect(event.defaultPrevented).toBe(false)
    expect(document.activeElement).toBe(byId('first'))
  })

  it('only the topmost dialog traps focus', async () => {
    mountDialog({ title: 'Parent', html: '<button id="parent-first">P1</button><button id="parent-last">P2</button>' })
    mountDialog({ title: 'Child', html: '<button id="child-first">C1</button><button id="child-last">C2</button>' })
    await settle()

    byId('child-last')!.focus()
    const event = pressTab(byId('child-last'))

    expect(event.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(byId('child-first'))
  })

  it('blocks Tab when the dialog has no focusable content', async () => {
    mountDialog({ html: '<p>Nothing to focus here</p>' })
    await settle()

    const event = pressTab(document.body)

    expect(event.defaultPrevented).toBe(true)
  })

  it('skips disabled controls when choosing the initial focus', async () => {
    mountDialog({ html: '<button id="disabled" disabled>Disabled</button><button id="enabled">Enabled</button>' })
    await settle()

    expect(document.activeElement).toBe(byId('enabled'))
  })
})
