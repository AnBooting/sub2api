import { describe, expect, it } from 'vitest'
import {
  DEFAULT_DEEPSEEK_TIMEZONE,
  formatPeriodTime,
  readDeepSeekPeriod,
  resolvePeriodLabel
} from '../deepseekPeriod'

const t = (key: string) => key

describe('readDeepSeekPeriod', () => {
  it('后端没给这个字段时返回 null（界面不显示这一块）', () => {
    expect(readDeepSeekPeriod({})).toBeNull()
    expect(readDeepSeekPeriod(null)).toBeNull()
    expect(readDeepSeekPeriod(undefined)).toBeNull()
    expect(readDeepSeekPeriod({ deepseek_current_period: undefined })).toBeNull()
  })

  it('字段存在但没有任何可显示内容时返回 null', () => {
    expect(readDeepSeekPeriod({ deepseek_current_period: {} })).toBeNull()
  })

  it('读取后端给的档位、倍率与下一档切换时间', () => {
    const period = readDeepSeekPeriod({
      deepseek_current_period: {
        state: 'peak',
        label: '高峰时段',
        timezone: 'Asia/Shanghai',
        window: '09:00-12:00',
        multiplier: 2,
        next_switch_at: '2026-10-21T11:00:00+08:00'
      }
    })
    expect(period).not.toBeNull()
    expect(period?.state).toBe('peak')
    expect(period?.label).toBe('高峰时段')
    expect(period?.multiplier).toBe(2)
    expect(period?.nextSwitchAt).toBe('2026-10-21T11:00:00+08:00')
    // 切换时间按官方时区显示墙上时间
    expect(period?.nextSwitchText).toBe('2026-10-21 11:00')
    expect(period?.timezone).toBe('Asia/Shanghai')
  })

  it('缺 timezone 时用官方时区兜底', () => {
    const period = readDeepSeekPeriod({
      deepseek_current_period: { state: 'valley', next_switch_at: '2026-10-21T09:00:00+08:00' }
    })
    expect(period?.timezone).toBe(DEFAULT_DEEPSEEK_TIMEZONE)
    expect(period?.nextSwitchText).toBe('2026-10-21 09:00')
  })

  it('切换时间不可解析时不显示时间，但档位仍然显示', () => {
    const period = readDeepSeekPeriod({
      deepseek_current_period: { state: 'valley', next_switch_at: 'not-a-date' }
    })
    expect(period?.state).toBe('valley')
    expect(period?.nextSwitchText).toBeNull()
  })

  it('带上当年假日日历缺失标记', () => {
    expect(
      readDeepSeekPeriod({ deepseek_current_period: { state: 'peak' }, deepseek_calendar_year_missing: true })
        ?.calendarYearMissing
    ).toBe(true)
    expect(
      readDeepSeekPeriod({ deepseek_current_period: { state: 'peak' } })?.calendarYearMissing
    ).toBe(false)
  })
})

describe('resolvePeriodLabel', () => {
  it('优先用服务端给的权威文案', () => {
    const period = readDeepSeekPeriod({
      deepseek_current_period: { state: 'peak', label: '官方高峰' }
    })
    expect(resolvePeriodLabel(period!, t)).toBe('官方高峰')
  })

  it('没有 label 时按 state 兜底', () => {
    const peak = readDeepSeekPeriod({ deepseek_current_period: { state: 'peak' } })
    expect(resolvePeriodLabel(peak!, t)).toBe('admin.settings.pricing.deepseekPeriod.peak')
    const valley = readDeepSeekPeriod({ deepseek_current_period: { state: 'valley' } })
    expect(resolvePeriodLabel(valley!, t)).toBe('admin.settings.pricing.deepseekPeriod.valley')
  })

  it('既没有 label 也没有已知 state 时返回空串', () => {
    const unknown = readDeepSeekPeriod({ deepseek_current_period: { state: 'weird' } })
    expect(resolvePeriodLabel(unknown!, t)).toBe('')
  })
})

describe('formatPeriodTime', () => {
  it('按指定时区格式化', () => {
    expect(formatPeriodTime('2026-10-21T03:00:00Z', 'Asia/Shanghai')).toBe('2026-10-21 11:00')
    expect(formatPeriodTime('2026-10-21T03:00:00Z', 'UTC')).toBe('2026-10-21 03:00')
  })

  it('时间非法或时区非法时返回空串', () => {
    expect(formatPeriodTime('nope', 'Asia/Shanghai')).toBe('')
    expect(formatPeriodTime('2026-10-21T03:00:00Z', 'Not/AZone')).toBe('')
  })
})
