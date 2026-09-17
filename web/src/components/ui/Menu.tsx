import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Ellipsis } from 'lucide-react'
import { cn } from '@/lib/utils'

export interface MenuAction {
  id: string
  label: string
  icon?: ReactNode
  onSelect: () => void
  tone?: 'default' | 'danger'
  disabled?: boolean
}

export function ActionMenu({
  actions,
  label = 'Actions',
  trigger,
  align = 'right',
}: {
  actions: MenuAction[]
  label?: string
  trigger?: ReactNode
  align?: 'left' | 'right'
}) {
  const [open, setOpen] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onPointerDown = (event: MouseEvent) => {
      if (!containerRef.current?.contains(event.target as Node)) setOpen(false)
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [open])

  return (
    <div ref={containerRef} className="relative inline-flex">
      <button
        type="button"
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className={cn(
          'inline-flex items-center justify-center rounded-lg border border-transparent text-muted transition-colors hover:bg-surface-3 hover:text-text',
          trigger ? 'gap-2 px-2 py-1.5' : 'size-8',
          open && 'bg-surface-3 text-text',
        )}
      >
        {trigger ?? <Ellipsis className="size-4" />}
      </button>
      {open ? (
        <div
          role="menu"
          className={cn(
            'anim-in absolute top-full z-40 mt-1 min-w-48 overflow-hidden rounded-lg border border-border bg-surface py-1 shadow-pop',
            align === 'right' ? 'right-0' : 'left-0',
          )}
        >
          {actions.map((action) => (
            <button
              key={action.id}
              role="menuitem"
              type="button"
              disabled={action.disabled}
              onClick={() => {
                setOpen(false)
                action.onSelect()
              }}
              className={cn(
                'flex w-full items-center gap-2.5 px-3 py-1.5 text-left text-[13px] transition-colors disabled:cursor-not-allowed disabled:opacity-40',
                action.tone === 'danger'
                  ? 'text-danger hover:bg-danger/10'
                  : 'text-text hover:bg-surface-3',
              )}
            >
              <span className="text-soft">{action.icon}</span>
              {action.label}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  )
}
