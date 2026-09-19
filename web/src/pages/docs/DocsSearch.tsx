import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router'
import { Search } from 'lucide-react'
import { useDocsSearch } from '@/api/hooks'
import { cn } from '@/lib/utils'
import { routeForDoc } from './paths'

export function DocsSearch() {
  const [query, setQuery] = useState('')
  const [debounced, setDebounced] = useState('')
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)
  const navigate = useNavigate()
  const inputRef = useRef<HTMLInputElement>(null)
  const boxRef = useRef<HTMLDivElement>(null)
  const results = useDocsSearch(debounced)
  const items = results.data ?? []

  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(query), 180)
    return () => window.clearTimeout(timer)
  }, [query])

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key === '/') {
        event.preventDefault()
        inputRef.current?.focus()
        inputRef.current?.select()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [])

  useEffect(() => {
    const onPointerDown = (event: MouseEvent) => {
      if (!boxRef.current?.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onPointerDown)
    return () => document.removeEventListener('mousedown', onPointerDown)
  }, [])

  const go = (path: string) => {
    setOpen(false)
    setQuery('')
    setDebounced('')
    inputRef.current?.blur()
    navigate(routeForDoc(path))
  }

  const showResults = open && debounced.trim().length > 1

  return (
    <div ref={boxRef} className="relative w-full sm:w-72">
      <Search
        className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-soft"
        aria-hidden="true"
      />
      <input
        ref={inputRef}
        type="search"
        value={query}
        role="searchbox"
        aria-label="Search the documentation"
        placeholder="Search the docs"
        className="input-base h-9 pl-8 pr-12 text-[13px]"
        onChange={(event) => {
          setQuery(event.target.value)
          setActive(0)
          setOpen(true)
        }}
        onFocus={() => setOpen(true)}
        onKeyDown={(event) => {
          if (event.key === 'ArrowDown') {
            event.preventDefault()
            setActive((current) => (items.length ? (current + 1) % items.length : 0))
          } else if (event.key === 'ArrowUp') {
            event.preventDefault()
            setActive((current) => (items.length ? (current - 1 + items.length) % items.length : 0))
          } else if (event.key === 'Enter') {
            event.preventDefault()
            const chosen = items[active] ?? items[0]
            if (chosen) go(chosen.path)
          } else if (event.key === 'Escape') {
            event.preventDefault()
            setOpen(false)
            inputRef.current?.blur()
          }
        }}
      />
      <kbd className="pointer-events-none absolute right-2 top-1/2 hidden -translate-y-1/2 rounded border border-border px-1 font-mono text-[10px] text-soft sm:block">
        ⌘/
      </kbd>
      {showResults ? (
        <div className="anim-in absolute left-0 right-0 top-11 z-40 overflow-hidden rounded-xl border border-border bg-surface shadow-pop">
          {items.length === 0 ? (
            <p className="px-3 py-4 text-center text-[13px] text-muted">
              {results.isLoading ? 'Searching' : 'No matches'}
            </p>
          ) : (
            <ul className="scrollbar-thin max-h-[60vh] overflow-y-auto py-1">
              {items.map((item, index) => (
                <li key={item.path}>
                  <button
                    type="button"
                    onMouseEnter={() => setActive(index)}
                    onClick={() => go(item.path)}
                    className={cn(
                      'block w-full px-3 py-2 text-left',
                      index === active ? 'bg-accent-soft' : 'hover:bg-surface-2',
                    )}
                  >
                    <span className="flex items-baseline justify-between gap-2">
                      <span className="truncate text-[13px] font-medium text-text">{item.title}</span>
                      <span className="shrink-0 text-[10.5px] uppercase tracking-wide text-soft">
                        {item.section}
                      </span>
                    </span>
                    <span className="mt-0.5 line-clamp-2 block text-[11.5px] leading-snug text-muted">
                      {item.snippet}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      ) : null}
    </div>
  )
}
