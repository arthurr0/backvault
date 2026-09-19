import { useEffect, useState, type RefObject } from 'react'
import { Link, useLocation } from 'react-router'
import type { DocsHeading } from '@/api/types'
import { cn } from '@/lib/utils'

export function DocsToc({
  articleRef,
  headings,
}: {
  articleRef: RefObject<HTMLElement | null>
  headings: DocsHeading[]
}) {
  const [activeId, setActiveId] = useState('')
  const location = useLocation()

  useEffect(() => {
    const root = articleRef.current
    if (!root) return
    const nodes = headings
      .map((heading) => root.querySelector<HTMLElement>(`#${CSS.escape(heading.id)}`))
      .filter((node): node is HTMLElement => node !== null)
    if (!nodes.length) return

    const visible = new Set<string>()
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) visible.add(entry.target.id)
          else visible.delete(entry.target.id)
        }
        const first = nodes.find((node) => visible.has(node.id))
        if (first) {
          setActiveId(first.id)
          return
        }
        let above = ''
        for (const node of nodes) {
          if (node.getBoundingClientRect().top < 140) above = node.id
        }
        setActiveId(above || nodes[0].id)
      },
      { rootMargin: '-88px 0px -62% 0px', threshold: [0, 1] },
    )
    for (const node of nodes) observer.observe(node)
    return () => observer.disconnect()
  }, [articleRef, headings])

  if (headings.length < 2) return null

  return (
    <div className="text-[12.5px]">
      <p className="mb-2 text-[10.5px] font-semibold uppercase tracking-[0.07em] text-soft">
        On this page
      </p>
      <ul className="space-y-0.5 border-l border-border">
        {headings.map((heading) => {
          const active = heading.id === activeId
          return (
            <li key={heading.id}>
              <Link
                to={{ pathname: location.pathname, hash: `#${heading.id}` }}
                data-active={active}
                className={cn(
                  '-ml-px block border-l py-1 leading-snug transition-colors',
                  heading.level === 3 ? 'pl-5 text-[12px]' : 'pl-3',
                  active
                    ? 'border-accent font-medium text-text'
                    : 'border-transparent text-muted hover:text-text',
                )}
              >
                {heading.text}
              </Link>
            </li>
          )
        })}
      </ul>
    </div>
  )
}
