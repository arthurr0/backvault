import { useMemo } from 'react'
import type { DriverSpec } from '@/api/types'
import { DriverIcon } from '@/lib/icons'
import { cn } from '@/lib/utils'

export function DriverPicker({
  specs,
  value,
  onChange,
  disabled,
}: {
  specs: DriverSpec[]
  value: string
  onChange: (kind: string) => void
  disabled?: boolean
}) {
  const groups = useMemo(() => {
    const map = new Map<string, DriverSpec[]>()
    for (const spec of specs) {
      const category = spec.category?.trim() || 'Other'
      const list = map.get(category) ?? []
      list.push(spec)
      map.set(category, list)
    }
    return [...map.entries()].sort((a, b) => a[0].localeCompare(b[0]))
  }, [specs])

  return (
    <div className="flex flex-col gap-5" role="radiogroup" aria-label="Driver">
      {groups.map(([category, items]) => (
        <div key={category}>
          <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-soft">{category}</h3>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4">
            {items.map((spec) => {
              const active = spec.kind === value
              return (
                <button
                  key={spec.kind}
                  type="button"
                  role="radio"
                  aria-checked={active}
                  disabled={disabled}
                  onClick={() => onChange(spec.kind)}
                  title={spec.description}
                  className={cn(
                    'group flex items-start gap-2.5 rounded-xl border p-3 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-50',
                    active
                      ? 'border-accent bg-accent-soft'
                      : 'border-border bg-surface hover:border-border-strong hover:bg-surface-2',
                  )}
                >
                  <DriverIcon
                    icon={spec.icon}
                    kind={spec.kind}
                    className={cn('mt-0.5 size-5 shrink-0', active ? 'text-accent' : 'text-muted')}
                  />
                  <span className="min-w-0">
                    <span className="block text-[13px] font-medium text-text">{spec.label}</span>
                    <span className="mt-0.5 line-clamp-2 block text-[11.5px] leading-snug text-muted">
                      {spec.description}
                    </span>
                  </span>
                </button>
              )
            })}
          </div>
        </div>
      ))}
    </div>
  )
}

export function DriverBadge({ spec, kind }: { spec?: DriverSpec; kind: string }) {
  return (
    <span className="inline-flex items-center gap-1.5 text-sm text-text">
      <DriverIcon icon={spec?.icon} kind={kind} className="size-4 text-muted" />
      {spec?.label ?? kind}
    </span>
  )
}
