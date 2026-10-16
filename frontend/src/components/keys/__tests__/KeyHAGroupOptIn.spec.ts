import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import KeyHAGroupOptIn from '../KeyHAGroupOptIn.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

vi.mock('@/components/common/GroupBadge.vue', () => ({
  default: { name: 'GroupBadge', props: ['name', 'platform', 'rateMultiplier'], template: '<span>{{ name }}</span>' }
}))

const listKeyHAGroups = vi.fn()
const setKeyHAGroup = vi.fn()
vi.mock('@/api/keys', () => ({
  listKeyHAGroups: (...args: unknown[]) => listKeyHAGroups(...args),
  setKeyHAGroup: (...args: unknown[]) => setKeyHAGroup(...args)
}))

const candidate = (overrides: Record<string, unknown> = {}) => ({
  group_id: 9,
  name: 'fallback',
  platform: 'openai',
  rate_multiplier: 2.5,
  enabled: false,
  scope_compatible: true,
  access_granted: true,
  ...overrides
})

const mountOptIn = () => mount(KeyHAGroupOptIn, { props: { apiKeyId: 7 } })

describe('KeyHAGroupOptIn', () => {
  beforeEach(() => {
    listKeyHAGroups.mockReset()
    setKeyHAGroup.mockReset()
  })

  it('lists the candidates the admin marked as fallback', async () => {
    listKeyHAGroups.mockResolvedValue([candidate()])
    const wrapper = mountOptIn()
    await flushPromises()

    expect(listKeyHAGroups).toHaveBeenCalledWith(7)
    expect(wrapper.find('[data-test="ha-candidate-9"]').exists()).toBe(true)
    // The server-owned multiplier is displayed, never edited.
    expect(wrapper.text()).toContain('2.5x')
  })

  it('shows the empty state when no group is configured', async () => {
    listKeyHAGroups.mockResolvedValue([])
    const wrapper = mountOptIn()
    await flushPromises()

    expect(wrapper.find('[data-test="ha-empty"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="ha-candidate-9"]').exists()).toBe(false)
  })

  it('enables a candidate through the API and adopts the server response', async () => {
    listKeyHAGroups.mockResolvedValue([candidate()])
    setKeyHAGroup.mockResolvedValue({ enabled: true, rate_multiplier: 2.5, group_chain_revision: 3 })
    const wrapper = mountOptIn()
    await flushPromises()

    await wrapper.find('[data-test="ha-toggle-9"]').trigger('click')
    await flushPromises()

    // Consent is requested for this key and group; no rate is sent by the client.
    expect(setKeyHAGroup).toHaveBeenCalledWith(7, 9, true)
    expect(wrapper.find('[data-test="ha-toggle-9"]').text()).toBe('keys.ha.enabled')
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('withdraws consent when the candidate is already enabled', async () => {
    listKeyHAGroups.mockResolvedValue([candidate({ enabled: true })])
    setKeyHAGroup.mockResolvedValue({ enabled: false, rate_multiplier: 2.5, group_chain_revision: 4 })
    const wrapper = mountOptIn()
    await flushPromises()

    await wrapper.find('[data-test="ha-toggle-9"]').trigger('click')
    await flushPromises()

    expect(setKeyHAGroup).toHaveBeenCalledWith(7, 9, false)
    expect(wrapper.find('[data-test="ha-toggle-9"]').text()).toBe('keys.ha.enable')
  })

  it('blocks enabling a candidate whose model scope does not match the key', async () => {
    listKeyHAGroups.mockResolvedValue([candidate({ scope_compatible: false })])
    const wrapper = mountOptIn()
    await flushPromises()

    expect(wrapper.find('[data-test="ha-scope-mismatch"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="ha-toggle-9"]').attributes('disabled')).toBeDefined()

    await wrapper.find('[data-test="ha-toggle-9"]').trigger('click')
    expect(setKeyHAGroup).not.toHaveBeenCalled()
  })

  it('re-reads from the server when a write fails instead of trusting the optimistic guess', async () => {
    listKeyHAGroups.mockResolvedValue([candidate()])
    setKeyHAGroup.mockRejectedValue(new Error('forbidden'))
    const wrapper = mountOptIn()
    await flushPromises()

    await wrapper.find('[data-test="ha-toggle-9"]').trigger('click')
    await flushPromises()

    // Initial load + reload after the failure.
    expect(listKeyHAGroups).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('changed')).toBeUndefined()
  })

  it('does not request a list without a key id', async () => {
    const wrapper = mount(KeyHAGroupOptIn, { props: { apiKeyId: 0 } })
    await flushPromises()

    expect(listKeyHAGroups).not.toHaveBeenCalled()
    expect(wrapper.find('[data-test="ha-empty"]').exists()).toBe(true)
  })
})

  it('does not offer an opt-in the server would reject (FE-02)', async () => {
    // A group the user cannot access must be shown as unavailable and not enableable,
    // rather than advertising an opt-in the write path refuses.
    listKeyHAGroups.mockResolvedValue([candidate({ access_granted: false })])
    const wrapper = mountOptIn()
    await flushPromises()

    expect(wrapper.find('[data-test="ha-access-required"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="ha-toggle-9"]').attributes('disabled')).toBeDefined()
  })

