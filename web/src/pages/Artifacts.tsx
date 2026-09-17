import { useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { Archive, Download, Funnel, RotateCcw, ShieldCheck, Trash, TriangleAlert } from 'lucide-react'
import { downloadUrl, errorMessage } from '@/api/client'
import {
  useArtifacts,
  useDeleteArtifact,
  useDestinations,
  useJobs,
  useVerifyArtifact,
} from '@/api/hooks'
import { RestoreDialog } from '@/components/RestoreDialog'
import { Button, IconButton } from '@/components/ui/Button'
import { ActionMenu, type MenuAction } from '@/components/ui/Menu'
import { useConfirm } from '@/components/ui/Confirm'
import { CopyButton } from '@/components/ui/Copyable'
import { EmptyState } from '@/components/ui/EmptyState'
import { Input, Select } from '@/components/ui/Input'
import { PAGE_SIZE, Pagination } from '@/components/ui/Pagination'
import { SkeletonRows } from '@/components/ui/Skeleton'
import { StatusPill } from '@/components/ui/StatusPill'
import { TableWrap, Td, Th, Tr } from '@/components/ui/Table'
import { RelativeTime } from '@/components/ui/Time'
import { useToast } from '@/components/ui/Toast'
import { dayEndISO, dayStartISO, formatBytes, toDayInput, truncateMiddle } from '@/lib/format'
import { usePageMeta } from '@/lib/pageMeta'
import { useCan } from '@/lib/permissions'
import type { Artifact } from '@/api/types'

export function ArtifactsPage() {
  usePageMeta('Artifacts', [{ label: 'Backvault', to: '/' }, { label: 'Artifacts' }])
  const [params, setParams] = useSearchParams()
  const jobs = useJobs()
  const destinations = useDestinations()
  const toast = useToast()
  const confirm = useConfirm()
  const verify = useVerifyArtifact()
  const remove = useDeleteArtifact()
  const can = useCan()
  const [restoring, setRestoring] = useState<Artifact | null>(null)

  const filters = {
    job: params.get('job') ?? '',
    destination: params.get('destination') ?? '',
    status: params.get('status') ?? '',
    q: params.get('q') ?? '',
    since: params.get('since') ?? '',
    until: params.get('until') ?? '',
  }
  const page = Math.max(1, Number(params.get('page') ?? '1') || 1)
  const offset = (page - 1) * PAGE_SIZE
  const [since, setSince] = useState(() => toDayInput(params.get('since')))
  const [until, setUntil] = useState(() => toDayInput(params.get('until')))

  const artifacts = useArtifacts({
    job: filters.job || undefined,
    destination: filters.destination || undefined,
    status: filters.status || undefined,
    q: filters.q || undefined,
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

  const onDelete = async (artifact: Artifact) => {
    const ok = await confirm({
      title: 'Delete this artifact',
      description: `${artifact.filename} is removed from ${artifact.destinationName ?? 'the destination'} and marked deleted. This cannot be undone.`,
      confirmLabel: 'Delete artifact',
    })
    if (!ok) return
    remove.mutate(artifact.id, {
      onSuccess: () => toast.success('Artifact deleted', artifact.filename),
      onError: (error) => toast.error('Could not delete the artifact', errorMessage(error)),
    })
  }

  const rowActions = (artifact: Artifact): MenuAction[] => {
    const actions: MenuAction[] = []
    if (can.run) {
      actions.push({
        id: 'verify',
        label: 'Verify',
        icon: <ShieldCheck className="size-3.5" />,
        onSelect: () =>
          verify.mutate(artifact.id, {
            onSuccess: () => toast.success('Verify queued', artifact.filename),
            onError: (error) => toast.error('Could not queue the verification', errorMessage(error)),
          }),
      })
    }
    if (can.admin) {
      actions.push({
        id: 'delete',
        label: 'Delete',
        icon: <Trash className="size-3.5" />,
        tone: 'danger',
        onSelect: () => void onDelete(artifact),
      })
    }
    return actions
  }

  const items = artifacts.data?.items ?? []

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end gap-2">
        <Input
          aria-label="Search by filename"
          placeholder="Search by filename"
          className="w-56"
          value={filters.q}
          onChange={(event) => update('q', event.target.value)}
        />
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
          aria-label="Filter by destination"
          className="w-52"
          value={filters.destination}
          onChange={(event) => update('destination', event.target.value)}
        >
          <option value="">All destinations</option>
          {(destinations.data ?? []).map((destination) => (
            <option key={destination.id} value={destination.id}>
              {destination.name}
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
          <option value="present">Present</option>
          <option value="missing">Missing</option>
          <option value="pruned">Pruned</option>
          <option value="deleted">Deleted</option>
        </Select>
        <label className="flex flex-col gap-1 text-[13px] font-medium text-text">
          Since
          <Input
            type="date"
            className="w-36"
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
            className="w-36"
            value={until}
            onChange={(event) => {
              setUntil(event.target.value)
              update('until', dayEndISO(event.target.value))
            }}
          />
        </label>
        {filters.job ||
        filters.destination ||
        filters.status ||
        filters.q ||
        filters.since ||
        filters.until ? (
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

      {artifacts.isLoading ? (
        <SkeletonRows rows={8} />
      ) : artifacts.isError ? (
        <EmptyState
          icon={<TriangleAlert className="size-6" />}
          title="Artifacts could not be loaded"
          description={errorMessage(artifacts.error)}
        />
      ) : items.length === 0 ? (
        <EmptyState
          icon={<Archive className="size-6" />}
          title="No artifacts found"
          description="Artifacts are created by successful backup runs and pushed uploads."
          action={
            <Link to="/jobs">
              <Button variant="primary">Go to jobs</Button>
            </Link>
          }
        />
      ) : (
        <div className="card overflow-hidden">
          <TableWrap>
            <thead>
              <tr>
                <Th>Filename</Th>
                <Th>Job</Th>
                <Th>Destination</Th>
                <Th>Status</Th>
                <Th>sha256</Th>
                <Th align="right">Size</Th>
                <Th align="right">Created</Th>
                <Th align="right" />
              </tr>
            </thead>
            <tbody>
              {items.map((artifact) => (
                <Tr key={artifact.id}>
                  <Td className="max-w-[20rem]">
                    <span className="block truncate font-mono text-[12.5px]" title={artifact.path}>
                      {artifact.filename}
                    </span>
                    <span className="text-[11px] text-soft">
                      {artifact.compression !== 'none' ? artifact.compression : 'no compression'}
                      {artifact.encryption === 'age' ? ' · age' : ''}
                    </span>
                  </Td>
                  <Td className="max-w-[11rem] text-muted">
                    <Link
                      to={`/jobs/${artifact.jobSlug}`}
                      className="block truncate hover:text-accent"
                      title={artifact.jobName ?? artifact.jobSlug}
                    >
                      {artifact.jobName ?? artifact.jobSlug}
                    </Link>
                  </Td>
                  <Td className="max-w-[8rem] text-muted" title={artifact.destinationName}>
                    <span className="block truncate">{artifact.destinationName}</span>
                  </Td>
                  <Td>
                    <StatusPill status={artifact.status} size="sm" />
                  </Td>
                  <Td>
                    <div className="flex items-center gap-1">
                      <span
                        className="whitespace-nowrap font-mono text-[11.5px] text-muted"
                        title={artifact.sha256}
                      >
                        {artifact.sha256 ? truncateMiddle(artifact.sha256, 9) : '-'}
                      </span>
                      {artifact.sha256 ? <CopyButton value={artifact.sha256} label="Copy sha256" /> : null}
                    </div>
                  </Td>
                  <Td align="right" className="whitespace-nowrap font-mono text-muted">
                    {formatBytes(artifact.size)}
                  </Td>
                  <Td align="right" className="whitespace-nowrap text-muted">
                    <RelativeTime value={artifact.createdAt} />
                  </Td>
                  <Td align="right" className="whitespace-nowrap pr-4">
                    <div className="flex items-center justify-end gap-1">
                      <a
                        href={downloadUrl(`/artifacts/${artifact.id}/download`)}
                        download={artifact.filename}
                      >
                        <IconButton
                          label={`Download ${artifact.filename} from ${artifact.destinationName}`}
                          size="sm"
                        >
                          <Download className="size-4" />
                        </IconButton>
                      </a>
                      {can.admin ? (
                        <IconButton
                          label={`Restore ${artifact.filename} from ${artifact.destinationName}`}
                          size="sm"
                          onClick={() => setRestoring(artifact)}
                        >
                          <RotateCcw className="size-4" />
                        </IconButton>
                      ) : null}
                      {can.run || can.admin ? (
                        <ActionMenu
                          label={`More actions for ${artifact.filename} on ${artifact.destinationName}`}
                          actions={rowActions(artifact)}
                        />
                      ) : null}
                    </div>
                  </Td>
                </Tr>
              ))}
            </tbody>
          </TableWrap>
        </div>
      )}

      {artifacts.data && !artifacts.isError ? (
        <Pagination
          offset={offset}
          limit={PAGE_SIZE}
          total={artifacts.data.total}
          count={items.length}
          noun="artifacts"
          onOffsetChange={goToOffset}
        />
      ) : null}

      <RestoreDialog
        key={restoring?.id ?? 'none'}
        artifact={restoring}
        onClose={() => setRestoring(null)}
      />
    </div>
  )
}
