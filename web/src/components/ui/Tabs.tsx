import { cn } from '@/lib/utils'

export interface TabItem {
  id: string
  label: string
}

export function Tabs({
  items,
  value,
  onChange,
  className,
}: {
  items: TabItem[]
  value: string
  onChange: (id: string) => void
  className?: string
}) {
  return (
    <div
      role="tablist"
      aria-orientation="horizontal"
      className={cn('scrollbar-thin flex gap-1 overflow-x-auto border-b border-border', className)}
    >
      {items.map((item) => {
        const active = item.id === value
        return (
          <button
            key={item.id}
            role="tab"
            type="button"
            aria-selected={active}
            onClick={() => onChange(item.id)}
            className={cn(
              '-mb-px whitespace-nowrap border-b-2 px-3 py-2 text-[13px] font-medium transition-colors',
              active
                ? 'border-accent text-text'
                : 'border-transparent text-muted hover:border-border-strong hover:text-text',
            )}
          >
            {item.label}
          </button>
        )
      })}
    </div>
  )
}
