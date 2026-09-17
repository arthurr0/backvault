import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
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

const hiddenStyle: CSSProperties = { position: 'fixed', top: 0, left: 0, visibility: 'hidden' }
const MENU_GAP = 4
const VIEWPORT_PADDING = 8

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
  const [style, setStyle] = useState<CSSProperties>(hiddenStyle)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onPointerDown = (event: MouseEvent) => {
      const target = event.target as Node
      if (buttonRef.current?.contains(target) || menuRef.current?.contains(target)) return
      setOpen(false)
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setOpen(false)
        buttonRef.current?.focus()
      }
    }
    const onScroll = (event: Event) => {
      if (menuRef.current?.contains(event.target as Node)) return
      setOpen(false)
    }
    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    window.addEventListener('scroll', onScroll, true)
    window.addEventListener('resize', onScroll)
    return () => {
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
      window.removeEventListener('scroll', onScroll, true)
      window.removeEventListener('resize', onScroll)
      setStyle(hiddenStyle)
    }
  }, [open])

  useLayoutEffect(() => {
    if (!open) return
    const button = buttonRef.current
    const menu = menuRef.current
    if (!button || !menu) return
    const anchor = button.getBoundingClientRect()
    const size = menu.getBoundingClientRect()
    const viewportWidth = window.innerWidth
    const viewportHeight = window.innerHeight
    let top = anchor.bottom + MENU_GAP
    if (top + size.height > viewportHeight - VIEWPORT_PADDING && anchor.top - MENU_GAP - size.height >= VIEWPORT_PADDING) {
      top = anchor.top - MENU_GAP - size.height
    }
    let left = align === 'right' ? anchor.right - size.width : anchor.left
    left = Math.min(Math.max(left, VIEWPORT_PADDING), viewportWidth - size.width - VIEWPORT_PADDING)
    setStyle({ position: 'fixed', top, left, visibility: 'visible' })
  }, [open, align, actions.length])

  useEffect(() => {
    if (!open) return
    const first = menuRef.current?.querySelector<HTMLButtonElement>('button:not(:disabled)')
    first?.focus()
  }, [open])

  const onMenuKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return
    const items = Array.from(menuRef.current?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') ?? [])
    if (items.length === 0) return
    event.preventDefault()
    const index = items.findIndex((item) => item === document.activeElement)
    const next = event.key === 'ArrowDown' ? (index + 1) % items.length : (index - 1 + items.length) % items.length
    items[next]?.focus()
  }

  return (
    <>
      <button
        ref={buttonRef}
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
      {open
        ? createPortal(
            <div
              ref={menuRef}
              role="menu"
              style={style}
              onKeyDown={onMenuKeyDown}
              className="anim-in z-[80] min-w-48 overflow-hidden rounded-lg border border-border bg-surface py-1 shadow-pop"
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
            </div>,
            document.body,
          )
        : null}
    </>
  )
}
