import { useMemo, useState } from 'react'
import { useSearchParams } from 'react-router'
import { KeyRound, PencilLine, Plug, Plus, Server, ShieldCheck, Trash, TriangleAlert } from 'lucide-react'
import { errorMessage } from '@/api/client'
import { useDeleteHost, useHosts, useTestHost } from '@/api/hooks'
import { HostDialog } from '@/components/HostDialog'
import { Button } from '@/components/ui/Button'
import { useConfirm } from '@/components/ui/Confirm'
import { EmptyState } from '@/components/ui/EmptyState'
import { Input } from '@/components/ui/Input'
import { SkeletonRows } from '@/components/ui/Skeleton'
import { StatusPill } from '@/components/ui/StatusPill'
import { Td, TableWrap, Th, Tr } from '@/components/ui/Table'
import { RelativeTime } from '@/components/ui/Time'
import { useToast } from '@/components/ui/Toast'
import { hostAddress } from '@/lib/format'
import { usePageMeta } from '@/lib/pageMeta'
import { useCan } from '@/lib/permissions'
import type { Host } from '@/api/types'

const TOOL_LIMIT = 6

export function HostsPage() {
  usePageMeta('Hosts', [{ label: 'Backvault', to: '/' }, { label: 'Hosts' }])
  const hosts = useHosts()
  const remove = useDeleteHost()
  const test = useTestHost()
  const toast = useToast()
  const confirm = useConfirm()
  const can = useCan()
  const [params, setParams] = useSearchParams()
  const [query, setQuery] = useState('')

  const editing = useMemo(() => {
    const id = params.get('edit')
    if (!id) return null
    return (hosts.data ?? []).find((host) => host.id === id) ?? null
  }, [params, hosts.data])
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
  const openEdit = (host: Host) => {
    const next = new URLSearchParams(params)
    next.set('edit', host.id)
    next.delete('new')
    setParams(next, { replace: true })
  }

  const filtered = (hosts.data ?? []).filter((host) =>
    `${host.name} ${host.address} ${host.user} ${host.description} ${(host.tags ?? []).join(' ')}`
      .toLowerCase()
      .includes(query.toLowerCase()),
  )

  const onTest = (host: Host) => {
    test.mutate(host.id, {
      onSuccess: (result) => {
        if (!result.ok) {
          toast.error('Connection failed', result.message)
          return
        }
        const tools = result.tools?.length ? ` · ${result.tools.join(', ')}` : ''
        toast.success('Connection succeeded', `${result.os ?? host.address}${tools}`)
      },
      onError: (error) => toast.error('The test could not be run', errorMessage(error)),
    })
  }

  const onDelete = async (host: Host) => {
    const ok = await confirm({
      title: `Delete ${host.name}`,
      description: 'The host and its stored credentials are removed. Nothing on the host is changed.',
      confirmLabel: 'Delete host',
    })
    if (!ok) return
    remove.mutate(host.id, {
      onSuccess: () => toast.success('Host deleted', host.name),
      onError: (error) => toast.error('Could not delete the host', errorMessage(error)),
    })
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          aria-label="Search hosts"
          placeholder="Search hosts"
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
            New host
          </Button>
        ) : null}
      </div>

      {hosts.isLoading ? (
        <SkeletonRows rows={3} />
      ) : hosts.isError ? (
        <EmptyState
          icon={<TriangleAlert className="size-6" />}
          title="Hosts could not be loaded"
          description={errorMessage(hosts.error)}
        />
      ) : filtered.length === 0 ? (
        <EmptyState
          icon={<Server className="size-6" />}
          title={hosts.data?.length ? 'No hosts match' : 'No hosts yet'}
          description="A host is a reusable SSH connection. Add one and sources such as files, Docker volumes or database dumps can run on that machine instead of the Backvault server."
          action={
            can.admin ? (
              <Button variant="primary" icon={<Plus className="size-4" />} onClick={openCreate}>
                Add host
              </Button>
            ) : null
          }
        />
      ) : (
        <div className="card overflow-hidden">
          <TableWrap>
            <thead>
              <tr>
                <Th>Host</Th>
                <Th>Address</Th>
                <Th>Auth</Th>
                <Th className="whitespace-nowrap">Tools found</Th>
                <Th align="right">Sources</Th>
                <Th className="whitespace-nowrap">Last test</Th>
                {can.admin ? <Th align="right" /> : null}
              </tr>
            </thead>
            <tbody>
              {filtered.map((host) => {
                const tools = host.tools ?? []
                const inUse = host.sourceCount > 0
                return (
                  <Tr key={host.id}>
                    <Td className="max-w-[16rem]">
                      <span className="flex items-center gap-2">
                        <Server className="size-4 shrink-0 text-soft" aria-hidden="true" />
                        <span className="truncate font-medium">{host.name}</span>
                      </span>
                      {host.description ? (
                        <span className="mt-0.5 block truncate text-[11px] text-soft">
                          {host.description}
                        </span>
                      ) : null}
                    </Td>
                    <Td className="font-mono text-[12.5px] text-muted">{hostAddress(host)}</Td>
                    <Td>
                      <span className="flex flex-wrap items-center gap-1.5">
                        <span className="inline-flex items-center gap-1 text-[12.5px] text-muted">
                          {host.auth === 'key' ? (
                            <KeyRound className="size-3.5 text-soft" aria-hidden="true" />
                          ) : null}
                          {host.auth === 'key' ? 'Key' : 'Password'}
                        </span>
                        {host.sudo ? (
                          <span
                            title="Commands run through sudo -n"
                            className="inline-flex items-center gap-1 rounded bg-surface-3 px-1.5 py-0.5 text-[10.5px] text-muted"
                          >
                            <ShieldCheck className="size-3" aria-hidden="true" />
                            sudo
                          </span>
                        ) : null}
                      </span>
                    </Td>
                    <Td>
                      {tools.length === 0 ? (
                        <span className="text-[12px] text-soft">Not probed yet</span>
                      ) : (
                        <span className="flex max-w-[22rem] flex-wrap gap-1">
                          {tools.slice(0, TOOL_LIMIT).map((tool) => (
                            <span
                              key={tool}
                              className="rounded bg-surface-3 px-1.5 py-0.5 font-mono text-[10.5px] text-muted"
                            >
                              {tool}
                            </span>
                          ))}
                          {tools.length > TOOL_LIMIT ? (
                            <span
                              title={tools.slice(TOOL_LIMIT).join(', ')}
                              className="rounded px-1 py-0.5 text-[10.5px] text-soft"
                            >
                              +{tools.length - TOOL_LIMIT}
                            </span>
                          ) : null}
                        </span>
                      )}
                    </Td>
                    <Td align="right" className="font-mono text-muted">
                      {host.sourceCount}
                    </Td>
                    <Td>
                      {host.lastTestAt ? (
                        <span className="flex flex-col items-start gap-1">
                          <span title={host.lastTestOk === false ? host.lastTestError : undefined}>
                            <StatusPill status={host.lastTestOk ? 'ok' : 'failed'} size="sm" />
                          </span>
                          <RelativeTime value={host.lastTestAt} className="text-[11px] text-soft" />
                        </span>
                      ) : (
                        <span className="text-soft">Never</span>
                      )}
                    </Td>
                    {can.admin ? (
                      <Td align="right" className="pr-4">
                        <span className="flex items-center justify-end gap-1">
                          <Button
                            size="sm"
                            icon={<Plug className="size-3.5" />}
                            loading={test.isPending && test.variables === host.id}
                            onClick={() => onTest(host)}
                          >
                            Test
                          </Button>
                          <Button
                            size="sm"
                            icon={<PencilLine className="size-3.5" />}
                            onClick={() => openEdit(host)}
                          >
                            Edit
                          </Button>
                          <Button
                            size="sm"
                            variant="ghost"
                            icon={<Trash className="size-3.5" />}
                            disabled={inUse}
                            title={
                              inUse
                                ? `${host.sourceCount} ${host.sourceCount === 1 ? 'source runs' : 'sources run'} on this host`
                                : undefined
                            }
                            onClick={() => void onDelete(host)}
                          >
                            Delete
                          </Button>
                        </span>
                      </Td>
                    ) : null}
                  </Tr>
                )
              })}
            </tbody>
          </TableWrap>
        </div>
      )}

      <HostDialog
        host={editing}
        open={can.admin && (creating || Boolean(editing))}
        onClose={closeDialog}
      />
    </div>
  )
}
