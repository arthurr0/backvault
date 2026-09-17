import { useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router'
import {
  Ban,
  CirclePlay,
  Copy,
  ListChecks,
  PencilLine,
  Plus,
  Search,
  Trash,
  TriangleAlert,
} from 'lucide-react'
import { errorMessage } from '@/api/client'
import {
  useDeleteJob,
  useDuplicateJob,
  useJobs,
  useRunJob,
  useSetJobEnabled,
} from '@/api/hooks'
import { Button } from '@/components/ui/Button'
import { useConfirm } from '@/components/ui/Confirm'
import { EmptyState } from '@/components/ui/EmptyState'
import { Input, Select } from '@/components/ui/Input'
import { ActionMenu } from '@/components/ui/Menu'
import { SkeletonRows } from '@/components/ui/Skeleton'
import { StatusPill } from '@/components/ui/StatusPill'
import { TableWrap, Td, Th, Tr } from '@/components/ui/Table'
import { RelativeTime } from '@/components/ui/Time'
import { useToast } from '@/components/ui/Toast'
import { describeCron } from '@/lib/cron'
import { formatBytes } from '@/lib/format'
import { usePageMeta } from '@/lib/pageMeta'
import { useCan } from '@/lib/permissions'
import type { Job } from '@/api/types'

type StatusFilter = 'all' | 'enabled' | 'disabled' | 'overdue' | 'failing'

export function JobsPage() {
  usePageMeta('Jobs', [{ label: 'Backvault', to: '/' }, { label: 'Jobs' }])
  const jobs = useJobs()
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState<StatusFilter>('all')
  const [tag, setTag] = useState('')
  const navigate = useNavigate()
  const toast = useToast()
  const confirm = useConfirm()
  const can = useCan()
  const runJob = useRunJob()
  const setEnabled = useSetJobEnabled()
  const duplicate = useDuplicateJob()
  const remove = useDeleteJob()
  const showRowActions = can.run || can.admin

  const tags = useMemo(() => {
    const set = new Set<string>()
    for (const job of jobs.data ?? []) for (const t of job.tags ?? []) set.add(t)
    return [...set].sort()
  }, [jobs.data])

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase()
    return (jobs.data ?? []).filter((job) => {
      if (needle && !`${job.name} ${job.slug} ${job.sourceName ?? ''}`.toLowerCase().includes(needle)) {
        return false
      }
      if (tag && !(job.tags ?? []).includes(tag)) return false
      if (status === 'enabled' && !job.enabled) return false
      if (status === 'disabled' && job.enabled) return false
      if (status === 'overdue' && !job.overdue) return false
      if (status === 'failing' && job.lastRun?.status !== 'failed') return false
      return true
    })
  }, [jobs.data, query, status, tag])

  const onRun = (job: Job) => {
    runJob.mutate(job.slug, {
      onSuccess: (result) => {
        toast.success('Run queued', job.name)
        navigate(`/runs/${result.run.id}`)
      },
      onError: (error) => toast.error('Could not start the run', errorMessage(error)),
    })
  }

  const onDelete = async (job: Job) => {
    const ok = await confirm({
      title: `Delete ${job.name}`,
      description:
        'The job and its schedule are removed. Stored artifacts stay listed and can still be downloaded or restored.',
      confirmLabel: 'Delete job',
      confirmText: job.slug,
    })
    if (!ok) return
    remove.mutate(
      { id: job.slug },
      {
        onSuccess: () => toast.success('Job deleted', job.name),
        onError: (error) => toast.error('Could not delete the job', errorMessage(error)),
      },
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-52 flex-1">
          <Search
            className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-soft"
            aria-hidden="true"
          />
          <Input
            aria-label="Search jobs"
            placeholder="Search jobs"
            className="pl-8"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </div>
        <Select
          aria-label="Filter by state"
          className="w-40"
          value={status}
          onChange={(event) => setStatus(event.target.value as StatusFilter)}
        >
          <option value="all">All jobs</option>
          <option value="enabled">Enabled</option>
          <option value="disabled">Disabled</option>
          <option value="overdue">Overdue</option>
          <option value="failing">Failing</option>
        </Select>
        {tags.length ? (
          <Select
            aria-label="Filter by tag"
            className="w-40"
            value={tag}
            onChange={(event) => setTag(event.target.value)}
          >
            <option value="">All tags</option>
            {tags.map((item) => (
              <option key={item} value={item}>
                {item}
              </option>
            ))}
          </Select>
        ) : null}
        {can.admin ? (
          <Link to="/jobs/new">
            <Button variant="primary" icon={<Plus className="size-4" />}>
              New job
            </Button>
          </Link>
        ) : null}
      </div>

      {jobs.isLoading ? (
        <SkeletonRows rows={6} />
      ) : jobs.isError ? (
        <EmptyState
          icon={<TriangleAlert className="size-6" />}
          title="Jobs could not be loaded"
          description={errorMessage(jobs.error)}
        />
      ) : filtered.length === 0 ? (
        <EmptyState
          icon={<ListChecks className="size-6" />}
          title={jobs.data?.length ? 'No jobs match these filters' : 'No jobs yet'}
          description={
            jobs.data?.length
              ? 'Clear the filters to see every job.'
              : 'A job connects one source to one or more destinations on a schedule.'
          }
          action={
            jobs.data?.length ? (
              <Button
                onClick={() => {
                  setQuery('')
                  setStatus('all')
                  setTag('')
                }}
              >
                Clear filters
              </Button>
            ) : can.admin ? (
              <Link to="/jobs/new">
                <Button variant="primary" icon={<Plus className="size-4" />}>
                  Create the first job
                </Button>
              </Link>
            ) : null
          }
        />
      ) : (
        <div className="card overflow-hidden">
          <TableWrap>
            <thead>
              <tr>
                <Th>Job</Th>
                <Th>State</Th>
                <Th>Source</Th>
                <Th>Destinations</Th>
                <Th>Schedule</Th>
                <Th>Last run</Th>
                <Th align="right">Size</Th>
                {showRowActions ? <Th align="right" /> : null}
              </tr>
            </thead>
            <tbody>
              {filtered.map((job) => (
                <Tr key={job.id}>
                  <Td className="max-w-[18rem]">
                    <Link to={`/jobs/${job.slug}`} className="block truncate font-medium hover:text-accent">
                      {job.name}
                    </Link>
                    <span className="font-mono text-[11px] text-soft">{job.slug}</span>
                    {job.tags?.length ? (
                      <div className="mt-1 flex flex-wrap gap-1">
                        {job.tags.map((item) => (
                          <span
                            key={item}
                            className="rounded bg-surface-3 px-1.5 py-0.5 text-[10.5px] text-muted"
                          >
                            {item}
                          </span>
                        ))}
                      </div>
                    ) : null}
                  </Td>
                  <Td>
                    <div className="flex flex-col items-start gap-1">
                      <StatusPill status={job.enabled ? 'enabled' : 'disabled'} size="sm" />
                      {job.overdue ? <StatusPill status="overdue" size="sm" /> : null}
                    </div>
                  </Td>
                  <Td className="text-muted">
                    <span className="block truncate">{job.sourceName ?? '-'}</span>
                    <span className="text-[11px] text-soft">{job.sourceKind}</span>
                  </Td>
                  <Td className="text-muted">
                    <span className="block max-w-[12rem] truncate">
                      {(job.destinationNames ?? []).join(', ') || '-'}
                    </span>
                  </Td>
                  <Td className="text-muted">
                    <span className="block max-w-[14rem] truncate" title={job.schedule || 'Manual only'}>
                      {job.schedule ? describeCron(job.schedule) : 'Manual only'}
                    </span>
                    {job.nextRunAt ? (
                      <RelativeTime value={job.nextRunAt} prefix="next" className="text-[11px] text-soft" />
                    ) : null}
                  </Td>
                  <Td>
                    {job.lastRun ? (
                      <div className="flex flex-col items-start gap-1">
                        <Link to={`/runs/${job.lastRun.id}`}>
                          <StatusPill status={job.lastRun.status} size="sm" />
                        </Link>
                        <RelativeTime
                          value={job.lastRun.finishedAt ?? job.lastRun.startedAt}
                          className="text-[11px] text-soft"
                        />
                      </div>
                    ) : (
                      <span className="text-soft">Never</span>
                    )}
                  </Td>
                  <Td align="right" className="font-mono text-muted">
                    {formatBytes(job.totalBytes)}
                  </Td>
                  {showRowActions ? (
                    <Td align="right" className="pr-4">
                      <div className="flex items-center justify-end gap-1">
                        {can.run ? (
                          <Button
                            size="sm"
                            icon={<CirclePlay className="size-3.5" />}
                            onClick={() => onRun(job)}
                          >
                            Run
                          </Button>
                        ) : null}
                        {can.admin ? (
                          <ActionMenu
                            label={`Actions for ${job.name}`}
                            actions={[
                              {
                                id: 'edit',
                                label: 'Edit',
                                icon: <PencilLine className="size-3.5" />,
                                onSelect: () => navigate(`/jobs/${job.slug}/edit`),
                              },
                              {
                                id: 'toggle',
                                label: job.enabled ? 'Disable' : 'Enable',
                                icon: <Ban className="size-3.5" />,
                                onSelect: () =>
                                  setEnabled.mutate(
                                    { id: job.slug, enabled: !job.enabled },
                                    {
                                      onSuccess: () =>
                                        toast.success(
                                          job.enabled ? 'Job disabled' : 'Job enabled',
                                          job.name,
                                        ),
                                      onError: (error) =>
                                        toast.error('Could not update the job', errorMessage(error)),
                                    },
                                  ),
                              },
                              {
                                id: 'duplicate',
                                label: 'Duplicate',
                                icon: <Copy className="size-3.5" />,
                                onSelect: () =>
                                  duplicate.mutate(job.slug, {
                                    onSuccess: (copy) => {
                                      toast.success('Job duplicated', copy.name)
                                      navigate(`/jobs/${copy.slug}/edit`)
                                    },
                                    onError: (error) =>
                                      toast.error('Could not duplicate the job', errorMessage(error)),
                                  }),
                              },
                              {
                                id: 'delete',
                                label: 'Delete',
                                icon: <Trash className="size-3.5" />,
                                tone: 'danger',
                                onSelect: () => void onDelete(job),
                              },
                            ]}
                          />
                        ) : null}
                      </div>
                    </Td>
                  ) : null}
                </Tr>
              ))}
            </tbody>
          </TableWrap>
        </div>
      )}
    </div>
  )
}
