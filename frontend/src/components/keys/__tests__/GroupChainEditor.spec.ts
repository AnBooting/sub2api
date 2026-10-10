import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import GroupChainEditor from '../GroupChainEditor.vue'
import type { Group } from '@/types'
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string, params?: unknown) => key + JSON.stringify(params ?? {}) }) }))
const groups = [
  { id: 1, name: 'A', status: 'active', rate_multiplier: 1 },
  { id: 2, name: 'B', status: 'active', rate_multiplier: 2 },
  { id: 3, name: 'Disabled', status: 'inactive', rate_multiplier: 3 },
] as Group[]
const open = (ids: number[], disabled = false) => mount(GroupChainEditor, {
  props: { modelValue: ids, groups, disabled, userRates: { 2: 0.5 } },
  global: { stubs: { GroupBadge: { props: ['name', 'rateMultiplier', 'userRateMultiplier'], template: '<span>{{ name }}:{{ userRateMultiplier ?? rateMultiplier }}x</span>' } } },
})
describe('GroupChainEditor', () => {
  it('renders the saved order and user effective rate, supports keyboard-native up/down', async () => {
    const wrapper = open([2, 1])
    expect(wrapper.findAll('li').map((item) => item.attributes('data-group-id'))).toEqual(['2', '1'])
    expect(wrapper.text()).toContain('B:0.5x')
    expect(wrapper.get('[data-action="up"]').attributes('disabled')).toBeDefined()
    await wrapper.findAll('[data-action="up"]')[1].trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[[1, 2]]])
  })
  it('adds only available active unselected groups and never duplicates them', async () => {
    const wrapper = open([1])
    expect(wrapper.findAll('option').map((option) => option.attributes('value'))).toEqual(['', '2'])
    await wrapper.get('select').setValue('2')
    expect(wrapper.emitted('update:modelValue')).toEqual([[[1, 2]]])
    await wrapper.setProps({ modelValue: [1, 2] })
    expect(wrapper.get('select').attributes('disabled')).toBeDefined()
  })
  it('retains unavailable saved groups visibly until explicitly removed', async () => {
    const wrapper = open([3, 99])
    expect(wrapper.findAll('li')).toHaveLength(2)
    expect(wrapper.text()).toContain('keys.chain.unavailable')
    await wrapper.findAll('[data-action="remove"]')[1].trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[[3]]])
  })
  it('prevents mutations while saving', async () => {
    const wrapper = open([1, 2], true)
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    await wrapper.findAll('[data-action="down"]')[0].trigger('click')
    await wrapper.findAll('[data-action="remove"]')[0].trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
})
