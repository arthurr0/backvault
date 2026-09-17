import type { ReactNode } from 'react'
import { Logo } from '@/components/Logo'

export function AuthShell({
  title,
  description,
  children,
  footer,
}: {
  title: string
  description?: string
  children: ReactNode
  footer?: ReactNode
}) {
  return (
    <div className="flex min-h-screen flex-col items-center justify-center bg-bg px-4 py-10">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex justify-center">
          <Logo />
        </div>
        <div className="card p-6">
          <h1 className="text-lg font-semibold text-text">{title}</h1>
          {description ? <p className="mt-1 text-sm text-muted">{description}</p> : null}
          <div className="mt-5">{children}</div>
        </div>
        {footer ? <div className="mt-4 text-center text-xs text-muted">{footer}</div> : null}
      </div>
    </div>
  )
}
