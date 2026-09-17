import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

export function EmptyState({
  icon,
  title,
  description,
  action,
  className,
}: {
  icon?: ReactNode
  title: string
  description?: ReactNode
  action?: ReactNode
  className?: string
}) {
  return (
    <div
      className={cn(
        'flex flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-border-strong px-6 py-12 text-center',
        className,
      )}
    >
      {icon ? <div className="text-soft">{icon}</div> : null}
      <div className="space-y-1">
        <h3 className="text-sm font-semibold text-text">{title}</h3>
        {description ? <p className="mx-auto max-w-md text-sm text-muted">{description}</p> : null}
      </div>
      {action}
    </div>
  )
}
