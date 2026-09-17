export interface CronParts {
  minutes: number[]
  hours: number[]
  daysOfMonth: number[]
  months: number[]
  daysOfWeek: number[]
  domRestricted: boolean
  dowRestricted: boolean
}

export interface CronParseResult {
  ok: boolean
  error?: string
  parts?: CronParts
  normalized?: string
}

const ALIASES: Record<string, string> = {
  '@yearly': '0 0 1 1 *',
  '@annually': '0 0 1 1 *',
  '@monthly': '0 0 1 * *',
  '@weekly': '0 0 * * 0',
  '@daily': '0 0 * * *',
  '@midnight': '0 0 * * *',
  '@hourly': '0 * * * *',
}

const MONTH_NAMES = [
  'January',
  'February',
  'March',
  'April',
  'May',
  'June',
  'July',
  'August',
  'September',
  'October',
  'November',
  'December',
]

const DAY_NAMES = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']

const MONTH_TOKENS: Record<string, number> = {
  jan: 1, feb: 2, mar: 3, apr: 4, may: 5, jun: 6,
  jul: 7, aug: 8, sep: 9, oct: 10, nov: 11, dec: 12,
}

const DAY_TOKENS: Record<string, number> = {
  sun: 0, mon: 1, tue: 2, wed: 3, thu: 4, fri: 5, sat: 6,
}

function parseField(
  raw: string,
  min: number,
  max: number,
  tokens?: Record<string, number>,
): number[] | string {
  const values = new Set<number>()
  for (const chunk of raw.split(',')) {
    const part = chunk.trim()
    if (!part) return `empty value in "${raw}"`
    let step = 1
    let range = part
    const slash = part.indexOf('/')
    if (slash !== -1) {
      range = part.slice(0, slash)
      const stepText = part.slice(slash + 1)
      step = Number(stepText)
      if (!Number.isInteger(step) || step <= 0) return `invalid step "${stepText}"`
    }
    let start: number
    let end: number
    if (range === '*') {
      start = min
      end = max
    } else if (range.includes('-')) {
      const [a, b] = range.split('-')
      const parsedA = token(a, tokens)
      const parsedB = token(b, tokens)
      if (parsedA === null || parsedB === null) return `invalid range "${range}"`
      start = parsedA
      end = parsedB
    } else {
      const single = token(range, tokens)
      if (single === null) return `invalid value "${range}"`
      start = single
      end = slash === -1 ? single : max
    }
    if (start < min || end > max || start > end) return `value out of range in "${part}"`
    for (let v = start; v <= end; v += step) values.add(v)
  }
  if (!values.size) return `no values in "${raw}"`
  return [...values].sort((a, b) => a - b)
}

function token(raw: string, tokens?: Record<string, number>): number | null {
  const text = raw.trim().toLowerCase()
  if (tokens && text in tokens) return tokens[text]
  const n = Number(text)
  return Number.isInteger(n) ? n : null
}

export function parseCron(expression: string): CronParseResult {
  const trimmed = expression.trim()
  if (!trimmed) return { ok: false, error: 'Schedule is empty' }
  const expanded = ALIASES[trimmed.toLowerCase()] ?? trimmed
  if (expanded.startsWith('@')) return { ok: false, error: `Unknown alias "${trimmed}"` }
  const fields = expanded.split(/\s+/)
  if (fields.length !== 5) {
    return { ok: false, error: 'A cron schedule needs 5 fields: minute hour day-of-month month day-of-week' }
  }
  const [minRaw, hourRaw, domRaw, monthRaw, dowRaw] = fields
  const minutes = parseField(minRaw, 0, 59)
  if (typeof minutes === 'string') return { ok: false, error: `Minute: ${minutes}` }
  const hours = parseField(hourRaw, 0, 23)
  if (typeof hours === 'string') return { ok: false, error: `Hour: ${hours}` }
  const daysOfMonth = parseField(domRaw, 1, 31)
  if (typeof daysOfMonth === 'string') return { ok: false, error: `Day of month: ${daysOfMonth}` }
  const months = parseField(monthRaw, 1, 12, MONTH_TOKENS)
  if (typeof months === 'string') return { ok: false, error: `Month: ${months}` }
  const dowParsed = parseField(dowRaw.replace(/7/g, '0'), 0, 6, DAY_TOKENS)
  if (typeof dowParsed === 'string') return { ok: false, error: `Day of week: ${dowParsed}` }

  return {
    ok: true,
    normalized: expanded,
    parts: {
      minutes,
      hours,
      daysOfMonth,
      months,
      daysOfWeek: dowParsed,
      domRestricted: domRaw !== '*',
      dowRestricted: dowRaw !== '*',
    },
  }
}

function zoneOffset(date: Date, timeZone: string): number {
  const formatter = new Intl.DateTimeFormat('en-US', {
    timeZone,
    hour12: false,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
  const parts = formatter.formatToParts(date)
  const map: Record<string, number> = {}
  for (const part of parts) {
    if (part.type !== 'literal') map[part.type] = Number(part.value)
  }
  const asUTC = Date.UTC(map.year, map.month - 1, map.day, map.hour % 24, map.minute, map.second)
  return asUTC - date.getTime()
}

function zonedPartsNow(date: Date, timeZone: string) {
  const offset = zoneOffset(date, timeZone)
  const shifted = new Date(date.getTime() + offset)
  return {
    year: shifted.getUTCFullYear(),
    month: shifted.getUTCMonth() + 1,
    day: shifted.getUTCDate(),
    hour: shifted.getUTCHours(),
    minute: shifted.getUTCMinutes(),
  }
}

function fromZoned(
  year: number,
  month: number,
  day: number,
  hour: number,
  minute: number,
  timeZone: string,
): Date {
  const guess = Date.UTC(year, month - 1, day, hour, minute, 0)
  const first = zoneOffset(new Date(guess), timeZone)
  let ts = guess - first
  const second = zoneOffset(new Date(ts), timeZone)
  if (second !== first) ts = guess - second
  return new Date(ts)
}

export function isValidTimeZone(timeZone: string): boolean {
  try {
    new Intl.DateTimeFormat('en-US', { timeZone })
    return true
  } catch {
    return false
  }
}

export function nextOccurrences(
  expression: string,
  timeZone: string,
  count = 5,
  from: Date = new Date(),
): Date[] {
  const parsed = parseCron(expression)
  if (!parsed.ok || !parsed.parts) return []
  const zone = isValidTimeZone(timeZone) ? timeZone : 'UTC'
  const parts = parsed.parts
  const out: Date[] = []
  const start = zonedPartsNow(from, zone)
  const baseDay = Date.UTC(start.year, start.month - 1, start.day)

  for (let dayOffset = 0; dayOffset < 1500 && out.length < count; dayOffset += 1) {
    const cursor = new Date(baseDay + dayOffset * 86_400_000)
    const year = cursor.getUTCFullYear()
    const month = cursor.getUTCMonth() + 1
    const day = cursor.getUTCDate()
    const weekday = cursor.getUTCDay()
    if (!parts.months.includes(month)) continue
    const domMatch = parts.daysOfMonth.includes(day)
    const dowMatch = parts.daysOfWeek.includes(weekday)
    let dayMatches: boolean
    if (parts.domRestricted && parts.dowRestricted) dayMatches = domMatch || dowMatch
    else if (parts.domRestricted) dayMatches = domMatch
    else if (parts.dowRestricted) dayMatches = dowMatch
    else dayMatches = true
    if (!dayMatches) continue
    for (const hour of parts.hours) {
      for (const minute of parts.minutes) {
        const when = fromZoned(year, month, day, hour, minute, zone)
        if (when.getTime() <= from.getTime()) continue
        out.push(when)
        if (out.length >= count) break
      }
      if (out.length >= count) break
    }
  }
  return out
}

function joinText(rendered: string[], joiner = 'and'): string {
  if (rendered.length === 1) return rendered[0]
  if (rendered.length === 2) return `${rendered[0]} ${joiner} ${rendered[1]}`
  return `${rendered.slice(0, -1).join(', ')} ${joiner} ${rendered[rendered.length - 1]}`
}

function listToText(values: number[], render: (v: number) => string, joiner = 'and'): string {
  return joinText(values.map(render), joiner)
}

function pad(value: number): string {
  return String(value).padStart(2, '0')
}

function isEveryStep(values: number[], min: number, max: number): number | null {
  if (values.length < 2) return null
  const step = values[1] - values[0]
  if (values[0] !== min) return null
  for (let i = 1; i < values.length; i += 1) {
    if (values[i] - values[i - 1] !== step) return null
  }
  if (values[values.length - 1] + step <= max) return null
  return step
}

function ordinal(n: number): string {
  const suffix = n % 100 >= 11 && n % 100 <= 13 ? 'th' : ['th', 'st', 'nd', 'rd'][n % 10] ?? 'th'
  return `${n}${suffix}`
}

export function describeCron(expression: string): string {
  const trimmed = expression.trim()
  if (!trimmed) return 'Manual only'
  const parsed = parseCron(trimmed)
  if (!parsed.ok || !parsed.parts) return 'Invalid schedule'
  const p = parsed.parts
  const everyMinute = p.minutes.length === 60
  const everyHour = p.hours.length === 24

  let timePart: string
  if (everyMinute && everyHour) {
    timePart = 'Every minute'
  } else if (everyHour) {
    const step = isEveryStep(p.minutes, 0, 59)
    timePart =
      step !== null && step > 1
        ? `Every ${step} minutes`
        : p.minutes.length === 1
          ? `Every hour at minute ${p.minutes[0]}`
          : `Every hour at minutes ${listToText(p.minutes, String)}`
  } else if (everyMinute) {
    timePart = `Every minute during ${listToText(p.hours, (h) => `${pad(h)}:00`)}`
  } else {
    const hourStep = isEveryStep(p.hours, 0, 23)
    if (hourStep !== null && hourStep > 1 && p.minutes.length === 1) {
      timePart = `Every ${hourStep} hours at minute ${p.minutes[0]}`
    } else {
      const times: string[] = []
      for (const h of p.hours) {
        for (const m of p.minutes) times.push(`${pad(h)}:${pad(m)}`)
      }
      timePart = times.length <= 4 ? `At ${joinText(times)}` : `At ${times.length} times a day`
    }
  }

  const segments: string[] = [timePart]

  if (p.dowRestricted) {
    if (p.daysOfWeek.length === 7) {
      segments.push('every day')
    } else if (p.daysOfWeek.length === 5 && [1, 2, 3, 4, 5].every((d) => p.daysOfWeek.includes(d))) {
      segments.push('on weekdays')
    } else if (p.daysOfWeek.length === 2 && p.daysOfWeek.includes(0) && p.daysOfWeek.includes(6)) {
      segments.push('on weekends')
    } else {
      segments.push(`on ${listToText(p.daysOfWeek, (d) => DAY_NAMES[d], 'and')}`)
    }
  }

  if (p.domRestricted) {
    const step = isEveryStep(p.daysOfMonth, 1, 31)
    if (step !== null && step > 1) segments.push(`every ${step} days of the month`)
    else segments.push(`on the ${listToText(p.daysOfMonth, ordinal, 'and')}`)
  }

  if (p.months.length !== 12) {
    segments.push(`in ${listToText(p.months, (m) => MONTH_NAMES[m - 1], 'and')}`)
  }

  if (!p.dowRestricted && !p.domRestricted && p.months.length === 12 && !everyMinute && !everyHour) {
    segments.push('every day')
  }

  return segments.join(' ')
}

export interface CronPreset {
  id: string
  label: string
  expression: string
}

export const CRON_PRESETS: CronPreset[] = [
  { id: 'every-15', label: 'Every 15 minutes', expression: '*/15 * * * *' },
  { id: 'hourly', label: 'Hourly', expression: '0 * * * *' },
  { id: 'every-6h', label: 'Every 6 hours', expression: '0 */6 * * *' },
  { id: 'daily-0200', label: 'Daily at 02:00', expression: '0 2 * * *' },
  { id: 'daily-0330', label: 'Daily at 03:30', expression: '30 3 * * *' },
  { id: 'weekdays-0100', label: 'Weekdays at 01:00', expression: '0 1 * * 1-5' },
  { id: 'weekly-sun-0300', label: 'Weekly, Sunday 03:00', expression: '0 3 * * 0' },
  { id: 'monthly-1st-0400', label: 'Monthly, 1st at 04:00', expression: '0 4 1 * *' },
]
