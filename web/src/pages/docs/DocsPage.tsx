import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useLocation, useParams } from 'react-router'
import { ArrowLeft, ArrowRight, ChevronDown, ChevronRight, ExternalLink, ListTree } from 'lucide-react'
import { useDocsIndex, useDocsPage } from '@/api/hooks'
import { EmptyState } from '@/components/ui/EmptyState'
import { usePageMeta } from '@/lib/pageMeta'
import { cn } from '@/lib/utils'
import { DocsMarkdown } from './DocsMarkdown'
import { DocsNav } from './DocsNav'
import { DocsSearch } from './DocsSearch'
import { DocsToc } from './DocsToc'
import { docPathForRoute, githubUrlFor, routeForDoc } from './paths'
import './docs.css'

function ArticleSkeleton() {
  return (
    <div className="space-y-3">
      <div className="skeleton h-7 w-2/5" />
      <div className="skeleton h-4 w-full" />
      <div className="skeleton h-4 w-11/12" />
      <div className="skeleton h-4 w-3/4" />
      <div className="skeleton h-24 w-full" />
      <div className="skeleton h-4 w-5/6" />
      <div className="skeleton h-4 w-2/3" />
    </div>
  )
}

export default function DocsPage() {
  const params = useParams()
  const location = useLocation()
  const index = useDocsIndex()
  const articleRef = useRef<HTMLElement>(null)
  const [navOpen, setNavOpen] = useState(false)

  const items = useMemo(() => index.data?.items ?? [], [index.data])
  const known = useMemo(() => new Set(items.map((item) => item.path)), [items])
  const slug = params['*'] ?? ''
  const docPath = docPathForRoute(slug, known)
  const page = useDocsPage(index.data ? docPath : undefined)

  const title = page.data?.title ?? (index.isLoading || page.isLoading ? 'Documentation' : 'Not found')
  const section = page.data?.section ?? ''
  usePageMeta(title, [{ label: 'Documentation', to: '/docs' }])

  useEffect(() => {
    if (!page.data) return
    const target = decodeURIComponent(location.hash.replace('#', ''))
    if (!target) {
      window.scrollTo({ top: 0 })
      return
    }
    const node = document.getElementById(target)
    if (!node) return
    node.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }, [page.data, location.hash, location.key])

  const notFound = page.isError || (index.data && !known.has(docPath))

  return (
    <div className="docs-layout">
      <header className="mb-5 flex flex-col gap-3 border-b border-border pb-4 lg:flex-row lg:items-center lg:justify-between">
        <nav aria-label="Breadcrumb" className="flex min-w-0 items-center gap-1 text-[12px] text-soft">
          <Link to="/docs" className="rounded hover:text-text">
            Docs
          </Link>
          {section ? (
            <>
              <ChevronRight className="size-3 shrink-0" aria-hidden="true" />
              <span>{section}</span>
            </>
          ) : null}
          {page.data ? (
            <>
              <ChevronRight className="size-3 shrink-0" aria-hidden="true" />
              <span className="truncate text-text">{page.data.title}</span>
            </>
          ) : null}
        </nav>
        <div className="flex shrink-0 items-center gap-2">
          <DocsSearch />
          <a
            href={githubUrlFor(docPath)}
            target="_blank"
            rel="noopener noreferrer"
            className="hidden shrink-0 items-center gap-1.5 rounded-lg border border-border px-2.5 py-2 text-[12.5px] text-muted transition-colors hover:bg-surface-2 hover:text-text sm:inline-flex"
          >
            View on GitHub
            <ExternalLink className="size-3.5" aria-hidden="true" />
          </a>
        </div>
      </header>

      <div className="grid gap-x-10 gap-y-5 lg:grid-cols-[13.5rem_minmax(0,1fr)] xl:grid-cols-[13.5rem_minmax(0,1fr)_13rem]">
        <div className="lg:sticky lg:top-[4.75rem] lg:max-h-[calc(100vh-6.5rem)] lg:self-start lg:overflow-y-auto lg:pb-6 scrollbar-thin">
          <button
            type="button"
            onClick={() => setNavOpen((current) => !current)}
            aria-expanded={navOpen}
            aria-controls="docs-nav-panel"
            className="flex w-full items-center justify-between gap-2 rounded-lg border border-border bg-surface px-3 py-2 text-[13px] font-medium text-text lg:hidden"
          >
            <span className="flex items-center gap-2">
              <ListTree className="size-4 text-soft" aria-hidden="true" />
              {page.data ? page.data.title : 'Documentation'}
            </span>
            <ChevronDown
              className={cn('size-4 text-soft transition-transform', navOpen && 'rotate-180')}
              aria-hidden="true"
            />
          </button>
          <div
            id="docs-nav-panel"
            className={cn(
              'mt-2 rounded-lg border border-border bg-surface p-2 lg:mt-0 lg:block lg:border-0 lg:bg-transparent lg:p-0',
              navOpen ? 'block' : 'hidden',
            )}
          >
            <DocsNav
              items={items}
              sections={index.data?.sections ?? []}
              currentPath={docPath}
              onNavigate={() => setNavOpen(false)}
            />
          </div>
        </div>

        <div className="min-w-0">
          {notFound ? (
            <EmptyState
              title="That page does not exist"
              description={`There is no documentation page at ${docPath}.`}
              action={
                <Link
                  to="/docs"
                  className="inline-flex items-center gap-1.5 rounded-lg border border-border px-3 py-2 text-[13px] text-text hover:bg-surface-2"
                >
                  <ArrowLeft className="size-4" aria-hidden="true" />
                  Back to the documentation index
                </Link>
              }
            />
          ) : (
            <>
              <article ref={articleRef} className="docs-article">
                {page.data ? (
                  <DocsMarkdown docPath={page.data.path} markdown={page.data.markdown} />
                ) : (
                  <ArticleSkeleton />
                )}
              </article>
              {page.data ? (
                <nav
                  aria-label="Pagination"
                  className="mt-10 grid gap-3 border-t border-border pt-5 sm:grid-cols-2"
                >
                  {page.data.prev ? (
                    <Link
                      to={routeForDoc(page.data.prev.path)}
                      className="group flex flex-col gap-0.5 rounded-xl border border-border px-3.5 py-2.5 transition-colors hover:border-border-strong hover:bg-surface-2"
                    >
                      <span className="flex items-center gap-1 text-[11px] text-soft">
                        <ArrowLeft className="size-3" aria-hidden="true" />
                        Previous
                      </span>
                      <span className="text-[13.5px] font-medium text-text">{page.data.prev.title}</span>
                    </Link>
                  ) : (
                    <span />
                  )}
                  {page.data.next ? (
                    <Link
                      to={routeForDoc(page.data.next.path)}
                      className="group flex flex-col items-end gap-0.5 rounded-xl border border-border px-3.5 py-2.5 text-right transition-colors hover:border-border-strong hover:bg-surface-2 sm:col-start-2"
                    >
                      <span className="flex items-center gap-1 text-[11px] text-soft">
                        Next
                        <ArrowRight className="size-3" aria-hidden="true" />
                      </span>
                      <span className="text-[13.5px] font-medium text-text">{page.data.next.title}</span>
                    </Link>
                  ) : null}
                </nav>
              ) : null}
            </>
          )}
        </div>

        <div className="hidden xl:sticky xl:top-[4.75rem] xl:block xl:max-h-[calc(100vh-6.5rem)] xl:self-start xl:overflow-y-auto xl:pb-6 scrollbar-thin">
          <DocsToc articleRef={articleRef} headings={page.data?.headings ?? []} />
        </div>
      </div>
    </div>
  )
}
