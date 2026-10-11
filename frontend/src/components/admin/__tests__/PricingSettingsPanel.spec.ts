import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import PricingSettingsPanel from '../PricingSettingsPanel.vue'
const api = vi.hoisted(() => ({ get: vi.fn(), update: vi.fn(), refresh: vi.fn(), holidays: vi.fn() }))
vi.mock('@/api/admin/pricing', () => ({ getPricingControls: api.get, updatePricingControls: api.update, refreshPricing: api.refresh, updateDeepseekHolidays: api.holidays }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ fetchPublicSettings: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
enableAutoUnmount(afterEach)
const initial = { remote_enabled: true, native_cny_enabled: false, usd_model_count: 10, cny_model_count: 0, usd_last_updated: '', cny_last_updated: '', cny_error: '' }
beforeEach(() => { vi.clearAllMocks(); api.get.mockResolvedValue({ ...initial }); api.update.mockImplementation(async (controls) => ({ ...initial, ...controls })) })
describe('pricing switches', () => {
  it('edits the Beijing holiday calendar without changing pricing switches', async () => {
    api.get.mockResolvedValue({ ...initial, deepseek_holidays: ['2026-10-01'] })
    api.holidays.mockResolvedValue({ ...initial, deepseek_holidays: ['2026-10-01', '2026-10-02'] })
    const wrapper = mount(PricingSettingsPanel); await flushPromises()
    expect((wrapper.get('[data-testid="deepseek-holidays"]').element as HTMLTextAreaElement).value).toBe('2026-10-01')
    await wrapper.get('[data-testid="deepseek-holidays"]').setValue('2026-10-01\n2026-10-02')
    await wrapper.get('[data-testid="save-deepseek-holidays"]').trigger('click'); await flushPromises()
    expect(api.holidays).toHaveBeenCalledWith(['2026-10-01', '2026-10-02'])
    expect(api.update).not.toHaveBeenCalled()
  })

  it('keeps holiday edits when the server rejects an invalid date', async () => {
    api.holidays.mockRejectedValue({ response: { data: { message: 'invalid holiday date' } } })
    const wrapper = mount(PricingSettingsPanel); await flushPromises()
    await wrapper.get('[data-testid="deepseek-holidays"]').setValue('2026-02-30')
    await wrapper.get('[data-testid="save-deepseek-holidays"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('invalid holiday date')
    expect((wrapper.get('[data-testid="deepseek-holidays"]').element as HTMLTextAreaElement).value).toBe('2026-02-30')
  })
  it('disables all pulls while a manual refresh is still running', async () => {
    let finish!: (value: unknown) => void
    api.refresh.mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const wrapper = mount(PricingSettingsPanel); await flushPromises()
    await wrapper.get('[data-testid="refresh-pricing"]').trigger('click')
    expect((wrapper.get('[data-testid="remote-pull-toggle"]').element as HTMLInputElement).disabled).toBe(false)
    await wrapper.get('[data-testid="remote-pull-toggle"]').setValue(false); await flushPromises()
    expect(api.update).toHaveBeenCalledWith({ remote_enabled: false, native_cny_enabled: false })
    api.get.mockResolvedValue({ ...initial, remote_enabled: false })
    finish({}); await flushPromises()
    expect((wrapper.get('[data-testid="remote-pull-toggle"]').element as HTMLInputElement).checked).toBe(false)
    expect((wrapper.get('[data-testid="refresh-pricing"]').element as HTMLButtonElement).disabled).toBe(true)
  })
  it('saves native CNY mode independently of remote pulls', async () => {
    api.get.mockResolvedValue({ ...initial, remote_enabled: false })
    const wrapper = mount(PricingSettingsPanel); await flushPromises()
    await wrapper.get('[data-testid="native-cny-toggle"]').setValue(true); await flushPromises()
    expect(api.update).toHaveBeenCalledWith({ remote_enabled: false, native_cny_enabled: true })
    expect(api.refresh).not.toHaveBeenCalled()
  })
})
