import { lazy, Suspense } from 'react'
import { Link } from 'react-router'
import {
  Activity,
  Archive,
  CalendarClock,
  CirclePlay,
  Database,
  HardDrive,
  ListChecks,
  TriangleAlert,
} from 'lucide-react'
import { errorMessage } from '@/api/client'
import { useDashboard, useRunJob } from '@/api/hooks'

import { Button } from '@/components/ui/Button'
import { Card, StatTile } from '@/components/ui/Card'
import { EmptyState } from '@/components/ui/EmptyState'
import { Skeleton, SkeletonRows, SkeletonTiles } from '@/components/ui/Skeleton'
import { StatusPill } from '@/components/ui/StatusPill'
import { Td, TableWrap, Th, Tr } from '@/components/ui/Table'
import { RelativeTime } from '@/components/ui/Time'
import { useToast } from '@/components/ui/Toast'
import { formatBytes, formatDuration, formatNumber } from '@/lib/format'
import { usePageMeta } from '@/lib/pageMeta'
import { useCan } from '@/lib/permissions'
import type { DailyStat } from '@/api/types'

const DailyChart = lazy(() => import('@/components/DailyChart').then((m) => ({ default: m.DailyChart })))

interface ChartPoint extends DailyStat {
  gib: number
}

export function DashboardPage() {
  usePageMeta('Dashboard')
  const { data, isLoading, isError, error } = useDashboard()
  const runJob = useRunJob()
  const toast = useToast()
  const can = useCan()

  if (isLoading) {
    return (
      <div className="space-y-4">
        <SkeletonTiles count={8} />
        <Skeleton className="h-72 w-full" />
        <SkeletonRows rows={6} />
      </div>
    )
  }

  if (isError || !data) {
    return (
      <EmptyState
        icon={<TriangleAlert className="size-6" />}
        title="The dashboard could not be loaded"
        description={errorMessage(error)}
      />
    )
  }

  const points: ChartPoint[] = data.daily.map((day) => ({
    ...day,
    gib: Number((day.bytes / 1024 ** 3).toFixed(3)),
  }))
  const maxDestinationBytes = Math.max(1, ...data.destinations.map((d) => d.bytes))

  const triggerRun = (slug: string, name: string) => {
    runJob.mutate(slug, {
      onSuccess: () => toast.success('Run queued', name),
      onError: (err) => toast.error('Could not start the run', errorMessage(err)),
    })
  }

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatTile
          label="Jobs"
          value={formatNumber(data.jobs)}
          sub={`${data.jobsEnabled} enabled`}
          icon={<ListChecks className="size-4" />}
        />
        <StatTile
          label="Overdue"
          value={formatNumber(data.jobsOverdue)}
          tone={data.jobsOverdue > 0 ? 'warning' : 'neutral'}
          sub="past the expected window"
          icon={<CalendarClock className="size-4" />}
        />
        <StatTile
          label="Failing"
          value={formatNumber(data.jobsFailing)}
          tone={data.jobsFailing > 0 ? 'danger' : 'neutral'}
          sub="last run did not succeed"
          icon={<TriangleAlert className="size-4" />}
        />
        <StatTile
          label="Running"
          value={formatNumber(data.runsRunning)}
          tone={data.runsRunning > 0 ? 'running' : 'neutral'}
          sub="right now"
          icon={<Activity className="size-4" />}
        />
        <StatTile
          label="Success 24h"
          value={formatNumber(data.runs24hSuccess)}
          tone="success"
          sub="completed runs"
        />
        <StatTile
          label="Failed 24h"
          value={formatNumber(data.runs24hFailed)}
          tone={data.runs24hFailed > 0 ? 'danger' : 'neutral'}
          sub="needs attention"
        />
        <StatTile
          label="Artifacts"
          value={formatNumber(data.artifacts)}
          sub="stored objects"
          icon={<Archive className="size-4" />}
        />
        <StatTile
          label="Storage"
          value={formatBytes(data.totalBytes)}
          tone="accent"
          sub="across all destinations"
          icon={<HardDrive className="size-4" />}
        />
      </div>

      <div className="grid gap-4 xl:grid-cols-3">
        <Card
          className="xl:col-span-2"
          title="Last 30 days"
          description="Runs per day and the volume uploaded"
        >
          <Suspense fallback={<Skeleton className="h-72 w-full" />}>
            <DailyChart points={points} />
          </Suspense>
        </Card>

        <Card title="Storage by destination" description="Bytes currently accounted for">
          {data.destinations.length === 0 ? (
            <EmptyState
              icon={<HardDrive className="size-5" />}
              title="No destinations yet"
              description="Add a destination to start storing artifacts."
              action={
                can.admin ? (
                  <Link to="/destinations?new=1">
                    <Button variant="primary" size="sm">
                      Add destination
                    </Button>
                  </Link>
                ) : null
              }
            />
          ) : (
            <ul className="space-y-3">
              {data.destinations.map((usage) => (
                <li key={usage.destinationId}>
                  <div className="flex items-baseline justify-between gap-2">
                    <Link
                      to={`/destinations?edit=${usage.destinationId}`}
                      className="truncate text-[13px] font-medium text-text hover:text-accent"
                    >
                      {usage.destinationName}
                    </Link>
                    <span className="font-mono text-xs text-muted">{formatBytes(usage.bytes)}</span>
                  </div>
                  <div className="mt-1.5 h-1.5 w-full overflow-hidden rounded-full bg-surface-3">
                    <div
                      className="h-full rounded-full bg-accent"
                      style={{ width: `${Math.max(2, (usage.bytes / maxDestinationBytes) * 100)}%` }}
                    />
                  </div>
                  <p className="mt-1 text-[11px] text-soft">
                    {usage.kind} · {formatNumber(usage.artifacts)} artifacts
                  </p>
                </li>
              ))}
            </ul>
          )}
        </Card>
      </div>

      <div className="grid gap-4 xl:grid-cols-3">
        <Card
          className="xl:col-span-2"
          title="Recent runs"
          description="Updated live"
          actions={
            <Link to="/runs">
              <Button size="sm">All runs</Button>
            </Link>
          }
          bodyClassName="p-0"
        >
          {data.recentRuns.length === 0 ? (
            <div className="p-4">
              <EmptyState
                icon={<Activity className="size-5" />}
                title="No runs yet"
                description="Create a job and run it to see the history here."
              />
            </div>
          ) : (
            <TableWrap>
              <thead>
                <tr>
                  <Th>Job</Th>
                  <Th>Status</Th>
                  <Th>Kind</Th>
                  <Th align="right">Size</Th>
                  <Th align="right">Duration</Th>
                  <Th align="right">Started</Th>
                </tr>
              </thead>
              <tbody>
                {data.recentRuns.map((run) => (
                  <Tr key={run.id}>
                    <Td className="max-w-[16rem]">
                      <Link to={`/runs/${run.id}`} className="truncate font-medium hover:text-accent">
                        {run.jobName}
                      </Link>
                    </Td>
                    <Td>
                      <StatusPill status={run.status} size="sm" />
                    </Td>
                    <Td className="text-muted">{run.kind}</Td>
                    <Td align="right" className="font-mono text-muted">
                      {run.bytes ? formatBytes(run.bytes) : '-'}
                    </Td>
                    <Td align="right" className="font-mono text-muted">
                      {run.durationMs ? formatDuration(run.durationMs) : '-'}
                    </Td>
                    <Td align="right" className="text-muted">
                      <RelativeTime value={run.startedAt ?? run.queuedAt} />
                    </Td>
                  </Tr>
                ))}
              </tbody>
            </TableWrap>
          )}
        </Card>

        <div className="space-y-4">
          <Card title="Upcoming" description="Next scheduled runs">
            {data.upcoming.length === 0 ? (
              <p className="text-sm text-muted">No scheduled jobs.</p>
            ) : (
              <ul className="space-y-2.5">
                {data.upcoming.map((job) => (
                  <li key={job.id} className="flex items-center justify-between gap-2">
                    <Link
                      to={`/jobs/${job.slug}`}
                      className="min-w-0 flex-1 truncate text-[13px] font-medium text-text hover:text-accent"
                    >
                      {job.name}
                    </Link>
                    <RelativeTime value={job.nextRunAt} className="text-xs text-muted" />
                  </li>
                ))}
              </ul>
            )}
          </Card>

          <Card title="Problem jobs" description="Overdue or failing">
            {data.problemJobs.length === 0 ? (
              <p className="text-sm text-muted">Everything is on schedule.</p>
            ) : (
              <ul className="space-y-3">
                {data.problemJobs.map((job) => (
                  <li key={job.id} className="flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <Link
                        to={`/jobs/${job.slug}`}
                        className="block truncate text-[13px] font-medium text-text hover:text-accent"
                      >
                        {job.name}
                      </Link>
                      <div className="mt-1 flex items-center gap-1.5">
                        {job.overdue ? <StatusPill status="overdue" size="sm" /> : null}
                        {job.lastRun ? <StatusPill status={job.lastRun.status} size="sm" /> : null}
                      </div>
                    </div>
                    {can.run ? (
                      <Button
                        size="sm"
                        icon={<CirclePlay className="size-3.5" />}
                        onClick={() => triggerRun(job.slug, job.name)}
                      >
                        Run
                      </Button>
                    ) : null}
                  </li>
                ))}
              </ul>
            )}
          </Card>

          {can.admin ? (
            <Card title="Quick start" description="Set up your first backup">
              <div className="flex flex-wrap gap-2">
                <Link to="/sources?new=1">
                  <Button size="sm" icon={<Database className="size-3.5" />}>
                    Add source
                  </Button>
                </Link>
                <Link to="/destinations?new=1">
                  <Button size="sm" icon={<HardDrive className="size-3.5" />}>
                    Add destination
                  </Button>
                </Link>
                <Link to="/jobs/new">
                  <Button size="sm" variant="primary" icon={<ListChecks className="size-3.5" />}>
                    Create job
                  </Button>
                </Link>
              </div>
            </Card>
          ) : null}
        </div>
      </div>
    </div>
  )
}
