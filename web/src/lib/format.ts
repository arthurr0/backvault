const UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']

export function formatBytes(bytes: number | undefined | null, digits = 1): string {
  if (bytes === undefined || bytes === null || Number.isNaN(bytes)) return '-'
  if (bytes === 0) return '0 B'
  const negative = bytes < 0
  let value = Math.abs(bytes)
  let unit = 0
  while (value >= 1024 && unit < UNITS.length - 1) {
    value /= 1024
    unit += 1
  }
  const text = unit === 0 ? String(Math.round(value)) : value.toFixed(value >= 100 ? 0 : digits)
  return `${negative ? '-' : ''}${text} ${UNITS[unit]}`
}

export function formatDuration(ms: number | undefined | null): string {
  if (ms === undefined || ms === null || Number.isNaN(ms)) return '-'
  if (ms < 1000) return `${Math.max(0, Math.round(ms))} ms`
  const totalSeconds = Math.round(ms / 1000)
  const seconds = totalSeconds % 60
  const minutes = Math.floor(totalSeconds / 60) % 60
  const hours = Math.floor(totalSeconds / 3600) % 24
  const days = Math.floor(totalSeconds / 86400)
  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${minutes}m`
  if (minutes > 0) return `${minutes}m ${seconds}s`
  return `${totalSeconds}s`
}

export function parseDate(value: string | undefined | null): Date | null {
  if (!value) return null
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? null : date
}

export function toDayInput(value: string | undefined | null): string {
  const date = parseDate(value)
  return date ? date.toISOString().slice(0, 10) : ''
}

export function dayStartISO(day: string): string {
  const date = parseDate(`${day}T00:00:00.000Z`)
  return date ? date.toISOString() : ''
}

export function dayEndISO(day: string): string {
  const date = parseDate(`${day}T23:59:59.999Z`)
  return date ? date.toISOString() : ''
}

const RELATIVE_STEPS: Array<[Intl.RelativeTimeFormatUnit, number]> = [
  ['second', 60],
  ['minute', 60],
  ['hour', 24],
  ['day', 7],
  ['week', 4.35],
  ['month', 12],
  ['year', Number.POSITIVE_INFINITY],
]

const relative = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' })

export function formatRelative(value: string | Date | undefined | null): string {
  const date = value instanceof Date ? value : parseDate(value)
  if (!date) return '-'
  let delta = (date.getTime() - Date.now()) / 1000
  for (const [unit, span] of RELATIVE_STEPS) {
    if (Math.abs(delta) < span || span === Number.POSITIVE_INFINITY) {
      return relative.format(Math.round(delta), unit)
    }
    delta /= span
  }
  return relative.format(Math.round(delta), 'year')
}

const absoluteFormat = new Intl.DateTimeFormat(undefined, {
  dateStyle: 'medium',
  timeStyle: 'medium',
})

export function formatAbsolute(value: string | Date | undefined | null): string {
  const date = value instanceof Date ? value : parseDate(value)
  if (!date) return '-'
  return absoluteFormat.format(date)
}

export function localTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  } catch {
    return 'UTC'
  }
}

const shortFormat = new Intl.DateTimeFormat(undefined, {
  month: 'short',
  day: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
})

export function formatShort(value: string | Date | undefined | null): string {
  const date = value instanceof Date ? value : parseDate(value)
  if (!date) return '-'
  return shortFormat.format(date)
}

export function formatDay(value: string | undefined | null): string {
  if (!value) return '-'
  const date = new Date(`${value}T00:00:00`)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric' }).format(date)
}

export function formatNumber(value: number | undefined | null): string {
  if (value === undefined || value === null || Number.isNaN(value)) return '-'
  return new Intl.NumberFormat().format(value)
}

export function plural(count: number, singular: string, pluralForm?: string): string {
  return count === 1 ? singular : (pluralForm ?? `${singular}s`)
}

export function truncateMiddle(value: string, max = 28): string {
  if (value.length <= max) return value
  const head = Math.ceil((max - 1) / 2)
  const tail = Math.floor((max - 1) / 2)
  return `${value.slice(0, head)}…${value.slice(value.length - tail)}`
}

export function slugify(value: string): string {
  return value
    .toLowerCase()
    .normalize('NFKD')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 60)
}

const ACRONYMS = new Set(['api', 'cli', 'sql', 'ssh', 'sftp', 's3', 'url', 'id', 'ui'])

export function titleCase(value: string): string {
  return value
    .replace(/[-_:]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
    .split(' ')
    .map((word) =>
      ACRONYMS.has(word.toLowerCase())
        ? word.toUpperCase()
        : word.charAt(0).toUpperCase() + word.slice(1),
    )
    .join(' ')
}

export function hostAddress(host: { user?: string; address: string; port?: number }): string {
  const port = host.port && host.port !== 22 ? `:${host.port}` : ''
  return `${host.user || 'root'}@${host.address}${port}`
}
