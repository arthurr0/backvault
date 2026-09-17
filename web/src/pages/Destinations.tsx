import { useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { FolderOpen, HardDrive, PencilLine, Plug, Plus, Trash, TriangleAlert } from 'lucide-react'
import { errorMessage } from '@/api/client'
import {
  useDeleteDestination,
  useDestinationSpecs,
  useDestinations,
  useJobs,
  useTestDestination,
} from '@/api/hooks'
import { DriverResourceDialog } from '@/components/DriverResourceDialog'
import { Button } from '@/components/ui/Button'
import { useConfirm } from '@/components/ui/Confirm'
import { EmptyState } from '@/components/ui/EmptyState'
import { Input } from '@/components/ui/Input'
import { SkeletonRows } from '@/components/ui/Skeleton'
import { StatusPill } from '@/components/ui/StatusPill'
import { RelativeTime } from '@/components/ui/Time'
import { useToast } from '@/components/ui/Toast'
import { formatBytes, formatNumber } from '@/lib/format'
import { DriverIcon } from '@/lib/icons'
import { usePageMeta } from '@/lib/pageMeta'
import { useCan } from '@/lib/permissions'
import type { Destination } from '@/api/types'

export function DestinationsPage() {
  usePageMeta('Destinations', [{ label: 'Backvault', to: '/' }, { label: 'Destinations' }])
  const destinations = useDestinations()
  const specs = useDestinationSpecs()
  const jobs = useJobs()
  const remove = useDeleteDestination()
  const test = useTestDestination()
  const toast = useToast()
  const confirm = useConfirm()
  const can = useCan()
  const [params, setParams] = useSearchParams()
  const [query, setQuery] = useState('')

  const editing = useMemo(() => {
    const id = params.get('edit')
    if (!id) return null
    return (destinations.data ?? []).find((item) => item.id === id) ?? null
  }, [params, destinations.data])
  const creating = params.get('new') === '1'

  const setDialog = (next: URLSearchParams) => setParams(next, { replace: true })
  const closeDialog = () => {
    const next = new URLSearchParams(params)
    next.delete('edit')
    next.delete('new')
    setDialog(next)
  }
  const openCreate = () => {
    const next = new URLSearchParams(params)
    next.set('new', '1')
    next.delete('edit')
    setDialog(next)
  }
  const openEdit = (destination: Destination) => {
    const next = new URLSearchParams(params)
    next.set('edit', destination.id)
    next.delete('new')
    setDialog(next)
  }

  const filtered = (destinations.data ?? []).filter((destination) =>
    `${destination.name} ${destination.kind} ${destination.description}`
      .toLowerCase()
      .includes(query.toLowerCase()),
  )
  const totalBytes = Math.max(1, ...(destinations.data ?? []).map((item) => item.usedBytes))

  const onDelete = async (destination: Destination) => {
    if (destination.jobCount > 0) {
      toast.warning('This destination is in use', `${destination.jobCount} jobs reference it.`)
      return
    }
    const ok = await confirm({
      title: `Delete ${destination.name}`,
      description: 'Stored objects are not touched, only the destination definition is removed.',
      confirmLabel: 'Delete destination',
    })
    if (!ok) return
    remove.mutate(destination.id, {
      onSuccess: () => toast.success('Destination deleted', destination.name),
      onError: (error) => toast.error('Could not delete the destination', errorMessage(error)),
    })
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          aria-label="Search destinations"
          placeholder="Search destinations"
          className="w-56"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
        {can.admin ? (
          <Button
            className="ml-auto"
            variant="primary"
            icon={<Plus className="size-4" />}
            onClick={openCreate}
          >
            New destination
          </Button>
        ) : null}
      </div>

      {destinations.isLoading ? (
        <SkeletonRows rows={4} />
      ) : destinations.isError ? (
        <EmptyState
          icon={<TriangleAlert className="size-6" />}
          title="Destinations could not be loaded"
          description={errorMessage(destinations.error)}
        />
      ) : filtered.length === 0 ? (
        <EmptyState
          icon={<HardDrive className="size-6" />}
          title={destinations.data?.length ? 'No destinations match' : 'No destinations yet'}
          description="A destination is a local directory, an S3 bucket, an SFTP host or WebDAV storage."
          action={
            can.admin ? (
              <Button variant="primary" icon={<Plus className="size-4" />} onClick={openCreate}>
                Add a destination
              </Button>
            ) : null
          }
        />
      ) : (
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {filtered.map((destination) => {
            const spec = (specs.data ?? []).find((item) => item.kind === destination.kind)
            const usedBy = (jobs.data ?? []).filter((job) =>
              (job.destinationIds ?? []).includes(destination.id),
            )
            return (
              <article key={destination.id} className="card flex flex-col gap-3 p-4">
                <div className="flex items-start gap-3">
                  <span className="rounded-lg bg-surface-3 p-2 text-accent">
                    <DriverIcon icon={spec?.icon} kind={destination.kind} className="size-5" />
                  </span>
                  <div className="min-w-0 flex-1">
                    <h3 className="truncate text-sm font-semibold text-text">{destination.name}</h3>
                    <p className="text-xs text-muted">{spec?.label ?? destination.kind}</p>
                  </div>
                  {destination.lastTestOk === undefined ? null : (
                    <StatusPill status={destination.lastTestOk ? 'ok' : 'failed'} size="sm" />
                  )}
                </div>

                <div>
                  <div className="flex items-baseline justify-between text-xs">
                    <span className="text-muted">Stored</span>
                    <span className="font-mono text-text">{formatBytes(destination.usedBytes)}</span>
                  </div>
                  <div className="mt-1.5 h-1.5 w-full overflow-hidden rounded-full bg-surface-3">
                    <div
                      className="h-full rounded-full bg-accent"
                      style={{ width: `${Math.max(2, (destination.usedBytes / totalBytes) * 100)}%` }}
                    />
                  </div>
                  <p className="mt-1 text-[11px] text-soft">
                    {formatNumber(destination.artifactCount)} artifacts
                    {destination.lastTestAt ? ' · tested ' : ''}
                    {destination.lastTestAt ? <RelativeTime value={destination.lastTestAt} /> : null}
                  </p>
                </div>

                <div className="text-xs text-muted">
                  <span>Used by </span>
                  {usedBy.length === 0 ? (
                    <span>no jobs</span>
                  ) : (
                    <span className="inline-flex flex-wrap gap-1">
                      {usedBy.slice(0, 3).map((job) => (
                        <Link
                          key={job.id}
                          to={`/jobs/${job.slug}`}
                          className="rounded bg-surface-3 px-1.5 py-0.5 hover:text-accent"
                        >
                          {job.name}
                        </Link>
                      ))}
                      {usedBy.length > 3 ? <span>+{usedBy.length - 3}</span> : null}
                    </span>
                  )}
                </div>

                <div className="mt-auto flex flex-wrap gap-1.5 pt-1">
                  {can.admin ? (
                    <Button
                      size="sm"
                      icon={<PencilLine className="size-3.5" />}
                      onClick={() => openEdit(destination)}
                    >
                      Edit
                    </Button>
                  ) : null}
                  <Link to={`/destinations/${destination.id}/browse`}>
                    <Button size="sm" icon={<FolderOpen className="size-3.5" />}>
                      Browse
                    </Button>
                  </Link>
                  {can.admin ? (
                    <>
                      <Button
                        size="sm"
                        icon={<Plug className="size-3.5" />}
                        loading={test.isPending && test.variables?.id === destination.id}
                        onClick={() =>
                          test.mutate(
                            { id: destination.id, kind: destination.kind, config: destination.config },
                            {
                              onSuccess: (result) =>
                                result.ok
                                  ? toast.success('Connection succeeded', destination.name)
                                  : toast.error('Connection failed', result.message),
                              onError: (error) =>
                                toast.error('The test could not be run', errorMessage(error)),
                            },
                          )
                        }
                      >
                        Test
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        icon={<Trash className="size-3.5" />}
                        onClick={() => void onDelete(destination)}
                      >
                        Delete
                      </Button>
                    </>
                  ) : null}
                </div>
              </article>
            )
          })}
        </div>
      )}

      <DriverResourceDialog
        kind="destination"
        specs={specs.data ?? []}
        record={editing}
        open={can.admin && (creating || Boolean(editing))}
        onClose={closeDialog}
      />
    </div>
  )
}
