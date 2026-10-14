import { describe, expect, it } from 'vitest'
import { parseDeepSeekCalendar, serializeDeepSeekCalendar, validateDeepSeekCalendar } from '../deepseekCalendar'
describe('DeepSeek calendar wire validation (format only, no legal dates generated)', () => {
  it('keeps missing years distinct from explicitly maintained empty arrays and explicit clear', () => {
    expect(parseDeepSeekCalendar('{}')).toEqual({ ok: true, calendar: {} })
    expect(parseDeepSeekCalendar('{"2026":[]}')).toEqual({ ok: true, calendar: { '2026': [] } })
    expect(validateDeepSeekCalendar({ '0001': ['0001-01-01'], '9999': ['9999-12-31'], '2000': ['2000-02-29'] }).ok).toBe(true)
  })
  it.each([
    ['{', 'json'], ['null', 'root'], ['[]', 'root'], ['42', 'root'],
    ['{"2026":null}', 'dates'], ['{"2026":{}}', 'dates'],
    ['{"0000":[]}', 'year'], ['{"26":[]}', 'year'], ['{"02026":[]}', 'year'], ['{"10000":[]}', 'year'], ['{"-001":[]}', 'year'],
    ['{"2026":["2027-01-01"]}', 'yearMismatch'],
    ['{"2026":["2026-02-29"]}', 'date'], ['{"1900":["1900-02-29"]}', 'date'],
    ['{"2026":["2026-13-01"]}', 'date'], ['{"2026":["2026-01-00"]}', 'date'], ['{"2026":["2026-04-31"]}', 'date'],
    ['{"2026":["2026-1-01"]}', 'date'], ['{"2026":[42]}', 'date'],
    ['{"2026":["2026-10-12","2026-10-12"]}', 'duplicate'],
  ])('rejects invalid draft %s without converting it to an empty year/map', (text, error) => { expect(parseDeepSeekCalendar(text)).toMatchObject({ ok: false, error }) })
  it('serializes only the supplied map without filling years, sorting dates, or inventing holidays', () => {
    const calendar = { '2026': ['2026-10-12', '2026-10-11'], '2028': [] }
    expect(JSON.parse(serializeDeepSeekCalendar(calendar))).toEqual(calendar)
    expect(calendar).not.toHaveProperty('2027')
  })
})
