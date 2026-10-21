import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import GroupPeakWindowStatus from '../GroupPeakWindowStatus.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key}:${JSON.stringify(params)}` : key
    })
  }
})

describe('GroupPeakWindowStatus', () => {
  it('未启用时列表里不显示', () => {
    const wrapper = mount(GroupPeakWindowStatus, {
      props: { enabled: false, start: '09:00', end: '12:00', multiplier: 2 }
    })
    expect(wrapper.find('[data-testid="group-peak-window"]').exists()).toBe(false)
  })

  it('showWhenDisabled 时显示未启用状态', () => {
    const wrapper = mount(GroupPeakWindowStatus, {
      props: { enabled: false, showWhenDisabled: true }
    })
    const badge = wrapper.get('[data-testid="group-peak-window"]')
    expect(badge.attributes('data-state')).toBe('disabled')
    expect(badge.text()).toBe('admin.groups.peakWindow.disabled')
  })

  it('高峰时段显示当前倍率', () => {
    const wrapper = mount(GroupPeakWindowStatus, {
      props: {
        enabled: true,
        start: '09:00',
        end: '12:00',
        multiplier: 2,
        now: new Date('2026-10-21T02:00:00Z')
      }
    })
    const badge = wrapper.get('[data-testid="group-peak-window"]')
    expect(badge.attributes('data-state')).toBe('peak')
    expect(badge.text()).toContain('admin.groups.peakWindow.peak')
  })

  it('提示语包含时段、倍率与下次切换时间', () => {
    const wrapper = mount(GroupPeakWindowStatus, {
      props: {
        enabled: true,
        start: '09:00',
        end: '12:00',
        multiplier: 2,
        now: new Date('2026-10-21T02:00:00Z')
      }
    })
    const title = wrapper.get('[data-testid="group-peak-window"]').attributes('title')
    expect(title).toContain('admin.groups.peakWindow.window')
    expect(title).toContain('admin.groups.peakWindow.multiplier')
    expect(title).toContain('admin.groups.peakWindow.nextSwitch')
  })

  it('配置无效时显示无效状态', () => {
    const wrapper = mount(GroupPeakWindowStatus, {
      props: {
        enabled: true,
        start: '12:00',
        end: '09:00',
        multiplier: 2,
        now: new Date('2026-10-21T02:00:00Z')
      }
    })
    expect(
      wrapper.get('[data-testid="group-peak-window"]').attributes('data-state')
    ).toBe('invalid')
  })
})
