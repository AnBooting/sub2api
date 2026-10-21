import { beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach } from 'vitest'
import PricingSettingsPanel from '../PricingSettingsPanel.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), update: vi.fn(), refresh: vi.fn(), holidays: vi.fn(), stepUp: vi.fn((action: () => Promise<unknown>) => action()) }))
vi.mock('@/composables/useStepUp', () => ({ useStepUp: () => ({ run: api.stepUp }), isStepUpCancelled: () => false, isStepUpBlocked: () => false, stepUpBlockReason: () => '' }))
vi.mock('@/components/auth/TotpStepUpDialog.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/api/admin/pricing', () => ({ getPricingControls: api.get, updatePricingControls: api.update, refreshPricing: api.refresh, updateDeepseekHolidays: api.holidays }))
const app = vi.hoisted(() => ({ fetchPublicSettings: vi.fn(), showSuccess: vi.fn() }))
vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}:${JSON.stringify(params)}` : key
  })
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => app }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
enableAutoUnmount(afterEach)

const initial = {
  remote_enabled: true,
  native_cny_enabled: false,
  usd_model_count: 10,
  cny_model_count: 0,
  usd_last_updated: '',
  cny_last_updated: '',
  cny_error: ''
}

const mountPanel = async (overrides: Record<string, unknown> = {}) => {
  api.get.mockResolvedValue({ ...initial, ...overrides })
  const wrapper = mount(PricingSettingsPanel)
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.resetAllMocks()
  api.stepUp.mockImplementation((action) => action())
  app.fetchPublicSettings.mockResolvedValue(undefined)
})

describe('DeepSeek 官方峰谷当前档位', () => {
  it('后端没有这个字段时不显示这一块', async () => {
    const wrapper = await mountPanel()
    expect(wrapper.find('[data-testid="deepseek-period"]').exists()).toBe(false)
  })

  it('显示服务端给的档位文案、倍率与下一档切换时间', async () => {
    const wrapper = await mountPanel({
      deepseek_current_period: {
        state: 'peak',
        label: '官方高峰时段',
        timezone: 'Asia/Shanghai',
        multiplier: 2,
        next_switch_at: '2026-10-21T11:00:00+08:00'
      }
    })
    const section = wrapper.get('[data-testid="deepseek-period"]')
    expect(section.text()).toContain('官方高峰时段')
    expect(wrapper.get('[data-testid="deepseek-period-multiplier"]').text()).toContain('2')
    const next = wrapper.get('[data-testid="deepseek-period-next-switch"]').text()
    expect(next).toContain('2026-10-21 11:00')
    expect(next).toContain('Asia/Shanghai')
  })

  it('当年假日日历缺失时给出提示', async () => {
    const wrapper = await mountPanel({
      deepseek_current_period: { state: 'valley', label: '谷价时段' },
      deepseek_calendar_year_missing: true
    })
    expect(wrapper.find('[data-testid="deepseek-period-year-missing"]').exists()).toBe(true)
  })

  it('日历已维护时不显示缺失提示', async () => {
    const wrapper = await mountPanel({
      deepseek_current_period: { state: 'valley', label: '谷价时段' },
      deepseek_calendar_year_missing: false
    })
    expect(wrapper.find('[data-testid="deepseek-period-year-missing"]').exists()).toBe(false)
  })

  it('切换时间缺失时不显示切换行', async () => {
    const wrapper = await mountPanel({
      deepseek_current_period: { state: 'valley', label: '谷价时段' }
    })
    expect(wrapper.find('[data-testid="deepseek-period-next-switch"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="deepseek-period"]').exists()).toBe(true)
  })
})
