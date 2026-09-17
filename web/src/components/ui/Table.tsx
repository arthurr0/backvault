import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

export function TableWrap({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={cn('scrollbar-thin w-full overflow-x-auto', className)}>
      <table className="w-full min-w-[42rem] border-collapse text-sm">{children}</table>
    </div>
  )
}

export function Th({
  children,
  className,
  align = 'left',
}: {
  children?: ReactNode
  className?: string
  align?: 'left' | 'right' | 'center'
}) {
  return (
    <th
      scope="col"
      className={cn(
        'border-b border-border px-3 py-2 text-xs font-medium uppercase tracking-wide text-muted',
        align === 'right' && 'text-right',
        align === 'center' && 'text-center',
        align === 'left' && 'text-left',
        className,
      )}
    >
      {children}
    </th>
  )
}

export function Td({
  children,
  className,
  align = 'left',
  title,
}: {
  children?: ReactNode
  className?: string
  align?: 'left' | 'right' | 'center'
  title?: string
}) {
  return (
    <td
      title={title}
      className={cn(
        'border-b border-border px-3 py-2.5 align-middle text-text',
        align === 'right' && 'text-right',
        align === 'center' && 'text-center',
        className,
      )}
    >
      {children}
    </td>
  )
}

export function Tr({
  children,
  className,
  onClick,
}: {
  children: ReactNode
  className?: string
  onClick?: () => void
}) {
  return (
    <tr
      onClick={onClick}
      className={cn('transition-colors hover:bg-surface-2', onClick && 'cursor-pointer', className)}
    >
      {children}
    </tr>
  )
}
