import type { ReactNode } from 'react'
import { Link, useParams } from 'react-router'
import { Ban, CircleCheck, CircleX, Clock, LoaderCircle, CircleMinus, TriangleAlert } from 'lucide-react'
import { errorMessage } from '@/api/client'
import { useArtifacts, useCancelRun, useRun } from '@/api/hooks'
import { LogViewer } from '@/components/LogViewer'
import { Button } from '@/components/ui/Button'
import { Card } from '@/components/ui/Card'
import { useConfirm } from '@/components/ui/Confirm'
import { CopyField } from '@/components/ui/Copyable'
import { EmptyState } from '@/components/ui/EmptyState'
import { SkeletonRows } from '@/components/ui/Skeleton'
import { StatusPill } from '@/components/ui/StatusPill'
import { TableWrap, Td, Th, Tr } from '@/components/ui/Table'
import { RelativeTime } from '@/components/ui/Time'
import { useToast } from '@/components/ui/Toast'
import { formatAbsolute, formatBytes, formatDuration, titleCase } from '@/lib/format'
import { usePageMeta } from '@/lib/pageMeta'
import { useCan } from '@/lib/permissions'
import { isTerminalStatus } from '@/lib/utils'
import type { Stage, StageStatus } from '@/api/types'

const STAGE_ICONS: Record<StageStatus, typeof CircleCheck> = {
  pending: Clock,
  running: LoaderCircle,
  success: CircleCheck,
  failed: CircleX,
  skipped: CircleMinus,
}

const STAGE_TONE: Record<StageStatus, string> = {
  pending: 'text-soft',
  running: 'text-running',
  success: 'text-success',
  failed: 'text-danger',
  skipped: 'text-soft',
}

function stageDuration(stage: Stage): string {
  if (!stage.startedAt) return ''
  const end = stage.finishedAt ? new Date(stage.finishedAt) : new Date()
  return formatDuration(end.getTime() - new Date(stage.startedAt).getTime())
}

export function RunDetailPage() {
  const { id = '' } = useParams()
  const run = useRun(id)
  const artifacts = useArtifacts({ run: id })
  const cancel = useCancelRun()
  const toast = useToast()
  const confirm = useConfirm()
  const can = useCan()

  usePageMeta(run.data ? `Run · ${run.data.jobName}` : 'Run', [
    { label: 'Runs', to: '/runs' },
    { label: run.data?.jobName ?? id },
  ])

  if (run.isLoading) return <SkeletonRows rows={8} />
  if (run.isError || !run.data) {
    return (
      <EmptyState
        icon={<TriangleAlert className="size-6" />}
        title="This run could not be loaded"
        description={errorMessage(run.error)}
        action={
          <Link to="/runs">
            <Button>Back to runs</Button>
          </Link>
        }
      />
    )
  }

  const data = run.data
  const live = !isTerminalStatus(data.status)
  const runArtifacts = (artifacts.data?.items ?? []).filter((artifact) => artifact.runId === data.id)

  const onCancel = async () => {
    const ok = await confirm({
      title: 'Cancel this run',
      description: 'The run context is canceled. Partial uploads are cleaned up where possible.',
      confirmLabel: 'Cancel run',
    })
    if (!ok) return
    cancel.mutate(data.id, {
      onSuccess: () => toast.success('Cancellation requested'),
      onError: (error) => toast.error('Could not cancel the run', errorMessage(error)),
    })
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <Link to={`/jobs/${data.jobSlug}`} className="text-lg font-semibold text-text hover:text-accent">
              {data.jobName}
            </Link>
            <StatusPill status={data.status} />
            <span className="rounded bg-surface-3 px-2 py-0.5 text-[11.5px] text-muted">
              {titleCase(data.kind)}
            </span>
            <span className="rounded bg-surface-3 px-2 py-0.5 text-[11.5px] text-muted">
              {titleCase(data.trigger)}
            </span>
          </div>
          <p className="mt-1 font-mono text-xs text-soft">{data.id}</p>
        </div>
        {live && can.run ? (
          <Button
            variant="danger"
            icon={<Ban className="size-4" />}
            loading={cancel.isPending}
            onClick={() => void onCancel()}
          >
            Cancel run
          </Button>
        ) : null}
      </div>

      {data.error ? (
        <div className="rounded-xl border border-danger/40 bg-danger/10 p-3">
          <p className="flex items-center gap-2 text-sm font-medium text-danger">
            <TriangleAlert className="size-4" aria-hidden="true" />
            The run reported an error
          </p>
          <p className="mt-1 break-words font-mono text-[12.5px] text-text">{data.error}</p>
        </div>
      ) : null}

      <div className="grid gap-4 xl:grid-cols-3 xl:items-start">
        <Card title="Stages" description="Pipeline progress">
          {data.stages.length === 0 ? (
            <p className="text-sm text-muted">The run has not started yet.</p>
          ) : (
            <ol className="relative space-y-3 pl-5">
              <span className="absolute left-[7px] top-2 h-[calc(100%-1rem)] w-px bg-border" aria-hidden="true" />
              {data.stages.map((stage) => {
                const Icon = STAGE_ICONS[stage.status]
                return (
                  <li key={stage.name} className="relative">
                    <Icon
                      aria-hidden="true"
                      className={`absolute -left-5 top-0.5 size-4 bg-surface ${STAGE_TONE[stage.status]} ${
                        stage.status === 'running' ? 'animate-spin' : ''
                      }`}
                    />
                    <div className="flex items-baseline justify-between gap-2">
                      <span className="text-[13px] font-medium text-text">{stage.name}</span>
                      <span className="font-mono text-[11px] text-soft">{stageDuration(stage)}</span>
                    </div>
                    {stage.message ? (
                      <p className="mt-0.5 break-words text-xs text-muted">{stage.message}</p>
                    ) : null}
                    {stage.startedAt ? (
                      <p className="text-[11px] text-soft" title={formatAbsolute(stage.startedAt)}>
                        {formatAbsolute(stage.startedAt)}
                      </p>
                    ) : null}
                  </li>
                )
              })}
            </ol>
          )}
        </Card>

        <div className="flex flex-col gap-4 xl:col-span-2">
          <Card title="Metrics">
            <dl className="grid grid-cols-2 gap-4 sm:grid-cols-3">
              <Metric label="Queued">
                <RelativeTime value={data.queuedAt} />
              </Metric>
              <Metric label="Started">
                <RelativeTime value={data.startedAt} />
              </Metric>
              <Metric label="Finished">
                <RelativeTime value={data.finishedAt} />
              </Metric>
              <Metric label="Duration">{formatDuration(data.durationMs)}</Metric>
              <Metric label="Uploaded">{formatBytes(data.bytes)}</Metric>
              <Metric label="Raw size">{formatBytes(data.rawBytes)}</Metric>
              <Metric label="Attempt">{data.attempt || 1}</Metric>
              <Metric label="Created by">{data.createdBy || 'scheduler'}</Metric>
              <Metric label="Compression ratio">
                {data.rawBytes && data.bytes ? `${(data.rawBytes / data.bytes).toFixed(2)}x` : '-'}
              </Metric>
            </dl>
            {data.filename ? (
              <div className="mt-4 space-y-1.5 border-t border-border pt-3">
                <p className="text-xs font-medium uppercase tracking-wide text-soft">Artifact</p>
                <CopyField value={data.filename} />
                {data.sha256 ? <CopyField value={data.sha256} /> : null}
              </div>
            ) : null}
          </Card>

          <Card title="Log" description="Streamed from the run" bodyClassName="p-0">
            <LogViewer runId={data.id} live={live} />
          </Card>
        </div>
      </div>

      <Card title="Artifacts" description="Objects created by this run" bodyClassName="p-0">
        {runArtifacts.length === 0 ? (
          <div className="p-4">
            <EmptyState
              title="No artifacts"
              description="Artifacts appear once an upload stage completes."
            />
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
              {runArtifacts.map((artifact) => (
                <Tr key={artifact.id}>
                  <Td className="max-w-[20rem]">
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
    </div>
  )
}

function Metric({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <dt className="text-xs uppercase tracking-wide text-soft">{label}</dt>
      <dd className="mt-0.5 font-mono text-sm text-text">{children}</dd>
    </div>
  )
}
