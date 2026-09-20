import { useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { Database, PencilLine, Plug, Plus, Server, Trash, TriangleAlert } from 'lucide-react'
import { errorMessage } from '@/api/client'
import { useDeleteSource, useJobs, useSourceSpecs, useSources, useTestSource } from '@/api/hooks'
import { DriverResourceDialog } from '@/components/DriverResourceDialog'
import { Button } from '@/components/ui/Button'
import { useConfirm } from '@/components/ui/Confirm'
import { EmptyState } from '@/components/ui/EmptyState'
import { Input } from '@/components/ui/Input'
import { SkeletonRows } from '@/components/ui/Skeleton'
import { StatusPill } from '@/components/ui/StatusPill'
import { RelativeTime } from '@/components/ui/Time'
import { useToast } from '@/components/ui/Toast'
import { DriverIcon } from '@/lib/icons'
import { usePageMeta } from '@/lib/pageMeta'
import { useCan } from '@/lib/permissions'
import type { Source } from '@/api/types'

export function SourcesPage() {
  usePageMeta('Sources', [{ label: 'Backvault', to: '/' }, { label: 'Sources' }])
  const sources = useSources()
  const specs = useSourceSpecs()
  const jobs = useJobs()
  const remove = useDeleteSource()
  const test = useTestSource()
  const toast = useToast()
  const confirm = useConfirm()
  const can = useCan()
  const [params, setParams] = useSearchParams()
  const [query, setQuery] = useState('')

  const editing = useMemo(() => {
    const id = params.get('edit')
    if (!id) return null
    return (sources.data ?? []).find((source) => source.id === id) ?? null
  }, [params, sources.data])
  const creating = params.get('new') === '1'

  const closeDialog = () => {
    const next = new URLSearchParams(params)
    next.delete('edit')
    next.delete('new')
    setParams(next, { replace: true })
  }

  const openCreate = () => {
    const next = new URLSearchParams(params)
    next.set('new', '1')
    next.delete('edit')
    setParams(next, { replace: true })
  }

  const openEdit = (source: Source) => {
    const next = new URLSearchParams(params)
    next.set('edit', source.id)
    next.delete('new')
    setParams(next, { replace: true })
  }

  const filtered = (sources.data ?? []).filter((source) =>
    `${source.name} ${source.kind} ${source.description}`.toLowerCase().includes(query.toLowerCase()),
  )

  const onDelete = async (source: Source) => {
    if (source.jobCount > 0) {
      toast.warning('This source is in use', `${source.jobCount} jobs reference it.`)
      return
    }
    const ok = await confirm({
      title: `Delete ${source.name}`,
      description: 'The source definition and its stored credentials are removed.',
      confirmLabel: 'Delete source',
    })
    if (!ok) return
    remove.mutate(source.id, {
      onSuccess: () => toast.success('Source deleted', source.name),
      onError: (error) => toast.error('Could not delete the source', errorMessage(error)),
    })
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          aria-label="Search sources"
          placeholder="Search sources"
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
            New source
          </Button>
        ) : null}
      </div>

      {sources.isLoading ? (
        <SkeletonRows rows={4} />
      ) : sources.isError ? (
        <EmptyState
          icon={<TriangleAlert className="size-6" />}
          title="Sources could not be loaded"
          description={errorMessage(sources.error)}
        />
      ) : filtered.length === 0 ? (
        <EmptyState
          icon={<Database className="size-6" />}
          title={sources.data?.length ? 'No sources match' : 'No sources yet'}
          description="A source is a database, a directory, a remote host or a push endpoint."
          action={
            can.admin ? (
              <Button variant="primary" icon={<Plus className="size-4" />} onClick={openCreate}>
                Add a source
              </Button>
            ) : null
          }
        />
      ) : (
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {filtered.map((source) => {
            const spec = (specs.data ?? []).find((item) => item.kind === source.kind)
            const usedBy = (jobs.data ?? []).filter((job) => job.sourceId === source.id)
            return (
              <article key={source.id} className="card flex flex-col gap-3 p-4">
                <div className="flex items-start gap-3">
                  <span className="rounded-lg bg-surface-3 p-2 text-accent">
                    <DriverIcon icon={spec?.icon} kind={source.kind} className="size-5" />
                  </span>
                  <div className="min-w-0 flex-1">
                    <h3 className="truncate text-sm font-semibold text-text">{source.name}</h3>
                    <p className="text-xs text-muted">{spec?.label ?? source.kind}</p>
                    {source.hostId ? (
                      <p className="mt-1 inline-flex max-w-full items-center gap-1 rounded bg-surface-3 px-1.5 py-0.5 text-[11px] text-muted">
                        <Server className="size-3 shrink-0" aria-hidden="true" />
                        <span className="truncate">{source.hostName || 'Remote host'}</span>
                      </p>
                    ) : null}
                  </div>
                  {source.lastTestOk === undefined ? null : (
                    <StatusPill status={source.lastTestOk ? 'ok' : 'failed'} size="sm" />
                  )}
                </div>

                {source.description ? (
                  <p className="line-clamp-2 text-xs text-muted">{source.description}</p>
                ) : null}

                <dl className="space-y-1 text-xs text-muted">
                  <div className="flex justify-between gap-2">
                    <dt>Used by</dt>
                    <dd className="text-text">
                      {usedBy.length === 0 ? (
                        'no jobs'
                      ) : (
                        <span className="flex flex-wrap justify-end gap-1">
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
                    </dd>
                  </div>
                  {source.lastTestAt ? (
                    <div className="flex justify-between gap-2">
                      <dt>Last test</dt>
                      <dd className="text-text">
                        <RelativeTime value={source.lastTestAt} />
                      </dd>
                    </div>
                  ) : null}
                  {source.lastTestOk === false && source.lastTestError ? (
                    <p className="break-words text-danger">{source.lastTestError}</p>
                  ) : null}
                </dl>

                {can.admin ? (
                  <div className="mt-auto flex flex-wrap gap-1.5 pt-1">
                    <Button
                      size="sm"
                      icon={<PencilLine className="size-3.5" />}
                      onClick={() => openEdit(source)}
                    >
                      Edit
                    </Button>
                    <Button
                      size="sm"
                      icon={<Plug className="size-3.5" />}
                      loading={test.isPending && test.variables?.id === source.id}
                      onClick={() =>
                        test.mutate(
                          {
                            id: source.id,
                            kind: source.kind,
                            config: source.config,
                            hostId: source.hostId,
                          },
                          {
                            onSuccess: (result) =>
                              result.ok
                                ? toast.success('Connection succeeded', source.name)
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
                      onClick={() => void onDelete(source)}
                    >
                      Delete
                    </Button>
                  </div>
                ) : null}
              </article>
            )
          })}
        </div>
      )}

      <DriverResourceDialog
        kind="source"
        specs={specs.data ?? []}
        record={editing}
        open={can.admin && (creating || Boolean(editing))}
        onClose={closeDialog}
      />
    </div>
  )
}
