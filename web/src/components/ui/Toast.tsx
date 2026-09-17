import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { CircleAlert, CircleCheck, Info, TriangleAlert, X } from 'lucide-react'
import { cn } from '@/lib/utils'

export type ToastTone = 'success' | 'error' | 'warning' | 'info'

export interface ToastItem {
  id: string
  tone: ToastTone
  title: string
  description?: string
}

interface ToastContextValue {
  push: (toast: Omit<ToastItem, 'id'>) => void
  success: (title: string, description?: string) => void
  error: (title: string, description?: string) => void
  info: (title: string, description?: string) => void
  warning: (title: string, description?: string) => void
}

const ToastContext = createContext<ToastContextValue | null>(null)

const ICONS = {
  success: CircleCheck,
  error: CircleAlert,
  warning: TriangleAlert,
  info: Info,
}

const TONES: Record<ToastTone, string> = {
  success: 'text-success',
  error: 'text-danger',
  warning: 'text-warning',
  info: 'text-info',
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([])
  const counter = useRef(0)

  const remove = useCallback((id: string) => {
    setItems((prev) => prev.filter((item) => item.id !== id))
  }, [])

  const push = useCallback(
    (toast: Omit<ToastItem, 'id'>) => {
      counter.current += 1
      const id = `toast-${counter.current}`
      setItems((prev) => [...prev.slice(-4), { ...toast, id }])
      window.setTimeout(() => remove(id), toast.tone === 'error' ? 8000 : 4500)
    },
    [remove],
  )

  const value = useMemo<ToastContextValue>(
    () => ({
      push,
      success: (title, description) => push({ tone: 'success', title, description }),
      error: (title, description) => push({ tone: 'error', title, description }),
      info: (title, description) => push({ tone: 'info', title, description }),
      warning: (title, description) => push({ tone: 'warning', title, description }),
    }),
    [push],
  )

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div
        className="pointer-events-none fixed bottom-4 right-4 z-[60] flex w-[min(24rem,calc(100vw-2rem))] flex-col gap-2"
        role="status"
        aria-live="polite"
      >
        {items.map((item) => {
          const Icon = ICONS[item.tone]
          return (
            <div
              key={item.id}
              className="anim-in pointer-events-auto flex items-start gap-3 rounded-xl border border-border bg-surface p-3 shadow-pop"
            >
              <Icon className={cn('mt-0.5 size-4 shrink-0', TONES[item.tone])} aria-hidden="true" />
              <div className="min-w-0 flex-1">
                <p className="text-sm font-medium text-text">{item.title}</p>
                {item.description ? (
                  <p className="mt-0.5 break-words text-xs text-muted">{item.description}</p>
                ) : null}
              </div>
              <button
                type="button"
                aria-label="Dismiss notification"
                onClick={() => remove(item.id)}
                className="rounded p-0.5 text-soft hover:text-text"
              >
                <X className="size-3.5" />
              </button>
            </div>
          )
        })}
      </div>
    </ToastContext.Provider>
  )
}

export function useToast(): ToastContextValue {
  const ctx = useContext(ToastContext)
  if (!ctx) throw new Error('useToast must be used inside ToastProvider')
  return ctx
}
