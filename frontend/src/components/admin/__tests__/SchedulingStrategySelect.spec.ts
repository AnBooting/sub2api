import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import SchedulingStrategySelect from '../SchedulingStrategySelect.vue'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
describe('scheduling strategy selector', () => {
  it('offers exactly the three global strategies', async () => {
    const wrapper = mount(SchedulingStrategySelect, { props: { modelValue: 'balanced' } })
    expect(wrapper.findAll('option').map((option) => option.attributes('value'))).toEqual(['stable', 'balanced', 'latency'])
    await wrapper.get('select').setValue('latency')
    expect(wrapper.emitted('update:modelValue')).toEqual([['latency']])
  })
  it('supports group inheritance and displays server effective value', async () => {
    const wrapper = mount(SchedulingStrategySelect, { props: { modelValue: 'inherit', allowInherit: true, effectiveStrategy: 'stable' } })
    expect(wrapper.findAll('option')).toHaveLength(4)
    expect(wrapper.get('[data-test="effective-strategy"]').text()).toContain('strategies.stable')
    await wrapper.get('select').setValue('balanced')
    expect(wrapper.emitted('update:modelValue')).toEqual([['balanced']])
  })
  it('does not emit changes when disabled', async () => {
    const wrapper = mount(SchedulingStrategySelect, { props: { modelValue: 'stable', disabled: true } })
    await wrapper.get('select').setValue('latency')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
})
