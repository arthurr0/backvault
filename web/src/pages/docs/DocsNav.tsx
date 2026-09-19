import { Link } from 'react-router'
import type { DocsIndexItem } from '@/api/types'
import { cn } from '@/lib/utils'
import { routeForDoc } from './paths'

export function DocsNav({
  items,
  sections,
  currentPath,
  onNavigate,
}: {
  items: DocsIndexItem[]
  sections: string[]
  currentPath: string
  onNavigate?: () => void
}) {
  return (
    <nav aria-label="Documentation" className="space-y-5 text-[13px]">
      {sections.map((section) => {
        const pages = items.filter((item) => item.section === section)
        if (!pages.length) return null
        return (
          <div key={section}>
            <p className="mb-1.5 px-2 text-[10.5px] font-semibold uppercase tracking-[0.07em] text-soft">
              {section}
            </p>
            <ul className="space-y-0.5">
              {pages.map((page) => {
                const active = page.path === currentPath
                return (
                  <li key={page.path}>
                    <Link
                      to={routeForDoc(page.path)}
                      onClick={onNavigate}
                      aria-current={active ? 'page' : undefined}
                      className={cn(
                        'block rounded-lg px-2 py-1.5 leading-snug transition-colors',
                        active
                          ? 'bg-accent-soft font-medium text-text'
                          : 'text-muted hover:bg-surface-2 hover:text-text',
                      )}
                    >
                      {page.title}
                    </Link>
                  </li>
                )
              })}
            </ul>
          </div>
        )
      })}
    </nav>
  )
}
