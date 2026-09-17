import { useMemo, useState } from 'react'
import { Bell, Plus, Send, Trash, TriangleAlert } from 'lucide-react'
import { errorMessage, fieldErrors } from '@/api/client'
import {
  useChannels,
  useDeleteChannel,
  useNotifierSpecs,
  useSaveChannel,
  useSettings,
  useTestChannel,
} from '@/api/hooks'
import { ALL_EVENTS, type Config, type DriverSpec, type NotificationChannel } from '@/api/types'
import { DriverPicker } from '@/components/form/DriverPicker'
import {
  DynamicForm,
  defaultsFor,
  maskStoredSecrets,
  normalizeConfig,
} from '@/components/form/DynamicForm'
import { Button } from '@/components/ui/Button'
import { Card } from '@/components/ui/Card'
import { useConfirm } from '@/components/ui/Confirm'
import { Dialog } from '@/components/ui/Dialog'
import { EmptyState } from '@/components/ui/EmptyState'
import { FieldShell } from '@/components/ui/Field'
import { Checkbox, Input, Switch } from '@/components/ui/Input'
import { SkeletonRows } from '@/components/ui/Skeleton'
import { StatusPill } from '@/components/ui/StatusPill'
import { TableWrap, Td, Th, Tr } from '@/components/ui/Table'
import { RelativeTime } from '@/components/ui/Time'
import { useToast } from '@/components/ui/Toast'
import { DriverIcon } from '@/lib/icons'
import { usePageMeta } from '@/lib/pageMeta'
import { useCan } from '@/lib/permissions'

export function NotificationsPage() {
  usePageMeta('Notifications', [{ label: 'Backvault', to: '/' }, { label: 'Notifications' }])
  const channels = useChannels()
  const specs = useNotifierSpecs()
  const settings = useSettings()
  const remove = useDeleteChannel()
  const test = useTestChannel()
  const toast = useToast()
  const confirm = useConfirm()
  const can = useCan()
  const [editing, setEditing] = useState<NotificationChannel | null>(null)
  const [creating, setCreating] = useState(false)

  const onDelete = async (channel: NotificationChannel) => {
    const ok = await confirm({
      title: `Delete ${channel.name}`,
      description: 'Jobs that reference this channel stop notifying through it.',
      confirmLabel: 'Delete channel',
    })
    if (!ok) return
    remove.mutate(channel.id, {
      onSuccess: () => toast.success('Channel deleted', channel.name),
      onError: (error) => toast.error('Could not delete the channel', errorMessage(error)),
    })
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm text-muted">
          Channels receive run results. Jobs pick the channels they use, and the events below filter
          further.
        </p>
        {can.admin ? (
          <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setCreating(true)}>
            New channel
          </Button>
        ) : null}
      </div>

      {channels.isLoading ? (
        <SkeletonRows rows={4} />
      ) : channels.isError ? (
        <EmptyState
          icon={<TriangleAlert className="size-6" />}
          title="Channels could not be loaded"
          description={errorMessage(channels.error)}
        />
      ) : (channels.data ?? []).length === 0 ? (
        <EmptyState
          icon={<Bell className="size-6" />}
          title="No channels yet"
          description="Add email, a webhook, Slack, Discord, Telegram or ntfy so failures do not go unnoticed."
          action={
            can.admin ? (
              <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setCreating(true)}>
                Add a channel
              </Button>
            ) : null
          }
        />
      ) : (
        <Card bodyClassName="p-0">
          <TableWrap>
            <thead>
              <tr>
                <Th>Channel</Th>
                <Th>State</Th>
                <Th>Events</Th>
                <Th>Last sent</Th>
                {can.admin ? <Th align="right" /> : null}
              </tr>
            </thead>
            <tbody>
              {(channels.data ?? []).map((channel) => {
                const spec = (specs.data ?? []).find((item) => item.kind === channel.kind)
                return (
                  <Tr key={channel.id}>
                    <Td>
                      <span className="flex items-center gap-2.5">
                        <DriverIcon icon={spec?.icon} kind={channel.kind} className="size-4 text-accent" />
                        <span className="min-w-0">
                          <span className="block truncate font-medium">{channel.name}</span>
                          <span className="text-[11px] text-soft">{spec?.label ?? channel.kind}</span>
                        </span>
                      </span>
                    </Td>
                    <Td>
                      <StatusPill status={channel.enabled ? 'enabled' : 'disabled'} size="sm" />
                      {channel.lastError ? (
                        <span className="mt-1 block max-w-[14rem] truncate text-[11px] text-danger">
                          {channel.lastError}
                        </span>
                      ) : null}
                    </Td>
                    <Td className="max-w-[18rem]">
                      {channel.events?.length ? (
                        <span className="flex flex-wrap gap-1">
                          {channel.events.map((event) => (
                            <span
                              key={event}
                              className="rounded bg-surface-3 px-1.5 py-0.5 font-mono text-[10.5px] text-muted"
                            >
                              {event}
                            </span>
                          ))}
                        </span>
                      ) : (
                        <span className="text-xs text-muted">
                          defaults ({(settings.data?.defaultNotifyOn ?? []).length || 'all'})
                        </span>
                      )}
                    </Td>
                    <Td className="text-muted">
                      <RelativeTime value={channel.lastSentAt} />
                    </Td>
                    {can.admin ? (
                      <Td align="right">
                        <div className="flex items-center justify-end gap-1.5">
                          <Button size="sm" onClick={() => setEditing(channel)}>
                            Edit
                          </Button>
                          <Button
                            size="sm"
                            icon={<Send className="size-3.5" />}
                            loading={test.isPending && test.variables?.id === channel.id}
                            onClick={() =>
                              test.mutate(
                                { id: channel.id, kind: channel.kind, config: channel.config },
                                {
                                  onSuccess: (result) =>
                                    result.ok
                                      ? toast.success('Test sent', channel.name)
                                      : toast.error('The test failed', result.message),
                                  onError: (error) =>
                                    toast.error('The test could not be sent', errorMessage(error)),
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
                            onClick={() => void onDelete(channel)}
                          >
                            Delete
                          </Button>
                        </div>
                      </Td>
                    ) : null}
                  </Tr>
                )
              })}
            </tbody>
          </TableWrap>
        </Card>
      )}

      <Card title="Events" description="What Backvault can tell you about">
        <ul className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
          {ALL_EVENTS.map((event) => (
            <li key={event} className="rounded-lg border border-border bg-surface-2 p-2.5">
              <p className="font-mono text-[12px] text-text">{event}</p>
              <p className="mt-0.5 text-[11.5px] text-muted">{EVENT_HELP[event]}</p>
            </li>
          ))}
        </ul>
      </Card>

      {can.admin && (creating || editing) ? (
        <ChannelDialog
          key={editing?.id ?? 'new'}
          specs={specs.data ?? []}
          channel={editing}
          onClose={() => {
            setEditing(null)
            setCreating(false)
          }}
        />
      ) : null}
    </div>
  )
}

const EVENT_HELP: Record<string, string> = {
  'run.success': 'A backup run finished without problems.',
  'run.failed': 'A run failed and no artifact was stored.',
  'run.warning': 'A run finished with partial success.',
  'job.overdue': 'A job did not run inside its expected window.',
  'artifact.missing': 'A verification could not find a stored artifact.',
  'prune.done': 'Retention removed artifacts.',
  'restore.done': 'A restore run finished.',
  'restore.failed': 'A restore run failed.',
}

function ChannelDialog({
  specs,
  channel,
  onClose,
}: {
  specs: DriverSpec[]
  channel: NotificationChannel | null
  onClose: () => void
}) {
  const isEdit = Boolean(channel?.id)
  const save = useSaveChannel(channel?.id)
  const test = useTestChannel()
  const toast = useToast()

  const [kind, setKind] = useState(channel?.kind ?? '')
  const [name, setName] = useState(channel?.name ?? '')
  const [enabled, setEnabled] = useState(channel?.enabled ?? true)
  const [events, setEvents] = useState<string[]>(channel?.events ?? [])
  const [config, setConfig] = useState<Config>(() => {
    if (!channel) return {}
    const spec = specs.find((item) => item.kind === channel.kind)
    if (!spec) return { ...channel.config }
    return maskStoredSecrets(spec.fields, { ...defaultsFor(spec.fields), ...channel.config })
  })

  const spec = useMemo(() => specs.find((item) => item.kind === kind), [specs, kind])
  const errors = fieldErrors(save.error)

  const pick = (next: string) => {
    setKind(next)
    const nextSpec = specs.find((item) => item.kind === next)
    setConfig(nextSpec ? defaultsFor(nextSpec.fields) : {})
    if (!name && nextSpec) setName(nextSpec.label)
  }

  const submit = () => {
    if (!spec) return
    save.mutate(
      {
        name: name.trim(),
        kind: spec.kind,
        enabled,
        events,
        config: normalizeConfig(spec.fields, config),
      },
      {
        onSuccess: (saved) => {
          toast.success(isEdit ? 'Channel saved' : 'Channel created', saved.name)
          onClose()
        },
        onError: (error) => toast.error('The channel could not be saved', errorMessage(error)),
      },
    )
  }

  return (
    <Dialog
      open
      onClose={onClose}
      width="xl"
      title={isEdit ? 'Edit channel' : 'New channel'}
      description="Notifications are sent per event, filtered again by each job."
      footer={
        <>
          {spec ? (
            <Button
              className="mr-auto"
              icon={<Send className="size-4" />}
              loading={test.isPending}
              onClick={() =>
                test.mutate(
                  { id: channel?.id, kind: spec.kind, config: normalizeConfig(spec.fields, config) },
                  {
                    onSuccess: (result) =>
                      result.ok
                        ? toast.success('Test sent')
                        : toast.error('The test failed', result.message),
                    onError: (error) => toast.error('The test could not be sent', errorMessage(error)),
                  },
                )
              }
            >
              Send test
            </Button>
          ) : null}
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" disabled={!spec || !name.trim()} loading={save.isPending} onClick={submit}>
            {isEdit ? 'Save' : 'Create'}
          </Button>
        </>
      }
    >
      <div className="space-y-5">
        {!isEdit ? <DriverPicker specs={specs} value={kind} onChange={pick} /> : null}

        {spec ? (
          <>
            <div className="grid gap-4 sm:grid-cols-2">
              <FieldShell label="Name" htmlFor="channel-name" required error={errors.name}>
                <Input
                  id="channel-name"
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  placeholder={spec.label}
                />
              </FieldShell>
              <FieldShell label="Enabled" htmlFor="channel-enabled">
                <div className="flex h-9 items-center gap-2.5">
                  <Switch id="channel-enabled" label="Enabled" checked={enabled} onChange={setEnabled} />
                  <span className="text-sm text-muted">{enabled ? 'Sending' : 'Muted'}</span>
                </div>
              </FieldShell>
            </div>

            <div className="border-t border-border pt-4">
              <DynamicForm
                fields={spec.fields}
                value={config}
                onChange={setConfig}
                errors={errors}
                storedSecrets={isEdit}
              />
            </div>

            <div className="border-t border-border pt-4">
              <p className="mb-1 text-[13px] font-medium text-text">Events</p>
              <p className="mb-2 text-xs text-muted">
                Leave everything unchecked to accept every event a job sends.
              </p>
              <div className="grid gap-1.5 sm:grid-cols-2 lg:grid-cols-4">
                {ALL_EVENTS.map((event) => (
                  <label
                    key={event}
                    className="flex cursor-pointer items-center gap-2 rounded-lg border border-border px-2.5 py-2"
                  >
                    <Checkbox
                      checked={events.includes(event)}
                      label={event}
                      onChange={(next) =>
                        setEvents(next ? [...events, event] : events.filter((item) => item !== event))
                      }
                    />
                    <span className="font-mono text-[12px] text-text">{event}</span>
                  </label>
                ))}
              </div>
            </div>
          </>
        ) : (
          <p className="text-sm text-muted">Pick a notifier to continue.</p>
        )}
      </div>
    </Dialog>
  )
}
