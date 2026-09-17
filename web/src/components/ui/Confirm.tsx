import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react'
import { Dialog } from './Dialog'
import { Button } from './Button'

export interface ConfirmOptions {
  title: string
  description?: ReactNode
  confirmLabel?: string
  cancelLabel?: string
  tone?: 'danger' | 'primary'
  confirmText?: string
}

type Resolver = (value: boolean) => void

const ConfirmContext = createContext<((options: ConfirmOptions) => Promise<boolean>) | null>(null)

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [options, setOptions] = useState<ConfirmOptions | null>(null)
  const [typed, setTyped] = useState('')
  const resolver = useRef<Resolver | null>(null)

  const confirm = useCallback((next: ConfirmOptions) => {
    setTyped('')
    setOptions(next)
    return new Promise<boolean>((resolve) => {
      resolver.current = resolve
    })
  }, [])

  const settle = useCallback((value: boolean) => {
    resolver.current?.(value)
    resolver.current = null
    setOptions(null)
    setTyped('')
  }, [])

  const value = useMemo(() => confirm, [confirm])
  const blocked = Boolean(options?.confirmText) && typed !== options?.confirmText

  return (
    <ConfirmContext.Provider value={value}>
      {children}
      <Dialog
        open={Boolean(options)}
        onClose={() => settle(false)}
        title={options?.title ?? ''}
        width="sm"
        footer={
          <>
            <Button onClick={() => settle(false)}>{options?.cancelLabel ?? 'Cancel'}</Button>
            <Button
              variant={options?.tone === 'primary' ? 'primary' : 'danger'}
              disabled={blocked}
              onClick={() => settle(true)}
            >
              {options?.confirmLabel ?? 'Confirm'}
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          {options?.description ? (
            <div className="text-sm text-muted">{options.description}</div>
          ) : null}
          {options?.confirmText ? (
            <div className="space-y-1.5">
              <label htmlFor="confirm-text" className="text-[13px] font-medium text-text">
                Type <span className="font-mono text-accent">{options.confirmText}</span> to confirm
              </label>
              <input
                id="confirm-text"
                className="input-base font-mono"
                value={typed}
                autoComplete="off"
                onChange={(event) => setTyped(event.target.value)}
              />
            </div>
          ) : null}
        </div>
      </Dialog>
    </ConfirmContext.Provider>
  )
}

export function useConfirm() {
  const ctx = useContext(ConfirmContext)
  if (!ctx) throw new Error('useConfirm must be used inside ConfirmProvider')
  return ctx
}
