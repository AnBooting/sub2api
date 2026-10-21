import { describe, expect, it } from 'vitest'
import { parseHhMm, peakWindowStatus, shanghaiMinutesOfDay } from '../peakWindow'

const config = {
  enabled: true,
  start: '09:00',
  end: '12:00',
  multiplier: 2
}

describe('parseHhMm', () => {
  it('解析合法时间', () => {
    expect(parseHhMm('09:00')).toBe(540)
    expect(parseHhMm('23:59')).toBe(1439)
    expect(parseHhMm('0:05')).toBe(5)
  })

  it('非法格式返回 null', () => {
    expect(parseHhMm('')).toBeNull()
    expect(parseHhMm('9')).toBeNull()
    expect(parseHhMm('24:00')).toBeNull()
    expect(parseHhMm('12:60')).toBeNull()
    expect(parseHhMm('abc')).toBeNull()
  })
})

describe('shanghaiMinutesOfDay', () => {
  it('按 Asia/Shanghai 计算墙上时间', () => {
    expect(shanghaiMinutesOfDay(new Date('2026-10-21T02:00:00Z'))).toBe(600)
    expect(shanghaiMinutesOfDay(new Date('2026-10-21T16:35:00Z'))).toBe(35) // 次日 00:35
  })
})

describe('peakWindowStatus', () => {
  it('未启用时是 disabled', () => {
    const status = peakWindowStatus({ ...config, enabled: false })
    expect(status.state).toBe('disabled')
    expect(status.multiplier).toBe(1)
    expect(status.nextSwitchAt).toBeNull()
  })

  it('高峰时段内返回 peak 与高峰倍率', () => {
    const status = peakWindowStatus(config, new Date('2026-10-21T02:00:00Z'))
    expect(status.state).toBe('peak')
    expect(status.multiplier).toBe(2)
    expect(status.nextSwitchAt).toBe('12:00')
    expect(status.nextSwitchDay).toBe('today')
    expect(status.nextState).toBe('valley')
  })

  it('时段起点含、终点不含', () => {
    expect(peakWindowStatus(config, new Date('2026-10-21T01:00:00Z')).state).toBe('peak')
    expect(peakWindowStatus(config, new Date('2026-10-21T04:00:00Z')).state).toBe('valley')
  })

  it('谷时段在起点之前时今天切换', () => {
    const status = peakWindowStatus(config, new Date('2026-10-21T00:30:00Z'))
    expect(status.state).toBe('valley')
    expect(status.multiplier).toBe(1)
    expect(status.nextSwitchAt).toBe('09:00')
    expect(status.nextSwitchDay).toBe('today')
  })

  it('谷时段在终点之后时顺延到明天', () => {
    const status = peakWindowStatus(config, new Date('2026-10-21T06:00:00Z'))
    expect(status.state).toBe('valley')
    expect(status.nextSwitchAt).toBe('09:00')
    expect(status.nextSwitchDay).toBe('tomorrow')
  })

  it('end 不晚于 start 时判定为无效配置', () => {
    expect(peakWindowStatus({ ...config, start: '12:00', end: '09:00' }).state).toBe('invalid')
    expect(peakWindowStatus({ ...config, start: '09:00', end: '09:00' }).state).toBe('invalid')
    expect(peakWindowStatus({ ...config, end: '' }).state).toBe('invalid')
  })
})
