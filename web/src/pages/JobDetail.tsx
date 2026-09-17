import { useState, type ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import {
  Ban,
  CirclePlay,
  Copy,
  Download,
  PencilLine,
  Scissors,
  Trash,
  TriangleAlert,
} from 'lucide-react'
import { downloadUrl, errorMessage } from '@/api/client'
import {
  useDeleteJob,
  useDuplicateJob,
  useJob,
  useJobArtifacts,
  useJobRuns,
  usePruneJob,
  useRunJob,
  useSetJobEnabled,
  useSources,
} from '@/api/hooks'
import { PushPanel } from '@/components/PushPanel'
import { Button } from '@/components/ui/Button'
import { Card } from '@/components/ui/Card'
import { useConfirm } from '@/components/ui/Confirm'
import { CopyField } from '@/components/ui/Copyable'
import { EmptyState } from '@/components/ui/EmptyState'
import { SkeletonRows } from '@/components/ui/Skeleton'
import { StatusPill } from '@/components/ui/StatusPill'
import { TableWrap, Td, Th, Tr } from '@/components/ui/Table'
import { Tabs } from '@/components/ui/Tabs'
import { RelativeTime } from '@/components/ui/Time'
import { useToast } from '@/components/ui/Toast'
import { describeCron, nextOccurrences } from '@/lib/cron'
import { formatAbsolute, formatBytes, formatDuration, formatNumber, localTimeZone } from '@/lib/format'
import { usePageMeta } from '@/lib/pageMeta'
import { useCan } from '@/lib/permissions'
import { describeRetention, isRetentionEmpty } from '@/lib/retention'

export function JobDetailPage() {
  const { slug = '' } = useParams()
  const job = useJob(slug)
  const runs = useJobRuns(slug)
  const artifacts = useJobArtifacts(slug)
  const sources = useSources()
  const navigate = useNavigate()
  const toast = useToast()
  const confirm = useConfirm()
  const can = useCan()
  const runJob = useRunJob()
  const prune = usePruneJob()
  const duplicate = useDuplicateJob()
  const setEnabled = useSetJobEnabled()
  const remove = useDeleteJob()
  const [tab, setTab] = useState('runs')

  usePageMeta(job.data?.name ?? 'Job', [{ label: 'Jobs', to: '/jobs' }, { label: job.data?.name ?? slug }])

  if (job.isLoading) return <SkeletonRows rows={8} />
  if (job.isError || !job.data) {
    return (
      <EmptyState
        icon={<TriangleAlert className="size-6" />}
        title="This job could not be loaded"
        description={errorMessage(job.error)}
        action={
          <Link to="/jobs">
            <Button>Back to jobs</Button>
          </Link>
        }
      />
    )
  }

  const data = job.data
  const source = (sources.data ?? []).find((item) => item.id === data.sourceId)
  const isPush = (data.sourceKind ?? source?.kind) === 'push'
  const upcoming = data.schedule ? nextOccurrences(data.schedule, data.timezone || 'UTC', 3) : []

  const onDelete = async () => {
    const ok = await confirm({
      title: `Delete ${data.name}`,
      description: 'The schedule stops immediately. Artifacts stay listed unless you remove them separately.',
      confirmLabel: 'Delete job',
      confirmText: data.slug,
    })
    if (!ok) return
    remove.mutate(
      { id: data.slug },
      {
        onSuccess: () => {
          toast.success('Job deleted', data.name)
          navigate('/jobs')
        },
        onError: (error) => toast.error('Could not delete the job', errorMessage(error)),
      },
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="truncate text-lg font-semibold text-text">{data.name}</h2>
            <StatusPill status={data.enabled ? 'enabled' : 'disabled'} size="sm" />
            {data.overdue ? <StatusPill status="overdue" size="sm" /> : null}
            {data.lastRun ? <StatusPill status={data.lastRun.status} size="sm" /> : null}
          </div>
          <p className="mt-1 font-mono text-xs text-soft">{data.slug}</p>
          {data.description ? <p className="mt-1 text-sm text-muted">{data.description}</p> : null}
        </div>
        {can.run || can.admin ? (
          <div className="flex flex-wrap items-center gap-2">
            {can.run ? (
              <Button
                variant="primary"
                disabled={isPush}
                title={isPush ? 'Push jobs receive artifacts through the ingest API' : undefined}
                icon={<CirclePlay className="size-4" />}
                loading={runJob.isPending}
                onClick={() =>
                  runJob.mutate(data.slug, {
                    onSuccess: (result) => {
                      toast.success('Run queued', data.name)
                      navigate(`/runs/${result.run.id}`)
                    },
                    onError: (error) => toast.error('Could not start the run', errorMessage(error)),
                  })
                }
              >
                Run now
              </Button>
            ) : null}
            {can.admin ? (
              <>
                <Button
                  icon={<Scissors className="size-4" />}
                  loading={prune.isPending}
                  onClick={() =>
                    prune.mutate(data.slug, {
                      onSuccess: () => toast.success('Prune queued', data.name),
                      onError: (error) => toast.error('Could not queue the prune', errorMessage(error)),
                    })
                  }
                >
                  Prune
                </Button>
                <Link to={`/jobs/${data.slug}/edit`}>
                  <Button icon={<PencilLine className="size-4" />}>Edit</Button>
                </Link>
                <Button
                  icon={<Ban className="size-4" />}
                  onClick={() =>
                    setEnabled.mutate(
                      { id: data.slug, enabled: !data.enabled },
                      {
                        onSuccess: () =>
                          toast.success(data.enabled ? 'Job disabled' : 'Job enabled', data.name),
                        onError: (error) => toast.error('Could not update the job', errorMessage(error)),
                      },
                    )
                  }
                >
                  {data.enabled ? 'Disable' : 'Enable'}
                </Button>
                <Button
                  icon={<Copy className="size-4" />}
                  onClick={() =>
                    duplicate.mutate(data.slug, {
                      onSuccess: (copy) => navigate(`/jobs/${copy.slug}/edit`),
                      onError: (error) => toast.error('Could not duplicate the job', errorMessage(error)),
                    })
                  }
                >
                  Duplicate
                </Button>
                <a href={downloadUrl('/export')} download="backvault-config.yaml">
                  <Button icon={<Download className="size-4" />}>Export YAML</Button>
                </a>
                <Button variant="danger" icon={<Trash className="size-4" />} onClick={() => void onDelete()}>
                  Delete
                </Button>
              </>
            ) : null}
          </div>
        ) : null}
      </div>

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <Card title="Schedule">
          <p className="text-sm text-text">
            {data.schedule ? describeCron(data.schedule) : 'Manual only'}
          </p>
          {data.schedule ? (
            <>
              <p className="mt-1 font-mono text-xs text-soft">
                {data.schedule} · {data.timezone || 'UTC'}
              </p>
              <p className="mt-3 text-xs font-medium uppercase tracking-wide text-soft">
                Next runs, shown in {localTimeZone()}
              </p>
              <ul className="mt-1.5 space-y-1">
                {upcoming.map((date) => (
                  <li key={date.toISOString()} className="text-xs text-muted">
                    {formatAbsolute(date)}
                  </li>
                ))}
              </ul>
            </>
          ) : (
            <p className="mt-1 text-xs text-muted">
              {isPush ? 'This job waits for pushed artifacts.' : 'Start this job from here or the API.'}
            </p>
          )}
        </Card>

        <Card title="Source">
          <p className="text-sm text-text">{data.sourceName ?? source?.name ?? '-'}</p>
          <p className="mt-1 text-xs text-muted">{data.sourceKind ?? source?.kind}</p>
          {isPush && data.expectedIntervalMinutes ? (
            <p className="mt-3 text-xs text-muted">
              Expected every {data.expectedIntervalMinutes} minutes
            </p>
          ) : null}
        </Card>

        <Card title="Destinations">
          <ul className="space-y-1 text-sm text-text">
            {(data.destinationNames ?? []).map((name) => (
              <li key={name} className="truncate">
                {name}
              </li>
            ))}
            {(data.destinationNames ?? []).length === 0 ? (
              <li className="text-muted">No destinations</li>
            ) : null}
          </ul>
        </Card>

        <Card title="Stored">
          <p className="font-mono text-lg text-text">{formatBytes(data.totalBytes)}</p>
          <p className="mt-1 text-xs text-muted">
            {formatNumber(data.artifactCount)} artifacts
            {data.lastRun ? ` · last run took ${formatDuration(data.lastRun.durationMs)}` : ''}
          </p>
        </Card>
      </div>

      {isPush ? (
        <Card title="How to push" description="Send artifacts to this job from any host">
          <PushPanel slug={data.slug} expectedIntervalMinutes={data.expectedIntervalMinutes} />
        </Card>
      ) : null}

      <div className="grid gap-4 xl:grid-cols-3">
        <Card className="xl:col-span-2" bodyClassName="p-0" title="History">
          <Tabs
            className="px-4"
            value={tab}
            onChange={setTab}
            items={[
              { id: 'runs', label: 'Runs' },
              { id: 'artifacts', label: 'Artifacts' },
            ]}
          />
          {tab === 'runs' ? (
            runs.isLoading ? (
              <div className="p-4">
                <SkeletonRows rows={5} />
              </div>
            ) : (runs.data ?? []).length === 0 ? (
              <div className="p-4">
                <EmptyState title="No runs yet" description="Run the job to create the first artifact." />
              </div>
            ) : (
              <TableWrap>
                <thead>
                  <tr>
                    <Th>Status</Th>
                    <Th>Kind</Th>
                    <Th>Trigger</Th>
                    <Th align="right">Size</Th>
                    <Th align="right">Duration</Th>
                    <Th align="right">Started</Th>
                  </tr>
                </thead>
                <tbody>
                  {(runs.data ?? []).map((run) => (
                    <Tr key={run.id} onClick={() => navigate(`/runs/${run.id}`)}>
                      <Td>
                        <StatusPill status={run.status} size="sm" />
                      </Td>
                      <Td className="text-muted">{run.kind}</Td>
                      <Td className="text-muted">{run.trigger}</Td>
                      <Td align="right" className="font-mono text-muted">
                        {run.bytes ? formatBytes(run.bytes) : '-'}
                      </Td>
                      <Td align="right" className="font-mono text-muted">
                        {formatDuration(run.durationMs)}
                      </Td>
                      <Td align="right" className="text-muted">
                        <RelativeTime value={run.startedAt ?? run.queuedAt} />
                      </Td>
                    </Tr>
                  ))}
                </tbody>
              </TableWrap>
            )
          ) : artifacts.isLoading ? (
            <div className="p-4">
              <SkeletonRows rows={5} />
            </div>
          ) : (artifacts.data ?? []).length === 0 ? (
            <div className="p-4">
              <EmptyState title="No artifacts" description="Artifacts appear after a successful run." />
            </div>
          ) : (
            <TableWrap>
              <thead>
                <tr>
                  <Th>Filename</Th>
                  <Th>Destination</Th>
                  <Th>Status</Th>
                  <Th align="right">Size</Th>
                  <Th align="right">Created</Th>
                </tr>
              </thead>
              <tbody>
                {(artifacts.data ?? []).map((artifact) => (
                  <Tr key={artifact.id}>
                    <Td className="max-w-[18rem]">
                      <Link
                        to={`/artifacts?q=${encodeURIComponent(artifact.filename)}`}
                        className="block truncate font-mono text-[12.5px] hover:text-accent"
                      >
                        {artifact.filename}
                      </Link>
                    </Td>
                    <Td className="text-muted">{artifact.destinationName}</Td>
                    <Td>
                      <StatusPill status={artifact.status} size="sm" />
                    </Td>
                    <Td align="right" className="font-mono text-muted">
                      {formatBytes(artifact.size)}
                    </Td>
                    <Td align="right" className="text-muted">
                      <RelativeTime value={artifact.createdAt} />
                    </Td>
                  </Tr>
                ))}
              </tbody>
            </TableWrap>
          )}
        </Card>

        <div className="space-y-4">
          <Card title="Packaging">
            <dl className="space-y-2 text-sm">
              <Row label="Compression">
                {data.compression}
                {data.compressionLevel ? ` (level ${data.compressionLevel})` : ''}
              </Row>
              <Row label="Encryption">{data.encryption === 'age' ? 'age passphrase' : 'none'}</Row>
              <Row label="Verify after upload">{data.verifyAfterUpload ? 'yes' : 'no'}</Row>
              <Row label="Retries">
                {data.retries} · {data.retryDelaySeconds}s delay
              </Row>
              <Row label="Timeout">
                {data.timeoutMinutes ? `${data.timeoutMinutes} min` : '6 hours (default)'}
              </Row>
            </dl>
          </Card>

          <Card title="Retention">
            <p className="text-sm text-muted">
              {describeRetention(data.retention, isRetentionEmpty(data.retention))}
            </p>
          </Card>

          {data.preCommand || data.postCommand ? (
            <Card title="Hooks">
              {data.preCommand ? <CopyField value={data.preCommand} /> : null}
              {data.postCommand ? <CopyField value={data.postCommand} /> : null}
            </Card>
          ) : null}
        </div>
      </div>
    </div>
  )
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-3">
      <dt className="text-muted">{label}</dt>
      <dd className="text-right text-text">{children}</dd>
    </div>
  )
}
