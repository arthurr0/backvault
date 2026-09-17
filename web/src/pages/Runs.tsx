import { useMemo, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { Activity, Funnel, TriangleAlert } from 'lucide-react'
import { errorMessage } from '@/api/client'
import { useJobs, useRuns } from '@/api/hooks'
import { EmptyState } from '@/components/ui/EmptyState'
import { Button } from '@/components/ui/Button'
import { Input, Select } from '@/components/ui/Input'
import { PAGE_SIZE, Pagination } from '@/components/ui/Pagination'
import { SkeletonRows } from '@/components/ui/Skeleton'
import { StatusPill } from '@/components/ui/StatusPill'
import { TableWrap, Td, Th, Tr } from '@/components/ui/Table'
import { RelativeTime } from '@/components/ui/Time'
import { dayEndISO, dayStartISO, formatBytes, formatDuration, toDayInput } from '@/lib/format'
import { usePageMeta } from '@/lib/pageMeta'

export function RunsPage() {
  usePageMeta('Runs', [{ label: 'Backvault', to: '/' }, { label: 'Runs' }])
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const jobs = useJobs()

  const filters = useMemo(
    () => ({
      job: params.get('job') ?? '',
      status: params.get('status') ?? '',
      kind: params.get('kind') ?? '',
      since: params.get('since') ?? '',
      until: params.get('until') ?? '',
    }),
    [params],
  )
  const page = Math.max(1, Number(params.get('page') ?? '1') || 1)
  const offset = (page - 1) * PAGE_SIZE
  const [since, setSince] = useState(() => toDayInput(filters.since))
  const [until, setUntil] = useState(() => toDayInput(filters.until))

  const runs = useRuns({
    job: filters.job || undefined,
    status: filters.status || undefined,
    kind: filters.kind || undefined,
    since: filters.since || undefined,
    until: filters.until || undefined,
    limit: PAGE_SIZE,
    offset: offset || undefined,
  })

  const update = (key: string, value: string) => {
    const next = new URLSearchParams(params)
    if (value) next.set(key, value)
    else next.delete(key)
    next.delete('page')
    setParams(next, { replace: true })
  }

  const goToOffset = (value: number) => {
    const next = new URLSearchParams(params)
    const target = Math.floor(value / PAGE_SIZE) + 1
    if (target > 1) next.set('page', String(target))
    else next.delete('page')
    setParams(next, { replace: true })
  }

  const items = runs.data?.items ?? []
  const hasFilters = Boolean(
    filters.job || filters.status || filters.kind || filters.since || filters.until,
  )

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end gap-2">
        <Select
          aria-label="Filter by job"
          className="w-52"
          value={filters.job}
          onChange={(event) => update('job', event.target.value)}
        >
          <option value="">All jobs</option>
          {(jobs.data ?? []).map((job) => (
            <option key={job.id} value={job.slug}>
              {job.name}
            </option>
          ))}
        </Select>
        <Select
          aria-label="Filter by status"
          className="w-40"
          value={filters.status}
          onChange={(event) => update('status', event.target.value)}
        >
          <option value="">Any status</option>
          <option value="queued">Queued</option>
          <option value="running">Running</option>
          <option value="success">Success</option>
          <option value="warning">Warning</option>
          <option value="failed">Failed</option>
          <option value="canceled">Canceled</option>
        </Select>
        <Select
          aria-label="Filter by kind"
          className="w-40"
          value={filters.kind}
          onChange={(event) => update('kind', event.target.value)}
        >
          <option value="">Any kind</option>
          <option value="backup">Backup</option>
          <option value="restore">Restore</option>
          <option value="prune">Prune</option>
          <option value="verify">Verify</option>
          <option value="ingest">Ingest</option>
        </Select>
        <label className="flex flex-col gap-1 text-[13px] font-medium text-text">
          Since
          <Input
            type="date"
            className="w-44"
            value={since}
            onChange={(event) => {
              setSince(event.target.value)
              update('since', dayStartISO(event.target.value))
            }}
          />
        </label>
        <label className="flex flex-col gap-1 text-[13px] font-medium text-text">
          Until
          <Input
            type="date"
            className="w-44"
            value={until}
            onChange={(event) => {
              setUntil(event.target.value)
              update('until', dayEndISO(event.target.value))
            }}
          />
        </label>
        {hasFilters ? (
          <Button
            icon={<Funnel className="size-4" />}
            onClick={() => {
              setSince('')
              setUntil('')
              setParams(new URLSearchParams(), { replace: true })
            }}
          >
            Clear
          </Button>
        ) : null}
      </div>

      {runs.isLoading ? (
        <SkeletonRows rows={8} />
      ) : runs.isError ? (
        <EmptyState
          icon={<TriangleAlert className="size-6" />}
          title="Runs could not be loaded"
          description={errorMessage(runs.error)}
        />
      ) : items.length === 0 ? (
        <EmptyState
          icon={<Activity className="size-6" />}
          title="No runs found"
          description="Runs appear here as soon as a job starts, on schedule or manually."
        />
      ) : (
        <div className="card overflow-hidden">
          <TableWrap>
            <thead>
              <tr>
                <Th>Job</Th>
                <Th>Status</Th>
                <Th>Kind</Th>
                <Th>Trigger</Th>
                <Th align="right">Size</Th>
                <Th align="right">Duration</Th>
                <Th align="right">Started</Th>
              </tr>
            </thead>
            <tbody>
              {items.map((run) => (
                <Tr key={run.id} onClick={() => navigate(`/runs/${run.id}`)}>
                  <Td className="max-w-[18rem]">
                    <span className="block truncate font-medium">{run.jobName}</span>
                    <span className="font-mono text-[11px] text-soft">{run.id}</span>
                  </Td>
                  <Td>
                    <StatusPill status={run.status} size="sm" />
                    {run.error ? (
                      <span className="mt-1 block max-w-[16rem] truncate text-[11px] text-danger">
                        {run.error}
                      </span>
                    ) : null}
                  </Td>
                  <Td className="text-muted">{run.kind}</Td>
                  <Td className="text-muted">{run.trigger}</Td>
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
        </div>
      )}

      {runs.data && !runs.isError ? (
        <Pagination
          offset={offset}
          limit={PAGE_SIZE}
          total={runs.data.total}
          count={items.length}
          noun="runs"
          onOffsetChange={goToOffset}
        />
      ) : null}
    </div>
  )
}
