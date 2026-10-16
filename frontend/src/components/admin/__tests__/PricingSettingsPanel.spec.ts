import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import PricingSettingsPanel from '../PricingSettingsPanel.vue'
const api = vi.hoisted(() => ({ get: vi.fn(), update: vi.fn(), refresh: vi.fn(), holidays: vi.fn(), stepUp: vi.fn((action: () => Promise<unknown>) => action()) }))
vi.mock('@/composables/useStepUp', () => ({ useStepUp: () => ({ run: api.stepUp }), isStepUpCancelled: () => false, isStepUpBlocked: () => false, stepUpBlockReason: () => '' }))
vi.mock('@/components/auth/TotpStepUpDialog.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/api/admin/pricing', () => ({ getPricingControls: api.get, updatePricingControls: api.update, refreshPricing: api.refresh, updateDeepseekHolidays: api.holidays }))
const app = vi.hoisted(() => ({ fetchPublicSettings: vi.fn(), showSuccess: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => app }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
enableAutoUnmount(afterEach)
const initial = { remote_enabled: true, native_cny_enabled: false, usd_model_count: 10, cny_model_count: 0, usd_last_updated: '', cny_last_updated: '', cny_error: '' }
beforeEach(() => { vi.resetAllMocks(); api.stepUp.mockImplementation((action) => action()); api.get.mockResolvedValue({ ...initial }); api.update.mockImplementation(async (controls) => ({ ...initial, ...controls })); api.refresh.mockResolvedValue({ ...initial }); app.fetchPublicSettings.mockResolvedValue(undefined) })
afterEach(() => vi.useRealTimers())
describe('pricing switches', () => {
  it('edits the yearly calendar through step-up without changing pricing switches', async () => {
    api.get.mockResolvedValue({ ...initial, deepseek_holidays: { '2026': ['2026-10-01'] }, deepseek_calendar_year_missing: false })
    api.update.mockResolvedValue({ ...initial, deepseek_holidays: { '2026': ['2026-10-01', '2026-10-02'] }, deepseek_calendar_year_missing: false })
    const wrapper = mount(PricingSettingsPanel); await flushPromises()
    await wrapper.get('[data-testid="calendar-draft"]').setValue('{"2026":["2026-10-01","2026-10-02"]}')
    await wrapper.get('[data-testid="calendar-save"]').trigger('click'); await flushPromises()
    expect(api.update).toHaveBeenCalledWith({ remote_enabled: true, native_cny_enabled: false, deepseek_holidays: { '2026': ['2026-10-01', '2026-10-02'] } })
    expect(api.stepUp).toHaveBeenCalledTimes(1)
  })
  it('keeps calendar edits when the server rejects the save', async () => {
    api.get.mockResolvedValue({ ...initial, deepseek_holidays: {}, deepseek_calendar_year_missing: false })
    api.update.mockRejectedValue({ response: { data: { message: 'calendar save failed' } } })
    const wrapper = mount(PricingSettingsPanel); await flushPromises()
    const draft = '{"2026":["2026-12-01"]}'
    await wrapper.get('[data-testid="calendar-draft"]').setValue(draft)
    await wrapper.get('[data-testid="calendar-save"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-testid="calendar-error"]').text()).toBe('calendar save failed')
    expect((wrapper.get('[data-testid="calendar-draft"]').element as HTMLTextAreaElement).value).toBe(draft)
  })
  it('shows calendar coverage and operational errors', async () => {
    api.get.mockResolvedValue({ ...initial, deepseek_calendar_years: [2026], deepseek_calendar_error: 'peak pricing disabled' })
    const wrapper = mount(PricingSettingsPanel); await flushPromises()
    expect(wrapper.text()).toContain('2026')
    expect(wrapper.get('[role="alert"]').text()).toBe('peak pricing disabled')
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

const maintained = { ...initial, native_cny_enabled: true, deepseek_holidays: { '2026': [] as string[] }, deepseek_calendar_year_missing: false }
const setupCalendar = async (overrides: Record<string, unknown> = {}) => {
  const state = { ...maintained, ...overrides }
  api.get.mockResolvedValue(state)
  api.update.mockImplementation(async (controls) => ({ ...state, ...controls }))
  const wrapper = mount(PricingSettingsPanel); await flushPromises()
  return wrapper
}
describe('DeepSeek calendar editor', () => {
  it('uses the server year-missing boolean, including an explicit {} calendar and a maintained empty year', async () => {
    const wrapper = await setupCalendar({ deepseek_holidays: {}, deepseek_calendar_year_missing: true })
    expect(wrapper.find('[data-testid="calendar-year-missing"]').exists()).toBe(true)
    expect((wrapper.get('[data-testid="calendar-draft"]').element as HTMLTextAreaElement).value).toBe('{}')
    expect(wrapper.get('[data-testid="deepseek-calendar"]').text()).toContain('admin.settings.pricing.calendar.rules')
    wrapper.unmount()
    const emptyYear = await setupCalendar({ deepseek_holidays: { '0001': [] }, deepseek_calendar_year_missing: false })
    expect(emptyYear.find('[data-testid="calendar-year-maintained"]').exists()).toBe(true)
    expect(emptyYear.find('[data-testid="calendar-year-missing"]').exists()).toBe(false)
  })
  it.each([
    {}, { deepseek_holidays: {} }, { deepseek_calendar_year_missing: false },
    { deepseek_holidays: null, deepseek_calendar_year_missing: false },
  ])('marks missing/partial old backend calendar support as unsupported (%s)', async (fields) => {
    api.get.mockResolvedValue({ ...initial, ...fields })
    const wrapper = mount(PricingSettingsPanel); await flushPromises()
    expect(wrapper.find('[data-testid="calendar-unsupported"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="calendar-draft"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="calendar-year-maintained"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="calendar-year-missing"]').exists()).toBe(false)
    expect(api.update).not.toHaveBeenCalled()
  })
  it('preserves unsaved drafts across polling while updating the separate server-saved state', async () => {
    vi.useFakeTimers()
    const wrapper = await setupCalendar()
    const draft = '{"2027":["2027-01-02"]}'
    await wrapper.get('[data-testid="calendar-draft"]').setValue(draft)
    api.get.mockResolvedValue({ ...maintained, deepseek_holidays: { '2028': [] }, deepseek_calendar_year_missing: true })
    await vi.advanceTimersByTimeAsync(5000); await flushPromises()
    expect((wrapper.get('[data-testid="calendar-draft"]').element as HTMLTextAreaElement).value).toBe(draft)
    expect(wrapper.get('[data-testid="calendar-saved-state"]').text()).toContain('2028')
    expect(wrapper.find('[data-testid="calendar-unsaved"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="calendar-year-missing"]').exists()).toBe(true)
    expect(api.update).not.toHaveBeenCalled()
  })
  it('switch PUT omits both saved and draft calendars and preserves the draft on server readback', async () => {
    const wrapper = await setupCalendar()
    const draft = '{"2027":[]}'
    await wrapper.get('[data-testid="calendar-draft"]').setValue(draft)
    api.update.mockResolvedValue({ ...maintained, native_cny_enabled: false, deepseek_holidays: { '2028': [] } })
    await wrapper.get('[data-testid="native-cny-toggle"]').setValue(false); await flushPromises()
    expect(api.update).toHaveBeenCalledWith({ remote_enabled: true, native_cny_enabled: false })
    expect(api.update.mock.calls[0][0]).not.toHaveProperty('deepseek_holidays')
    expect((wrapper.get('[data-testid="calendar-draft"]').element as HTMLTextAreaElement).value).toBe(draft)
    expect(wrapper.get('[data-testid="calendar-saved-state"]').text()).toContain('2028')
  })
  it('refresh readback updates saved state but never overwrites an unsaved calendar', async () => {
    const wrapper = await setupCalendar()
    const draft = '{"2027":[]}'
    await wrapper.get('[data-testid="calendar-draft"]').setValue(draft)
    api.get.mockResolvedValue({ ...maintained, deepseek_holidays: { '2028': [] } })
    await wrapper.get('[data-testid="refresh-pricing"]').trigger('click'); await flushPromises()
    expect(api.refresh).toHaveBeenCalledTimes(1)
    expect((wrapper.get('[data-testid="calendar-draft"]').element as HTMLTextAreaElement).value).toBe(draft)
    expect(wrapper.get('[data-testid="calendar-saved-state"]').text()).toContain('2028')
  })
  it.each(['null', '[]', '{"2026":null}', '{"0000":[]}', '{"26":[]}', '{"2026":["2026-02-29"]}', '{"2026":["2027-01-01"]}', '{"2026":["2026-10-12","2026-10-12"]}', '{'])('rejects invalid calendar draft %s without any PUT or change to effective state', async (draft) => {
    const wrapper = await setupCalendar()
    await wrapper.get('[data-testid="calendar-draft"]').setValue(draft)
    await wrapper.get('[data-testid="calendar-save"]').trigger('click'); await flushPromises()
    expect(api.update).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="calendar-error"]').exists()).toBe(true)
    expect((wrapper.get('[data-testid="calendar-draft"]').element as HTMLTextAreaElement).value).toBe(draft)
    expect(wrapper.get('[data-testid="calendar-saved-state"]').text()).toContain('2026')
  })
  it('requires an explicit {} edit/save to clear all years and applies only the server response', async () => {
    const wrapper = await setupCalendar()
    expect(api.update).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="calendar-draft"]').setValue('{}')
    api.update.mockResolvedValue({ ...maintained, deepseek_holidays: {}, deepseek_calendar_year_missing: true })
    await wrapper.get('[data-testid="calendar-save"]').trigger('click'); await flushPromises()
    expect(api.update).toHaveBeenCalledWith({ remote_enabled: true, native_cny_enabled: true, deepseek_holidays: {} })
    expect((wrapper.get('[data-testid="calendar-draft"]').element as HTMLTextAreaElement).value).toBe('{}')
    expect(wrapper.find('[data-testid="calendar-unsaved"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="calendar-year-missing"]').exists()).toBe(true)
    expect(app.fetchPublicSettings).toHaveBeenCalledWith(true)
    expect(app.showSuccess).toHaveBeenCalledWith('admin.settings.pricing.calendar.saved')
  })
  it('keeps a failed save draft and previous effective state rather than claiming it took effect', async () => {
    const wrapper = await setupCalendar()
    const draft = '{"2027":[]}'
    await wrapper.get('[data-testid="calendar-draft"]').setValue(draft)
    api.update.mockRejectedValueOnce(new Error('persistence failed'))
    await wrapper.get('[data-testid="calendar-save"]').trigger('click'); await flushPromises()
    expect((wrapper.get('[data-testid="calendar-draft"]').element as HTMLTextAreaElement).value).toBe(draft)
    expect(wrapper.get('[data-testid="calendar-saved-state"]').text()).toContain('2026')
    expect(wrapper.get('[data-testid="calendar-error"]').text()).toBe('persistence failed')
    expect(app.showSuccess).not.toHaveBeenCalled()
    expect(app.fetchPublicSettings).not.toHaveBeenCalled()
  })
  it('consumes canonical server readback instead of optimistically using the submitted map', async () => {
    const wrapper = await setupCalendar()
    await wrapper.get('[data-testid="calendar-draft"]').setValue('{"2027":["2027-01-02","2027-01-01"]}')
    api.update.mockResolvedValue({ ...maintained, deepseek_holidays: { '2027': ['2027-01-01', '2027-01-02'] } })
    await wrapper.get('[data-testid="calendar-save"]').trigger('click'); await flushPromises()
    expect(api.update).toHaveBeenCalledWith({ remote_enabled: true, native_cny_enabled: true, deepseek_holidays: { '2027': ['2027-01-02', '2027-01-01'] } })
    expect(JSON.parse((wrapper.get('[data-testid="calendar-draft"]').element as HTMLTextAreaElement).value)).toEqual({ '2027': ['2027-01-01', '2027-01-02'] })
    expect(wrapper.find('[data-testid="calendar-unsaved"]').exists()).toBe(false)
  })
  it('does not discard the draft or claim success when save returns an old/incomplete status', async () => {
    const wrapper = await setupCalendar()
    await wrapper.get('[data-testid="calendar-draft"]').setValue('{"2027":[]}')
    api.update.mockResolvedValue({ ...initial })
    await wrapper.get('[data-testid="calendar-save"]').trigger('click'); await flushPromises()
    expect((wrapper.get('[data-testid="calendar-draft"]').element as HTMLTextAreaElement).value).toBe('{"2027":[]}')
    expect(wrapper.get('[data-testid="calendar-saved-state"]').text()).toContain('2026')
    expect(wrapper.get('[data-testid="calendar-error"]').text()).toBe('admin.settings.pricing.calendar.missingReadback')
    expect(app.showSuccess).not.toHaveBeenCalled()
  })
  it('rejects a stale poll result after a calendar save without replacing the new effective map or draft', async () => {
    vi.useFakeTimers()
    const wrapper = await setupCalendar()
    let finishPoll!: (value: unknown) => void
    api.get.mockImplementationOnce(() => new Promise((resolve) => { finishPoll = resolve }))
    await vi.advanceTimersByTimeAsync(5000)
    await wrapper.get('[data-testid="calendar-draft"]').setValue('{"2027":[]}')
    api.update.mockResolvedValue({ ...maintained, deepseek_holidays: { '2027': [] } })
    await wrapper.get('[data-testid="calendar-save"]').trigger('click'); await flushPromises()
    finishPoll({ ...maintained, deepseek_holidays: { '2025': [] } }); await flushPromises()
    expect(wrapper.get('[data-testid="calendar-saved-state"]').text()).toContain('2027')
    expect(wrapper.get('[data-testid="calendar-saved-state"]').text()).not.toContain('2025')
    expect(JSON.parse((wrapper.get('[data-testid="calendar-draft"]').element as HTMLTextAreaElement).value)).toEqual({ '2027': [] })
  })
  it('does not publish a stale refresh result after a switch save and does not overwrite a dirty draft', async () => {
    const wrapper = await setupCalendar()
    let finishRefresh!: (value: unknown) => void
    api.refresh.mockImplementationOnce(() => new Promise((resolve) => { finishRefresh = resolve }))
    const draft = '{"2027":[]}'
    await wrapper.get('[data-testid="calendar-draft"]').setValue(draft)
    await wrapper.get('[data-testid="refresh-pricing"]').trigger('click')
    api.update.mockResolvedValue({ ...maintained, remote_enabled: false, deepseek_holidays: { '2028': [] } })
    await wrapper.get('[data-testid="remote-pull-toggle"]').setValue(false); await flushPromises()
    const readsBeforeCompletion = api.get.mock.calls.length
    finishRefresh({ ...maintained, deepseek_holidays: { '2025': [] } }); await flushPromises()
    expect(api.get.mock.calls.length).toBe(readsBeforeCompletion)
    expect(wrapper.get('[data-testid="calendar-saved-state"]').text()).toContain('2028')
    expect((wrapper.get('[data-testid="calendar-draft"]').element as HTMLTextAreaElement).value).toBe(draft)
    expect((wrapper.get('[data-testid="remote-pull-toggle"]').element as HTMLInputElement).checked).toBe(false)
  })
  it('does not start polling after unmount while the initial request is pending', async () => {
    vi.useFakeTimers()
    let finish!: (value: unknown) => void
    api.get.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve }))
    const wrapper = mount(PricingSettingsPanel)
    wrapper.unmount()
    finish(maintained); await flushPromises()
    await vi.advanceTimersByTimeAsync(10000)
    expect(api.get).toHaveBeenCalledTimes(1)
  })

  it('explicitly resets a dirty draft to the latest server value without issuing a write', async () => {
    const wrapper = await setupCalendar()
    await wrapper.get('[data-testid="calendar-draft"]').setValue('{"2027":[]}')
    await wrapper.get('[data-testid="calendar-reset"]').trigger('click')
    expect(JSON.parse((wrapper.get('[data-testid="calendar-draft"]').element as HTMLTextAreaElement).value)).toEqual({ '2026': [] })
    expect(api.update).not.toHaveBeenCalled()
  })
})

describe('pricing sync interval (PRD-01/K-6)', () => {
  it('saves the administrator-configured period and reports the effective value', async () => {
    api.get.mockResolvedValue({ ...initial, update_interval_hours: 0, effective_interval_hours: 24 })
    const wrapper = mount(PricingSettingsPanel); await flushPromises()
    // No stored override yet: the panel still renders the effective-period line
    // (this suite stubs t() to echo keys, so assert the key rather than the number).
    expect(wrapper.get('[data-testid="sync-interval-effective"]').text()).toContain('admin.settings.pricing.syncIntervalEffective')
    await wrapper.get('[data-testid="sync-interval-input"]').setValue(6)
    await wrapper.get('[data-testid="sync-interval-save"]').trigger('click'); await flushPromises()
    expect(api.update).toHaveBeenCalledWith({ remote_enabled: true, native_cny_enabled: false, update_interval_hours: 6 })
  })

  it('refuses an out-of-range period before calling the API', async () => {
    api.get.mockResolvedValue({ ...initial, update_interval_hours: 24, effective_interval_hours: 24 })
    const wrapper = mount(PricingSettingsPanel); await flushPromises()
    await wrapper.get('[data-testid="sync-interval-input"]').setValue(0)
    await wrapper.get('[data-testid="sync-interval-save"]').trigger('click'); await flushPromises()
    expect(api.update).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="sync-interval-error"]').exists()).toBe(true)
  })

  it('does not let a polling refresh overwrite an unsaved period draft', async () => {
    vi.useFakeTimers()
    api.get.mockResolvedValue({ ...initial, update_interval_hours: 12, effective_interval_hours: 12 })
    const wrapper = mount(PricingSettingsPanel); await flushPromises()
    await wrapper.get('[data-testid="sync-interval-input"]').setValue(3)
    // A poll returns the old stored value; the draft must survive it.
    await vi.advanceTimersByTimeAsync(60000); await flushPromises()
    expect((wrapper.get('[data-testid="sync-interval-input"]').element as HTMLInputElement).value).toBe('3')
  })
})
