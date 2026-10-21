import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import KeyHAGroupPicker from '../KeyHAGroupPicker.vue'
import type { Group } from '@/types'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

const groups = [
  { id: 1, name: 'Fallback A', status: 'active', rate_multiplier: 1, allow_high_availability: true },
  { id: 2, name: 'Normal B', status: 'active', rate_multiplier: 2, allow_high_availability: false },
  { id: 3, name: 'Disabled C', status: 'inactive', rate_multiplier: 3, allow_high_availability: true },
] as Group[]

const open = (modelValue: number[] = []) => mount(KeyHAGroupPicker, { props: { modelValue, groups } })

describe('KeyHAGroupPicker', () => {
  it('only offers groups the administrator marked as fallback and that are still active', () => {
    const wrapper = open()

    expect(wrapper.find('[data-test="ha-picker-1"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="ha-picker-2"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="ha-picker-3"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('emits the picked fallback groups', async () => {
    const wrapper = open()

    await wrapper.get('[data-test="ha-picker-1"]').trigger('change')

    expect(wrapper.emitted('update:modelValue')).toEqual([[[1]]])
    wrapper.unmount()
  })

  it('removes a group that is picked twice', async () => {
    const wrapper = open([1])

    await wrapper.get('[data-test="ha-picker-1"]').trigger('change')

    expect(wrapper.emitted('update:modelValue')).toEqual([[[]]])
    wrapper.unmount()
  })

  it('explains that no fallback group is available', () => {
    const wrapper = mount(KeyHAGroupPicker, { props: { modelValue: [], groups: [groups[1]!] as Group[] } })

    expect(wrapper.get('[data-test="ha-picker-empty"]').text()).toContain('keys.ha.empty')
    wrapper.unmount()
  })
})
