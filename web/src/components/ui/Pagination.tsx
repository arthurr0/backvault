import { ChevronLeft, ChevronRight } from 'lucide-react'
import { Button } from '@/components/ui/Button'
import { cn } from '@/lib/utils'

export const PAGE_SIZE = 50

export interface PaginationProps {
  offset: number
  limit: number
  total: number
  count: number
  noun: string
  onOffsetChange: (offset: number) => void
  className?: string
}

export function Pagination({
  offset,
  limit,
  total,
  count,
  noun,
  onOffsetChange,
  className,
}: PaginationProps) {
  if (total <= 0) return null
  const label =
    count === 0 ? `0 of ${total} ${noun}` : `${offset + 1}-${offset + count} of ${total} ${noun}`
  const hasPrevious = offset > 0
  const hasNext = offset + limit < total

  return (
    <div className={cn('flex flex-wrap items-center justify-between gap-2', className)}>
      <span className="text-xs tabular-nums text-muted">{label}</span>
      <div className="flex items-center gap-2">
        <Button
          size="sm"
          disabled={!hasPrevious}
          icon={<ChevronLeft className="size-4" />}
          onClick={() => onOffsetChange(Math.max(0, offset - limit))}
        >
          Previous
        </Button>
        <Button size="sm" disabled={!hasNext} onClick={() => onOffsetChange(offset + limit)}>
          Next
          <ChevronRight className="size-4" />
        </Button>
      </div>
    </div>
  )
}
