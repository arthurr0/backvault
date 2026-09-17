import { useMemo } from 'react'
import { CalendarClock, CircleCheck, CircleX } from 'lucide-react'
import { CRON_PRESETS, describeCron, nextOccurrences, parseCron } from '@/lib/cron'
import { formatAbsolute, formatRelative, localTimeZone } from '@/lib/format'
import { cn } from '@/lib/utils'
import { FieldShell } from '@/components/ui/Field'
import { Input, Select } from '@/components/ui/Input'

export function CronBuilder({
  value,
  onChange,
  timezone,
  onTimezoneChange,
  timezones,
  error,
}: {
  value: string
  onChange: (value: string) => void
  timezone: string
  onTimezoneChange: (value: string) => void
  timezones: string[]
  error?: string
}) {
  const parsed = useMemo(() => parseCron(value), [value])
  const upcoming = useMemo(
    () => (value.trim() ? nextOccurrences(value, timezone || 'UTC', 5) : []),
    [value, timezone],
  )
  const zoneOptions = useMemo(() => {
    const list = timezones.length ? timezones : ['UTC']
    return timezone && !list.includes(timezone) ? [timezone, ...list] : list
  }, [timezones, timezone])

  return (
    <div className="space-y-4">
      <div>
        <p className="mb-2 text-[13px] font-medium text-text">Presets</p>
        <div className="flex flex-wrap gap-1.5">
          <button
            type="button"
            onClick={() => onChange('')}
            className={cn(
              'rounded-lg border px-2.5 py-1.5 text-[12.5px] transition-colors',
              value.trim() === ''
                ? 'border-accent bg-accent-soft text-text'
                : 'border-border bg-surface text-muted hover:border-border-strong hover:text-text',
            )}
          >
            Manual only
          </button>
          {CRON_PRESETS.map((preset) => (
            <button
              key={preset.id}
              type="button"
              onClick={() => onChange(preset.expression)}
              className={cn(
                'rounded-lg border px-2.5 py-1.5 text-[12.5px] transition-colors',
                value.trim() === preset.expression
                  ? 'border-accent bg-accent-soft text-text'
                  : 'border-border bg-surface text-muted hover:border-border-strong hover:text-text',
              )}
            >
              {preset.label}
            </button>
          ))}
        </div>
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <FieldShell
          label="Cron expression"
          htmlFor="job-cron"
          help="Five fields: minute hour day-of-month month day-of-week. Leave empty for manual runs only."
          error={error ?? (value.trim() && !parsed.ok ? parsed.error : undefined)}
        >
          <Input
            id="job-cron"
            className="font-mono"
            spellCheck={false}
            placeholder="0 2 * * *"
            value={value}
            onChange={(event) => onChange(event.target.value)}
          />
        </FieldShell>

        <FieldShell label="Timezone" htmlFor="job-timezone" help="Schedules are evaluated in this zone.">
          <Select
            id="job-timezone"
            value={timezone}
            onChange={(event) => onTimezoneChange(event.target.value)}
          >
            {zoneOptions.map((zone) => (
              <option key={zone} value={zone}>
                {zone}
              </option>
            ))}
          </Select>
        </FieldShell>
      </div>

      <div className="rounded-xl border border-border bg-surface-2 p-3">
        <div className="flex items-center gap-2">
          {value.trim() === '' ? (
            <CalendarClock className="size-4 text-soft" aria-hidden="true" />
          ) : parsed.ok ? (
            <CircleCheck className="size-4 text-success" aria-hidden="true" />
          ) : (
            <CircleX className="size-4 text-danger" aria-hidden="true" />
          )}
          <p className="text-sm text-text">
            {value.trim() === '' ? 'Manual only, no schedule' : describeCron(value)}
          </p>
        </div>
        {upcoming.length ? (
          <>
            <p className="mt-3 text-xs font-medium uppercase tracking-wide text-soft">
              Next 5 occurrences, shown in {localTimeZone()}
            </p>
            <ul className="mt-1.5 space-y-1">
              {upcoming.map((date) => (
                <li key={date.toISOString()} className="flex justify-between gap-3 text-xs text-muted">
                  <span className="font-mono">{formatAbsolute(date)}</span>
                  <span className="text-soft">{formatRelative(date)}</span>
                </li>
              ))}
            </ul>
          </>
        ) : null}
      </div>
    </div>
  )
}
