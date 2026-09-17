import { useEffect, useState } from 'react'
import { formatAbsolute, formatRelative } from '@/lib/format'
import { cn } from '@/lib/utils'

export function RelativeTime({
  value,
  className,
  prefix,
}: {
  value: string | undefined | null
  className?: string
  prefix?: string
}) {
  const [, setTick] = useState(0)

  useEffect(() => {
    const timer = setInterval(() => setTick((n) => n + 1), 30_000)
    return () => clearInterval(timer)
  }, [])

  if (!value) return <span className={cn('text-soft', className)}>-</span>
  return (
    <time dateTime={value} title={formatAbsolute(value)} className={cn('whitespace-nowrap', className)}>
      {prefix ? `${prefix} ` : ''}
      {formatRelative(value)}
    </time>
  )
}
