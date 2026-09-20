import { Suspense, useCallback, useEffect, useRef, useState } from 'react'
import { Outlet, useNavigate } from 'react-router'
import { LoaderCircle, X } from 'lucide-react'
import { useEventStream } from '@/api/events'
import { useLogout } from '@/api/hooks'
import type { MeResponse } from '@/api/types'
import { ErrorBoundary } from '@/components/ErrorBoundary'
import { IconButton } from '@/components/ui/Button'
import { CommandPalette } from './CommandPalette'
import { ShortcutsDialog } from './ShortcutsDialog'
import { SidebarContent } from './Sidebar'
import { TopBar } from './TopBar'
import { cn } from '@/lib/utils'

function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  const tag = target.tagName.toLowerCase()
  return tag === 'input' || tag === 'textarea' || tag === 'select' || target.isContentEditable
}

export function AppLayout({ me }: { me?: MeResponse }) {
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [paletteQuery, setPaletteQuery] = useState('')
  const [shortcutsOpen, setShortcutsOpen] = useState(false)
  const navigate = useNavigate()
  const logout = useLogout()
  const streamState = useEventStream(true)
  const pendingG = useRef(false)
  const gTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  const openPalette = useCallback((query = '') => {
    setPaletteQuery(query)
    setPaletteOpen(true)
  }, [])

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        openPalette('')
        return
      }
      if (isTypingTarget(event.target) || event.metaKey || event.ctrlKey || event.altKey) return
      if (event.key === '?') {
        event.preventDefault()
        setShortcutsOpen(true)
        return
      }
      if (event.key.toLowerCase() === 'g') {
        pendingG.current = true
        if (gTimer.current) clearTimeout(gTimer.current)
        gTimer.current = setTimeout(() => {
          pendingG.current = false
        }, 1200)
        return
      }
      if (!pendingG.current) return
      const targets: Record<string, string> = {
        j: '/jobs',
        r: '/runs',
        a: '/artifacts',
        d: '/',
        s: '/sources',
        o: '/hosts',
        n: '/notifications',
        h: '/docs',
      }
      const to = targets[event.key.toLowerCase()]
      if (to) {
        event.preventDefault()
        pendingG.current = false
        navigate(to)
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => {
      window.removeEventListener('keydown', onKeyDown)
      if (gTimer.current) clearTimeout(gTimer.current)
    }
  }, [navigate, openPalette])

  const onLogout = () => {
    logout.mutate(undefined, {
      onSettled: () => {
        window.location.assign('/login')
      },
    })
  }

  return (
    <div className="flex min-h-screen bg-bg">
      <aside className="sticky top-0 hidden h-screen w-60 shrink-0 border-r border-border bg-surface md:block">
        <SidebarContent />
      </aside>

      {drawerOpen ? (
        <div className="fixed inset-0 z-40 md:hidden">
          <div
            className="absolute inset-0 bg-ink/60"
            onClick={() => setDrawerOpen(false)}
            aria-hidden="true"
          />
          <div className="anim-in absolute left-0 top-0 h-full w-64 border-r border-border bg-surface shadow-pop">
            <div className="absolute right-2 top-3">
              <IconButton label="Close navigation" size="sm" onClick={() => setDrawerOpen(false)}>
                <X className="size-4" />
              </IconButton>
            </div>
            <SidebarContent onNavigate={() => setDrawerOpen(false)} />
          </div>
        </div>
      ) : null}

      <div className="flex min-w-0 flex-1 flex-col">
        <TopBar
          me={me}
          streamState={streamState}
          onOpenMenu={() => setDrawerOpen(true)}
          onOpenPalette={() => openPalette('')}
          onRunJob={() => openPalette('Run ')}
          onLogout={onLogout}
        />
        <main className={cn('mx-auto w-full max-w-[1400px] flex-1 px-4 py-5 sm:px-6')}>
          <ErrorBoundary>
            <Suspense
              fallback={
                <div className="flex min-h-[50vh] items-center justify-center">
                  <LoaderCircle className="size-5 animate-spin text-accent" aria-label="Loading" />
                </div>
              }
            >
              <Outlet />
            </Suspense>
          </ErrorBoundary>
        </main>
      </div>

      {paletteOpen ? (
        <CommandPalette
          key={paletteQuery}
          initialQuery={paletteQuery}
          onClose={() => setPaletteOpen(false)}
        />
      ) : null}
      <ShortcutsDialog open={shortcutsOpen} onClose={() => setShortcutsOpen(false)} />
    </div>
  )
}
