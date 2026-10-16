import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import UserApiKeysModal from '../UserApiKeysModal.vue'
import type { AdminUser } from '@/types'

const { getKeys, updateGroups, showError } = vi.hoisted(() => ({ getKeys: vi.fn(), updateGroups: vi.fn(), showError: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: {
  users: { getUserApiKeys: getKeys }, groups: { getAll: vi.fn().mockResolvedValue([]) }, apiKeys: { updateApiKeyGroups: updateGroups },
} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess: vi.fn() }) }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => { getKeys.mockReset(); updateGroups.mockReset(); showError.mockReset(); vi.spyOn(console, 'error').mockImplementation(() => {}) })
afterEach(() => vi.restoreAllMocks())
function deferred() {
  let resolve!: (value: unknown) => void
  let reject!: (error: Error) => void
  const promise = new Promise((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
const user = (id: number) => ({ id, email: `user${id}@example.com`, username: `user${id}` }) as AdminUser
const keys = (id: number, name: string) => ({ items: [{ id, name, key: 'sk-example-key-value-for-tests', status: 'active', created_at: '2026-09-20', group_id: null, group_chain_revision: 0 }] })
async function open() {
  const wrapper = mount(UserApiKeysModal, {
    props: { show: false, user: user(1) },
    global: { stubs: {
      BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
      GroupBadge: true, GroupOptionItem: true,
    } },
  })
  await wrapper.setProps({ show: true })
  return wrapper
}
async function switchUser(wrapper: Awaited<ReturnType<typeof open>>) {
  await wrapper.setProps({ show: false })
  await wrapper.setProps({ show: true, user: user(2) })
}

describe('user API key loading', () => {
  it('does not display the previous user keys when the next load fails', async () => {
    getKeys.mockResolvedValueOnce(keys(1, 'first-user-key')).mockRejectedValueOnce(new Error('unavailable'))
    const wrapper = await open(); await flushPromises()
    expect(wrapper.text()).toContain('first-user-key')
    await switchUser(wrapper); await flushPromises()
    expect(wrapper.text()).toContain('user2@example.com')
    expect(wrapper.text()).not.toContain('first-user-key')
  })

  it('does not replace current keys with a late previous response', async () => {
    const old = deferred()
    getKeys.mockReturnValueOnce(old.promise).mockResolvedValueOnce(keys(2, 'current-user-key'))
    const wrapper = await open()
    await switchUser(wrapper); await flushPromises()
    old.resolve(keys(1, 'old-user-key')); await flushPromises()
    expect(wrapper.text()).toContain('current-user-key')
    expect(wrapper.text()).not.toContain('old-user-key')
  })

  it('keeps the current request loading when an obsolete request fails', async () => {
    const old = deferred(); const current = deferred()
    getKeys.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const wrapper = await open()
    await switchUser(wrapper)
    old.reject(new Error('obsolete')); await flushPromises()
    expect(wrapper.find('.animate-spin').exists()).toBe(true)
    current.resolve(keys(2, 'current-user-key')); await flushPromises()
    expect(wrapper.text()).toContain('current-user-key')
    expect(wrapper.find('.animate-spin').exists()).toBe(false)
  })

  it('loads keys when the selected user changes while the dialog is open', async () => {
    getKeys.mockResolvedValueOnce(keys(1, 'first-user-key')).mockResolvedValueOnce(keys(2, 'second-user-key'))
    const wrapper = await open(); await flushPromises()
    await wrapper.setProps({ user: user(2) }); await flushPromises()
    expect(getKeys).toHaveBeenLastCalledWith(2)
    expect(wrapper.text()).toContain('second-user-key')
    expect(wrapper.text()).not.toContain('first-user-key')
  })

  it('saves an admin normal-only chain with the read revision and keeps HA out of the request', async () => {
    const key = { ...keys(1, 'chain-key').items[0], group_id: 1, group_chain_revision: 9, groups: [
      { group_id: 1, role: 'normal', position: 0 },
      { group_id: 8, role: 'ha', position: 0, enabled_by_user_at: 'server-only' },
    ] }
    getKeys.mockResolvedValue({ items: [key] })
    updateGroups.mockResolvedValue({ api_key: { ...key, group_chain_revision: 10 } })
    const wrapper = await open(); await flushPromises()
    await wrapper.findAll('button').find((button) => button.text() === 'keys.chain.edit')!.trigger('click')
    const editor = wrapper.findComponent({ name: 'GroupChainEditor' })
    editor.vm.$emit('update:modelValue', [2, 1])
    await wrapper.findAll('button').find((button) => button.text() === 'common.save')!.trigger('click')
    await flushPromises()
    expect(updateGroups).toHaveBeenCalledWith(1, [
      { group_id: 2, role: 'normal', position: 0 },
      { group_id: 1, role: 'normal', position: 1 },
    ], 9)
  })

  it('on conflict shows a refresh prompt without retrying the admin chain', async () => {
    getKeys.mockResolvedValue(keys(1, 'key'))
    updateGroups.mockRejectedValue({ status: 409, code: 'group_chain_conflict' })
    const wrapper = await open(); await flushPromises()
    await wrapper.findAll('button').find((button) => button.text() === 'keys.chain.edit')!.trigger('click')
    await wrapper.findAll('button').find((button) => button.text() === 'common.save')!.trigger('click')
    await flushPromises()
    expect(updateGroups).toHaveBeenCalledTimes(1)
    expect(updateGroups).toHaveBeenCalledWith(1, [], 0)
    expect(showError).toHaveBeenCalledWith('keys.chain.conflict')
  })
})
