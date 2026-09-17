import { cn } from '@/lib/utils'

export function LogoMark({ className, title }: { className?: string; title?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      role={title ? 'img' : 'presentation'}
      aria-label={title}
      aria-hidden={title ? undefined : true}
      className={cn('size-8', className)}
    >
      <rect x="1" y="1" width="22" height="22" rx="5.2" fill="var(--logo-body)" />
      <g fill="var(--logo-hinge)">
        <rect x="2.5" y="4.8" width="3" height="5" rx="1.5" />
        <rect x="2.5" y="14.2" width="3" height="5" rx="1.5" />
      </g>
      <circle cx="13.2" cy="12" r="7" fill="var(--logo-door)" />
      <g fill="var(--logo-handle)">
        <rect
          x="8.5"
          y="11.1"
          width="9.4"
          height="1.8"
          rx="0.9"
          transform="rotate(45 13.2 12)"
        />
        <rect
          x="8.5"
          y="11.1"
          width="9.4"
          height="1.8"
          rx="0.9"
          transform="rotate(-45 13.2 12)"
        />
        <circle cx="13.2" cy="12" r="2.5" />
      </g>
      <circle cx="13.2" cy="12" r="0.9" fill="var(--logo-door)" />
    </svg>
  )
}

export function Logo({ className, compact = false }: { className?: string; compact?: boolean }) {
  return (
    <span className={cn('inline-flex items-center gap-2.5', className)}>
      <LogoMark title="Backvault" className={compact ? 'size-7' : 'size-8'} />
      {compact ? null : (
        <span className="flex flex-col leading-none">
          <span
            className="text-[17px] font-semibold text-text"
            style={{ letterSpacing: '-0.01em' }}
          >
            Backvault
          </span>
          <span className="mt-0.5 text-[10.5px] font-medium uppercase tracking-wider text-soft">
            Backup manager
          </span>
        </span>
      )}
    </span>
  )
}
