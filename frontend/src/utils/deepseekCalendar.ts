export type DeepSeekHolidayCalendar = Record<string, string[]>
export type DeepSeekCalendarError = 'json' | 'root' | 'year' | 'dates' | 'date' | 'yearMismatch' | 'duplicate'
export type DeepSeekCalendarParseResult = { ok: true; calendar: DeepSeekHolidayCalendar } | { ok: false; error: DeepSeekCalendarError; value?: string }

// Validate only the wire format. Never manufacture legal holidays, a current
// pricing period, or a calendar year from the browser's timezone.
export function validateDeepSeekCalendar(value: unknown): DeepSeekCalendarParseResult {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return { ok: false, error: 'root' }
  const calendar: DeepSeekHolidayCalendar = {}
  for (const [year, dates] of Object.entries(value)) {
    if (!/^[0-9]{4}$/.test(year) || Number(year) < 1) return { ok: false, error: 'year', value: year }
    if (!Array.isArray(dates)) return { ok: false, error: 'dates', value: year }
    const seen = new Set<string>()
    for (const date of dates) {
      if (typeof date !== 'string' || !/^[0-9]{4}-[0-9]{2}-[0-9]{2}$/.test(date)) return { ok: false, error: 'date', value: String(date) }
      if (date.slice(0, 4) !== year) return { ok: false, error: 'yearMismatch', value: date }
      const month = Number(date.slice(5, 7)), day = Number(date.slice(8, 10)), numericYear = Number(year)
      const leap = numericYear % 4 === 0 && (numericYear % 100 !== 0 || numericYear % 400 === 0)
      const daysInMonth = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]
      if (month < 1 || month > 12 || day < 1 || day > daysInMonth[month - 1]) return { ok: false, error: 'date', value: date }
      if (seen.has(date)) return { ok: false, error: 'duplicate', value: date }
      seen.add(date)
    }
    calendar[year] = [...dates]
  }
  return { ok: true, calendar }
}
export function parseDeepSeekCalendar(text: string): DeepSeekCalendarParseResult {
  try { return validateDeepSeekCalendar(JSON.parse(text)) } catch { return { ok: false, error: 'json' } }
}
export function serializeDeepSeekCalendar(calendar: DeepSeekHolidayCalendar): string {
  return JSON.stringify(calendar, null, 2)
}
