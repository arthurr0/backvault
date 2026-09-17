import { useId, type ReactNode } from 'react'
import { cn } from '@/lib/utils'

export interface FieldShellProps {
  label: string
  htmlFor?: string
  required?: boolean
  help?: ReactNode
  error?: string
  className?: string
  children: ReactNode
  hint?: ReactNode
}

export function FieldShell({
  label,
  htmlFor,
  required,
  help,
  error,
  className,
  children,
  hint,
}: FieldShellProps) {
  return (
    <div className={cn('flex flex-col gap-1.5', className)}>
      <div className="flex items-baseline justify-between gap-3">
        <label htmlFor={htmlFor} className="text-[13px] font-medium text-text">
          {label}
          {required ? (
            <span className="ml-1 text-danger" aria-hidden="true">
              *
            </span>
          ) : null}
        </label>
        {hint ? <span className="text-xs text-soft">{hint}</span> : null}
      </div>
      {children}
      {error ? (
        <p className="text-xs text-danger">{error}</p>
      ) : help ? (
        <p className="text-xs text-muted">{help}</p>
      ) : null}
    </div>
  )
}

export function useFieldId(provided?: string): string {
  const generated = useId()
  return provided ?? generated
}
