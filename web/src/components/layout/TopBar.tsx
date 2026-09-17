import { Link, useNavigate } from 'react-router'
import { ChevronRight, CirclePlay, Menu, Moon, Search, Sun, User } from 'lucide-react'
import { Button, IconButton } from '@/components/ui/Button'
import { ActionMenu } from '@/components/ui/Menu'
import { usePageMetaValue } from '@/lib/pageMeta'
import { useCan } from '@/lib/permissions'
import { useTheme } from '@/lib/theme'
import { cn } from '@/lib/utils'
import type { MeResponse } from '@/api/types'

export function TopBar({
  onOpenMenu,
  onOpenPalette,
  onRunJob,
  onLogout,
  me,
  streamState,
}: {
  onOpenMenu: () => void
  onOpenPalette: () => void
  onRunJob: () => void
  onLogout: () => void
  me?: MeResponse
  streamState: 'connecting' | 'open' | 'closed'
}) {
  const meta = usePageMetaValue()
  const theme = useTheme()
  const navigate = useNavigate()
  const can = useCan()

  return (
    <header className="sticky top-0 z-30 flex h-16 shrink-0 items-center gap-2 border-b border-border bg-bg px-4 sm:gap-3">
      <IconButton label="Open navigation" className="md:hidden" onClick={onOpenMenu}>
        <Menu className="size-5" />
      </IconButton>

      <div className="min-w-0 flex-1">
        {meta.crumbs.length ? (
          <nav aria-label="Breadcrumb" className="flex items-center gap-1 text-[11.5px] text-soft">
            {meta.crumbs.map((crumb, index) => (
              <span key={`${crumb.label}-${index}`} className="flex items-center gap-1">
                {index > 0 ? <ChevronRight className="size-3" aria-hidden="true" /> : null}
                {crumb.to ? (
                  <Link to={crumb.to} className="rounded hover:text-text">
                    {crumb.label}
                  </Link>
                ) : (
                  <span>{crumb.label}</span>
                )}
              </span>
            ))}
          </nav>
        ) : null}
        <h1 className="truncate text-[15px] font-semibold text-text">{meta.title}</h1>
      </div>

      <span
        title={
          streamState === 'open'
            ? 'Live updates connected'
            : streamState === 'connecting'
              ? 'Connecting to live updates'
              : 'Live updates disconnected'
        }
        className="hidden items-center gap-1.5 rounded-full border border-border px-2 py-1 text-[11px] text-muted sm:inline-flex"
      >
        <span
          className={cn(
            'size-1.5 rounded-full',
            streamState === 'open' ? 'bg-success' : streamState === 'connecting' ? 'bg-warning' : 'bg-soft',
          )}
        />
        Live
      </span>

      <span className="hidden sm:block">
        <Button size="sm" onClick={onOpenPalette} icon={<Search className="size-3.5" />} className="text-muted">
          Search
          <kbd className="ml-1 rounded border border-border px-1 font-mono text-[10px] text-soft">
            ⌘K
          </kbd>
        </Button>
      </span>

      {can.run ? (
        <Button
          size="sm"
          variant="primary"
          aria-label="Run job"
          onClick={onRunJob}
          icon={<CirclePlay className="size-3.5" />}
        >
          <span className="hidden sm:inline">Run job</span>
        </Button>
      ) : null}

      <IconButton
        label={theme.resolved === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}
        size="sm"
        onClick={theme.toggle}
      >
        {theme.resolved === 'dark' ? <Sun className="size-4" /> : <Moon className="size-4" />}
      </IconButton>

      <ActionMenu
        label="Account menu"
        trigger={
          <span className="flex items-center gap-2">
            <User className="size-4" aria-hidden="true" />
            <span className="hidden text-[13px] lg:inline">{me?.user?.name ?? 'Account'}</span>
          </span>
        }
        actions={[
          {
            id: 'account',
            label: me?.user?.email ?? 'Signed in',
            onSelect: () => undefined,
            disabled: true,
          },
          { id: 'settings', label: 'Settings', onSelect: () => navigate('/settings') },
          { id: 'logout', label: 'Sign out', tone: 'danger', onSelect: onLogout },
        ]}
      />
    </header>
  )
}
