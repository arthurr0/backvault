import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

export interface CardProps {
  title?: ReactNode
  description?: ReactNode
  actions?: ReactNode
  children?: ReactNode
  className?: string
  bodyClassName?: string
  footer?: ReactNode
}

export function Card({
  title,
  description,
  actions,
  children,
  className,
  bodyClassName,
  footer,
}: CardProps) {
  return (
    <section className={cn('card flex flex-col', className)}>
      {title || actions ? (
        <header className="flex items-start justify-between gap-3 border-b border-border px-4 py-3">
          <div className="min-w-0">
            <h2 className="truncate text-sm font-semibold text-text">{title}</h2>
            {description ? <p className="mt-0.5 text-xs text-muted">{description}</p> : null}
          </div>
          {actions ? <div className="flex shrink-0 items-center gap-2">{actions}</div> : null}
        </header>
      ) : null}
      <div className={cn('flex-1 p-4', bodyClassName)}>{children}</div>
      {footer ? <footer className="border-t border-border px-4 py-3">{footer}</footer> : null}
    </section>
  )
}

export function StatTile({
  label,
  value,
  sub,
  tone = 'neutral',
  icon,
}: {
  label: string
  value: ReactNode
  sub?: ReactNode
  tone?: 'neutral' | 'success' | 'warning' | 'danger' | 'running' | 'accent'
  icon?: ReactNode
}) {
  const toneClass: Record<string, string> = {
    neutral: 'text-text',
    success: 'text-success',
    warning: 'text-warning',
    danger: 'text-danger',
    running: 'text-running',
    accent: 'text-accent',
  }
  return (
    <div className="card flex flex-col gap-1 p-4">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-medium uppercase tracking-wide text-muted">{label}</span>
        {icon ? <span className="text-soft">{icon}</span> : null}
      </div>
      <span className={cn('font-mono text-2xl font-medium tabular-nums', toneClass[tone])}>{value}</span>
      {sub ? <span className="text-xs text-muted">{sub}</span> : null}
    </div>
  )
}
