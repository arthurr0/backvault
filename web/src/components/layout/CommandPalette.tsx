import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { useNavigate } from 'react-router'
import {
  Activity,
  Archive,
  Bell,
  CirclePlay,
  Database,
  HardDrive,
  LayoutDashboard,
  ListChecks,
  Plus,
  Search,
  Settings,
} from 'lucide-react'
import { useDestinations, useJobs, useRunJob, useRuns, useSources } from '@/api/hooks'
import { errorMessage } from '@/api/client'
import { useToast } from '@/components/ui/Toast'
import { useCan } from '@/lib/permissions'
import { cn } from '@/lib/utils'

export interface CommandPaletteProps {
  onClose: () => void
  initialQuery?: string
}

interface Command {
  id: string
  group: string
  label: string
  hint?: string
  icon: ReactNode
  run: () => void
}

export function CommandPalette({ onClose, initialQuery = '' }: CommandPaletteProps) {
  const navigate = useNavigate()
  const toast = useToast()
  const [query, setQuery] = useState(initialQuery)
  const [active, setActive] = useState(0)
  const [ready, setReady] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  const jobs = useJobs()
  const sources = useSources()
  const destinations = useDestinations()
  const runs = useRuns({ limit: 8 })
  const runJob = useRunJob()
  const can = useCan()

  useEffect(() => {
    const raf = requestAnimationFrame(() => {
      inputRef.current?.focus()
      setReady(true)
    })
    return () => cancelAnimationFrame(raf)
  }, [])

  const commands = useMemo<Command[]>(() => {
    const go = (to: string) => () => {
      navigate(to)
      onClose()
    }
    const list: Command[] = [
      { id: 'nav-dashboard', group: 'Go to', label: 'Dashboard', icon: <LayoutDashboard className="size-4" />, run: go('/') },
      { id: 'nav-jobs', group: 'Go to', label: 'Jobs', hint: 'g j', icon: <ListChecks className="size-4" />, run: go('/jobs') },
      { id: 'nav-runs', group: 'Go to', label: 'Runs', hint: 'g r', icon: <Activity className="size-4" />, run: go('/runs') },
      { id: 'nav-artifacts', group: 'Go to', label: 'Artifacts', hint: 'g a', icon: <Archive className="size-4" />, run: go('/artifacts') },
      { id: 'nav-sources', group: 'Go to', label: 'Sources', icon: <Database className="size-4" />, run: go('/sources') },
      { id: 'nav-destinations', group: 'Go to', label: 'Destinations', icon: <HardDrive className="size-4" />, run: go('/destinations') },
      { id: 'nav-notifications', group: 'Go to', label: 'Notifications', icon: <Bell className="size-4" />, run: go('/notifications') },
      { id: 'nav-settings', group: 'Go to', label: 'Settings', icon: <Settings className="size-4" />, run: go('/settings') },
    ]

    if (can.admin) {
      list.push(
        { id: 'new-job', group: 'Create', label: 'New job', icon: <Plus className="size-4" />, run: go('/jobs/new') },
        { id: 'new-source', group: 'Create', label: 'New source', icon: <Plus className="size-4" />, run: go('/sources?new=1') },
        { id: 'new-destination', group: 'Create', label: 'New destination', icon: <Plus className="size-4" />, run: go('/destinations?new=1') },
      )
    }

    for (const job of jobs.data ?? []) {
      if (can.run) {
        list.push({
          id: `job-run-${job.id}`,
          group: 'Run job',
          label: `Run ${job.name}`,
          hint: job.slug,
          icon: <CirclePlay className="size-4" />,
          run: () => {
            onClose()
            runJob.mutate(job.slug, {
              onSuccess: (result) => {
                toast.success('Run queued', job.name)
                navigate(`/runs/${result.run.id}`)
              },
              onError: (error) => toast.error('Could not start the run', errorMessage(error)),
            })
          },
        })
      }
      list.push({
        id: `job-open-${job.id}`,
        group: 'Jobs',
        label: job.name,
        hint: job.slug,
        icon: <ListChecks className="size-4" />,
        run: go(`/jobs/${job.slug}`),
      })
    }
    for (const source of sources.data ?? []) {
      list.push({
        id: `source-${source.id}`,
        group: 'Sources',
        label: source.name,
        hint: source.kind,
        icon: <Database className="size-4" />,
        run: go(can.admin ? `/sources?edit=${source.id}` : '/sources'),
      })
    }
    for (const destination of destinations.data ?? []) {
      list.push({
        id: `destination-${destination.id}`,
        group: 'Destinations',
        label: destination.name,
        hint: destination.kind,
        icon: <HardDrive className="size-4" />,
        run: go(can.admin ? `/destinations?edit=${destination.id}` : '/destinations'),
      })
    }
    for (const run of runs.data?.items?.slice(0, 8) ?? []) {
      list.push({
        id: `run-${run.id}`,
        group: 'Recent runs',
        label: `${run.jobName} · ${run.status}`,
        hint: run.id,
        icon: <Activity className="size-4" />,
        run: go(`/runs/${run.id}`),
      })
    }
    return list
  }, [jobs.data, sources.data, destinations.data, runs.data, navigate, onClose, runJob, toast, can])

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase()
    if (!needle) return commands.slice(0, 40)
    return commands
      .filter((command) =>
        `${command.group} ${command.label} ${command.hint ?? ''}`.toLowerCase().includes(needle),
      )
      .slice(0, 40)
  }, [commands, query])

  const activeIndex = filtered.length ? Math.min(active, filtered.length - 1) : 0

  useEffect(() => {
    if (!ready) return
    const node = listRef.current?.querySelector<HTMLElement>('[data-active="true"]')
    node?.scrollIntoView({ block: 'nearest' })
  }, [activeIndex, ready])

  const groups = new Map<string, Command[]>()
  for (const command of filtered) {
    const bucket = groups.get(command.group)
    if (bucket) bucket.push(command)
    else groups.set(command.group, [command])
  }
  const grouped = [...groups.entries()]

  return createPortal(
    <div className="fixed inset-0 z-[70] flex items-start justify-center p-4 pt-[10vh]">
      <div className="fixed inset-0 bg-ink/60 backdrop-blur-[2px]" onClick={onClose} aria-hidden="true" />
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Command palette"
        className="anim-in relative z-10 w-full max-w-xl overflow-hidden rounded-xl border border-border bg-surface shadow-pop"
      >
        <div className="flex items-center gap-2 border-b border-border px-3">
          <Search className="size-4 text-soft" aria-hidden="true" />
          <input
            ref={inputRef}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search pages, jobs, sources, destinations, runs"
            aria-label="Command palette search"
            className="h-12 flex-1 bg-transparent text-sm outline-none placeholder:text-soft"
            onKeyDown={(event) => {
              if (event.key === 'ArrowDown') {
                event.preventDefault()
                setActive((current) => (current + 1) % Math.max(filtered.length, 1))
              } else if (event.key === 'ArrowUp') {
                event.preventDefault()
                setActive((current) => (current - 1 + filtered.length) % Math.max(filtered.length, 1))
              } else if (event.key === 'Enter') {
                event.preventDefault()
                filtered[activeIndex]?.run()
              } else if (event.key === 'Escape') {
                event.preventDefault()
                onClose()
              }
            }}
          />
          <kbd className="rounded border border-border px-1.5 py-0.5 font-mono text-[11px] text-soft">
            esc
          </kbd>
        </div>
        <div ref={listRef} className="scrollbar-thin max-h-[52vh] overflow-y-auto py-2">
          {filtered.length === 0 ? (
            <p className="px-4 py-6 text-center text-sm text-muted">No matches</p>
          ) : (
            grouped.map(([group, items]) => (
              <div key={group} className="mb-1">
                <p className="px-4 py-1 text-[11px] font-semibold uppercase tracking-wide text-soft">
                  {group}
                </p>
                {items.map((command) => {
                  const index = filtered.indexOf(command)
                  const isActive = index === activeIndex
                  return (
                    <button
                      key={command.id}
                      type="button"
                      data-active={isActive}
                      onMouseEnter={() => setActive(index)}
                      onClick={command.run}
                      className={cn(
                        'flex w-full items-center gap-3 px-4 py-2 text-left text-sm',
                        isActive ? 'bg-accent-soft text-text' : 'text-text hover:bg-surface-2',
                      )}
                    >
                      <span className="text-soft">{command.icon}</span>
                      <span className="flex-1 truncate">{command.label}</span>
                      {command.hint ? (
                        <span className="font-mono text-[11px] text-soft">{command.hint}</span>
                      ) : null}
                    </button>
                  )
                })}
              </div>
            ))
          )}
        </div>
      </div>
    </div>,
    document.body,
  )
}
