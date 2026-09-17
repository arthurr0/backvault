import type { ReactNode } from 'react'

export interface TooltipRow {
  label: string
  value: string
  color?: string
}

export function ChartTooltip({ title, rows }: { title: string; rows: TooltipRow[] }) {
  return (
    <div className="rounded-lg border border-border bg-surface px-3 py-2 shadow-pop">
      <p className="mb-1 text-xs font-medium text-text">{title}</p>
      <ul className="space-y-0.5">
        {rows.map((row) => (
          <li key={row.label} className="flex items-center gap-2 text-xs text-muted">
            {row.color ? (
              <span className="size-2 rounded-sm" style={{ background: row.color }} aria-hidden="true" />
            ) : null}
            <span>{row.label}</span>
            <span className="ml-auto font-mono text-text">{row.value}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}

export function ChartFrame({ children, height = 260 }: { children: ReactNode; height?: number }) {
  return (
    <div style={{ height }} className="w-full">
      {children}
    </div>
  )
}
