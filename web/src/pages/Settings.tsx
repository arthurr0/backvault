import { useRef, useState, type ReactNode } from 'react'
import { Link, useSearchParams } from 'react-router'
import {
  CircleCheck,
  CircleX,
  Download,
  FileUp,
  KeyRound,
  Plus,
  Save,
  Trash,
  Upload,
} from 'lucide-react'
import { downloadUrl, errorMessage } from '@/api/client'
import {
  useAudit,
  useChangePassword,
  useCreateToken,
  useDeleteToken,
  useDeleteUser,
  useImportConfig,
  useMe,
  useSaveSettings,
  useSaveUser,
  useSettings,
  useTokens,
  useTools,
  useUsers,
  useVersion,
} from '@/api/hooks'
import { ALL_EVENTS, type Retention, type Settings, type TokenCreated } from '@/api/types'
import { Button } from '@/components/ui/Button'
import { Card } from '@/components/ui/Card'
import { useConfirm } from '@/components/ui/Confirm'
import { CodeBlock } from '@/components/ui/Copyable'
import { Dialog } from '@/components/ui/Dialog'
import { EmptyState } from '@/components/ui/EmptyState'
import { FieldShell } from '@/components/ui/Field'
import { Checkbox, Input, Select, Textarea } from '@/components/ui/Input'
import { PAGE_SIZE, Pagination } from '@/components/ui/Pagination'
import { SkeletonRows } from '@/components/ui/Skeleton'
import { StatusPill } from '@/components/ui/StatusPill'
import { TableWrap, Td, Th, Tr } from '@/components/ui/Table'
import { Tabs } from '@/components/ui/Tabs'
import { RelativeTime } from '@/components/ui/Time'
import { useToast } from '@/components/ui/Toast'
import { formatAbsolute } from '@/lib/format'
import { usePageMeta } from '@/lib/pageMeta'
import { useCan } from '@/lib/permissions'
import { describeRetention } from '@/lib/retention'

const TABS = [
  { id: 'general', label: 'General' },
  { id: 'users', label: 'Users', admin: true },
  { id: 'tokens', label: 'API tokens', admin: true },
  { id: 'audit', label: 'Audit log', admin: true },
  { id: 'tools', label: 'Tools' },
  { id: 'transfer', label: 'Import / Export', admin: true },
  { id: 'about', label: 'About' },
]

export function SettingsPage() {
  usePageMeta('Settings', [{ label: 'Backvault', to: '/' }, { label: 'Settings' }])
  const [params, setParams] = useSearchParams()
  const can = useCan()

  const items = TABS.filter((item) => can.admin || !item.admin).map(({ id, label }) => ({ id, label }))
  const requested = params.get('tab') ?? items[0].id
  const tab = items.some((item) => item.id === requested) ? requested : items[0].id

  return (
    <div className="space-y-4">
      <Tabs
        items={items}
        value={tab}
        onChange={(id) => {
          const next = new URLSearchParams(params)
          next.set('tab', id)
          setParams(next, { replace: true })
        }}
      />
      {tab === 'general' ? <GeneralTab canEdit={can.admin} /> : null}
      {tab === 'users' ? <UsersTab /> : null}
      {tab === 'tokens' ? <TokensTab /> : null}
      {tab === 'audit' ? <AuditTab /> : null}
      {tab === 'tools' ? <ToolsTab /> : null}
      {tab === 'transfer' ? <TransferTab /> : null}
      {tab === 'about' ? <AboutTab /> : null}
    </div>
  )
}

function GeneralTab({ canEdit }: { canEdit: boolean }) {
  const settings = useSettings()
  if (settings.isLoading || !settings.data) return <SkeletonRows rows={6} />
  return <GeneralForm key={settings.dataUpdatedAt} initial={settings.data} canEdit={canEdit} />
}

function GeneralForm({ initial, canEdit }: { initial: Settings; canEdit: boolean }) {
  const save = useSaveSettings()
  const toast = useToast()
  const [draft, setDraft] = useState<Settings>(initial)

  const patch = (next: Partial<Settings>) => setDraft({ ...draft, ...next })
  const patchRetention = (next: Partial<Retention>) =>
    setDraft({ ...draft, defaultRetention: { ...draft.defaultRetention, ...next } })

  return (
    <div className="space-y-4">
      <Card title="Instance" description="Names and addresses used in the panel and notifications">
        <div className="grid gap-4 sm:grid-cols-2">
          <FieldShell label="Site name" htmlFor="settings-site">
            <Input
              id="settings-site"
              disabled={!canEdit}
              value={draft.siteName}
              onChange={(event) => patch({ siteName: event.target.value })}
            />
          </FieldShell>
          <FieldShell
            label="Base URL"
            htmlFor="settings-base"
            help="Used for links in notifications and for secure cookies."
          >
            <Input
              id="settings-base"
              disabled={!canEdit}
              className="font-mono"
              value={draft.baseUrl}
              onChange={(event) => patch({ baseUrl: event.target.value })}
              placeholder="https://backvault.example.com"
            />
          </FieldShell>
          <FieldShell label="Default timezone" htmlFor="settings-tz">
            <Input
              id="settings-tz"
              disabled={!canEdit}
              className="font-mono"
              value={draft.defaultTimezone}
              onChange={(event) => patch({ defaultTimezone: event.target.value })}
            />
          </FieldShell>
          <FieldShell
            label="Max concurrent runs"
            htmlFor="settings-concurrency"
            help="How many runs may execute at the same time."
          >
            <Input
              id="settings-concurrency"
              disabled={!canEdit}
              type="number"
              min={1}
              className="font-mono"
              value={draft.maxConcurrentRuns}
              onChange={(event) => patch({ maxConcurrentRuns: Number(event.target.value) || 1 })}
            />
          </FieldShell>
          <FieldShell label="Run history (days)" htmlFor="settings-run-history">
            <Input
              id="settings-run-history"
              disabled={!canEdit}
              type="number"
              min={0}
              className="font-mono"
              value={draft.runHistoryDays}
              onChange={(event) => patch({ runHistoryDays: Number(event.target.value) || 0 })}
            />
          </FieldShell>
          <FieldShell label="Audit history (days)" htmlFor="settings-audit-history">
            <Input
              id="settings-audit-history"
              disabled={!canEdit}
              type="number"
              min={0}
              className="font-mono"
              value={draft.auditHistoryDays}
              onChange={(event) => patch({ auditHistoryDays: Number(event.target.value) || 0 })}
            />
          </FieldShell>
          <FieldShell
            label="Overdue check (minutes)"
            htmlFor="settings-overdue"
            help="How often Backvault looks for jobs that missed their window."
          >
            <Input
              id="settings-overdue"
              disabled={!canEdit}
              type="number"
              min={1}
              className="font-mono"
              value={draft.overdueCheckMinutes}
              onChange={(event) => patch({ overdueCheckMinutes: Number(event.target.value) || 1 })}
            />
          </FieldShell>
        </div>
      </Card>

      <Card title="Default retention" description="Applied to jobs that define no retention of their own">
        <div className="grid gap-4 sm:grid-cols-3 lg:grid-cols-4">
          {(
            [
              ['keepLast', 'Keep last'],
              ['keepHourly', 'Keep hourly'],
              ['keepDaily', 'Keep daily'],
              ['keepWeekly', 'Keep weekly'],
              ['keepMonthly', 'Keep monthly'],
              ['keepYearly', 'Keep yearly'],
              ['maxAgeDays', 'Max age (days)'],
            ] as Array<[keyof Retention, string]>
          ).map(([key, label]) => (
            <FieldShell key={key} label={label} htmlFor={`settings-${key}`}>
              <Input
                id={`settings-${key}`}
                disabled={!canEdit}
                type="number"
                min={0}
                className="font-mono"
                value={draft.defaultRetention[key]}
                onChange={(event) => patchRetention({ [key]: Number(event.target.value) || 0 })}
              />
            </FieldShell>
          ))}
        </div>
        <p className="mt-3 rounded-lg border border-border bg-surface-2 p-3 text-sm text-muted">
          {describeRetention(draft.defaultRetention)}
        </p>
      </Card>

      <Card title="Default notifications" description="Events sent when a job defines no filter">
        <div className="grid gap-1.5 sm:grid-cols-2 lg:grid-cols-4">
          {ALL_EVENTS.map((event) => (
            <label
              key={event}
              className="flex cursor-pointer items-center gap-2 rounded-lg border border-border px-2.5 py-2"
            >
              <Checkbox
                checked={draft.defaultNotifyOn.includes(event)}
                label={event}
                disabled={!canEdit}
                onChange={(next) =>
                  patch({
                    defaultNotifyOn: next
                      ? [...draft.defaultNotifyOn, event]
                      : draft.defaultNotifyOn.filter((item) => item !== event),
                  })
                }
              />
              <span className="font-mono text-[12px] text-text">{event}</span>
            </label>
          ))}
        </div>
      </Card>

      {canEdit ? (
        <div className="flex justify-end">
          <Button
            variant="primary"
            icon={<Save className="size-4" />}
            loading={save.isPending}
            onClick={() =>
              save.mutate(draft, {
                onSuccess: () => toast.success('Settings saved'),
                onError: (error) => toast.error('Settings could not be saved', errorMessage(error)),
              })
            }
          >
            Save settings
          </Button>
        </div>
      ) : (
        <p className="text-sm text-muted">
          These settings are read-only for your account. An administrator can change them.
        </p>
      )}
    </div>
  )
}

function UsersTab() {
  const users = useUsers()
  const me = useMe()
  const remove = useDeleteUser()
  const toast = useToast()
  const confirm = useConfirm()
  const [creating, setCreating] = useState(false)
  const [passwordFor, setPasswordFor] = useState<string | null>(null)

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-2">
        <p className="text-sm text-muted">Admins manage everything. Viewers can read and nothing else.</p>
        <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setCreating(true)}>
          New user
        </Button>
      </div>

      {users.isLoading ? (
        <SkeletonRows rows={4} />
      ) : (
        <Card bodyClassName="p-0">
          <TableWrap>
            <thead>
              <tr>
                <Th>Name</Th>
                <Th>Email</Th>
                <Th>Role</Th>
                <Th>Last login</Th>
                <Th align="right" />
              </tr>
            </thead>
            <tbody>
              {(users.data ?? []).map((user) => (
                <Tr key={user.id}>
                  <Td className="font-medium">{user.name}</Td>
                  <Td className="text-muted">{user.email}</Td>
                  <Td>
                    <StatusPill status={user.role === 'admin' ? 'ok' : 'pending'} label={user.role} size="sm" />
                  </Td>
                  <Td className="text-muted">
                    <RelativeTime value={user.lastLoginAt} />
                  </Td>
                  <Td align="right">
                    <div className="flex items-center justify-end gap-1.5">
                      <Button size="sm" onClick={() => setPasswordFor(user.id)}>
                        Password
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        disabled={user.id === me.data?.user.id}
                        icon={<Trash className="size-3.5" />}
                        onClick={async () => {
                          const ok = await confirm({
                            title: `Delete ${user.name}`,
                            description: 'The user loses access immediately.',
                            confirmLabel: 'Delete user',
                          })
                          if (!ok) return
                          remove.mutate(user.id, {
                            onSuccess: () => toast.success('User deleted', user.name),
                            onError: (error) =>
                              toast.error('Could not delete the user', errorMessage(error)),
                          })
                        }}
                      >
                        Delete
                      </Button>
                    </div>
                  </Td>
                </Tr>
              ))}
            </tbody>
          </TableWrap>
        </Card>
      )}

      {creating ? <UserDialog onClose={() => setCreating(false)} /> : null}
      {passwordFor ? (
        <PasswordDialog
          userId={passwordFor}
          isSelf={passwordFor === me.data?.user.id}
          onClose={() => setPasswordFor(null)}
        />
      ) : null}
    </div>
  )
}

function UserDialog({ onClose }: { onClose: () => void }) {
  const save = useSaveUser()
  const toast = useToast()
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [role, setRole] = useState('viewer')
  const [password, setPassword] = useState('')

  return (
    <Dialog
      open
      onClose={onClose}
      width="sm"
      title="New user"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button
            variant="primary"
            loading={save.isPending}
            disabled={!name || !email || !password}
            onClick={() =>
              save.mutate(
                { name, email, role, password },
                {
                  onSuccess: () => {
                    toast.success('User created', name)
                    onClose()
                  },
                  onError: (error) => toast.error('The user could not be created', errorMessage(error)),
                },
              )
            }
          >
            Create
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <FieldShell label="Name" htmlFor="user-name" required>
          <Input id="user-name" value={name} onChange={(event) => setName(event.target.value)} />
        </FieldShell>
        <FieldShell label="Email" htmlFor="user-email" required>
          <Input
            id="user-email"
            type="email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
          />
        </FieldShell>
        <FieldShell label="Role" htmlFor="user-role">
          <Select id="user-role" value={role} onChange={(event) => setRole(event.target.value)}>
            <option value="admin">Admin</option>
            <option value="viewer">Viewer</option>
          </Select>
        </FieldShell>
        <FieldShell label="Password" htmlFor="user-password" required>
          <Input
            id="user-password"
            type="password"
            autoComplete="new-password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
        </FieldShell>
      </div>
    </Dialog>
  )
}

function PasswordDialog({
  userId,
  isSelf,
  onClose,
}: {
  userId: string
  isSelf: boolean
  onClose: () => void
}) {
  const change = useChangePassword()
  const toast = useToast()
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')

  return (
    <Dialog
      open
      onClose={onClose}
      width="sm"
      title="Change password"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button
            variant="primary"
            loading={change.isPending}
            disabled={!next || (isSelf && !current)}
            onClick={() =>
              change.mutate(
                { id: userId, currentPassword: isSelf ? current : undefined, password: next },
                {
                  onSuccess: () => {
                    toast.success('Password changed')
                    onClose()
                  },
                  onError: (error) => toast.error('The password was not changed', errorMessage(error)),
                },
              )
            }
          >
            Change password
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        {isSelf ? (
          <FieldShell label="Current password" htmlFor="password-current" required>
            <Input
              id="password-current"
              type="password"
              autoComplete="current-password"
              value={current}
              onChange={(event) => setCurrent(event.target.value)}
            />
          </FieldShell>
        ) : null}
        <FieldShell label="New password" htmlFor="password-next" required>
          <Input
            id="password-next"
            type="password"
            autoComplete="new-password"
            value={next}
            onChange={(event) => setNext(event.target.value)}
          />
        </FieldShell>
      </div>
    </Dialog>
  )
}

function TokensTab() {
  const tokens = useTokens()
  const remove = useDeleteToken()
  const toast = useToast()
  const confirm = useConfirm()
  const [creating, setCreating] = useState(false)
  const [created, setCreated] = useState<TokenCreated | null>(null)

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-2">
        <p className="text-sm text-muted">
          Tokens authenticate the CLI, push scripts and monitoring. The secret is shown once.
        </p>
        <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setCreating(true)}>
          New token
        </Button>
      </div>

      {tokens.isLoading ? (
        <SkeletonRows rows={3} />
      ) : (tokens.data ?? []).length === 0 ? (
        <EmptyState
          icon={<KeyRound className="size-6" />}
          title="No tokens yet"
          description="Create a token with the ingest scope for push jobs, or read for monitoring."
        />
      ) : (
        <Card bodyClassName="p-0">
          <TableWrap>
            <thead>
              <tr>
                <Th>Name</Th>
                <Th>Prefix</Th>
                <Th>Scopes</Th>
                <Th>Jobs</Th>
                <Th>Last used</Th>
                <Th align="right" />
              </tr>
            </thead>
            <tbody>
              {(tokens.data ?? []).map((token) => (
                <Tr key={token.id}>
                  <Td className="font-medium">{token.name}</Td>
                  <Td className="font-mono text-muted">{token.prefix}</Td>
                  <Td>
                    <span className="flex flex-wrap gap-1">
                      {token.scopes.map((scope) => (
                        <span
                          key={scope}
                          className="rounded bg-surface-3 px-1.5 py-0.5 font-mono text-[10.5px] text-muted"
                        >
                          {scope}
                        </span>
                      ))}
                    </span>
                  </Td>
                  <Td className="text-muted">
                    {token.jobSlugs?.length ? token.jobSlugs.join(', ') : 'all'}
                  </Td>
                  <Td className="text-muted">
                    <RelativeTime value={token.lastUsedAt} />
                  </Td>
                  <Td align="right">
                    <Button
                      size="sm"
                      variant="ghost"
                      icon={<Trash className="size-3.5" />}
                      onClick={async () => {
                        const ok = await confirm({
                          title: `Revoke ${token.name}`,
                          description: 'Anything using this token stops working immediately.',
                          confirmLabel: 'Revoke token',
                        })
                        if (!ok) return
                        remove.mutate(token.id, {
                          onSuccess: () => toast.success('Token revoked', token.name),
                          onError: (error) => toast.error('Could not revoke the token', errorMessage(error)),
                        })
                      }}
                    >
                      Revoke
                    </Button>
                  </Td>
                </Tr>
              ))}
            </tbody>
          </TableWrap>
        </Card>
      )}

      {creating ? (
        <TokenDialog
          onClose={() => setCreating(false)}
          onCreated={(result) => {
            setCreating(false)
            setCreated(result)
          }}
        />
      ) : null}

      {created ? (
        <Dialog
          open
          onClose={() => setCreated(null)}
          width="lg"
          title="Token created"
          description="Copy the secret now. It is never shown again."
          footer={<Button variant="primary" onClick={() => setCreated(null)}>Done</Button>}
        >
          <div className="space-y-3">
            <CodeBlock code={created.secret} />
            <p className="text-sm text-muted">
              Use it as <span className="font-mono text-text">Authorization: Bearer {created.token.prefix}…</span>{' '}
              or as <span className="font-mono text-text">BACKVAULT_TOKEN</span> for the CLI and scripts.
            </p>
          </div>
        </Dialog>
      ) : null}
    </div>
  )
}

function TokenDialog({
  onClose,
  onCreated,
}: {
  onClose: () => void
  onCreated: (result: TokenCreated) => void
}) {
  const create = useCreateToken()
  const toast = useToast()
  const [name, setName] = useState('')
  const [scopes, setScopes] = useState<string[]>(['read'])
  const [jobSlugs, setJobSlugs] = useState('')

  return (
    <Dialog
      open
      onClose={onClose}
      width="md"
      title="New API token"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button
            variant="primary"
            disabled={!name || scopes.length === 0}
            loading={create.isPending}
            onClick={() =>
              create.mutate(
                {
                  name,
                  scopes,
                  jobSlugs: jobSlugs
                    .split(/[\s,]+/)
                    .map((item) => item.trim())
                    .filter(Boolean),
                },
                {
                  onSuccess: onCreated,
                  onError: (error) => toast.error('The token could not be created', errorMessage(error)),
                },
              )
            }
          >
            Create token
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <FieldShell label="Name" htmlFor="token-name" required>
          <Input
            id="token-name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="db01 push agent"
          />
        </FieldShell>
        <FieldShell label="Scopes" htmlFor="token-scopes" required>
          <div className="grid grid-cols-2 gap-1.5">
            {['admin', 'read', 'run', 'ingest'].map((scope) => (
              <label
                key={scope}
                className="flex cursor-pointer items-center gap-2 rounded-lg border border-border px-2.5 py-2"
              >
                <Checkbox
                  checked={scopes.includes(scope)}
                  label={scope}
                  onChange={(next) =>
                    setScopes(next ? [...scopes, scope] : scopes.filter((item) => item !== scope))
                  }
                />
                <span className="font-mono text-[12.5px] text-text">{scope}</span>
              </label>
            ))}
          </div>
        </FieldShell>
        <FieldShell
          label="Restrict to job slugs"
          htmlFor="token-jobs"
          help="Optional. Space or comma separated. Leave empty to allow every job."
        >
          <Input
            id="token-jobs"
            className="font-mono"
            value={jobSlugs}
            onChange={(event) => setJobSlugs(event.target.value)}
            placeholder="db01-postgres db01-files"
          />
        </FieldShell>
      </div>
    </Dialog>
  )
}

function AuditTab() {
  const [action, setAction] = useState('')
  const [offset, setOffset] = useState(0)
  const audit = useAudit({
    action: action || undefined,
    limit: PAGE_SIZE,
    offset: offset || undefined,
  })
  const items = audit.data?.items ?? []

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          aria-label="Filter by action"
          className="w-56"
          placeholder="Filter by action, job.update"
          value={action}
          onChange={(event) => {
            setAction(event.target.value)
            setOffset(0)
          }}
        />
      </div>
      {audit.isLoading ? (
        <SkeletonRows rows={6} />
      ) : items.length === 0 ? (
        <EmptyState title="No audit entries" description="Administrative changes are recorded here." />
      ) : (
        <Card bodyClassName="p-0">
          <TableWrap>
            <thead>
              <tr>
                <Th>Time</Th>
                <Th>Actor</Th>
                <Th>Action</Th>
                <Th>Object</Th>
                <Th>IP</Th>
              </tr>
            </thead>
            <tbody>
              {items.map((entry) => (
                <Tr key={entry.id}>
                  <Td className="text-muted" title={formatAbsolute(entry.time)}>
                    <RelativeTime value={entry.time} />
                  </Td>
                  <Td>{entry.actorLabel}</Td>
                  <Td className="font-mono text-[12.5px] text-text">{entry.action}</Td>
                  <Td className="text-muted">
                    {entry.objectType}
                    {entry.objectName ? ` · ${entry.objectName}` : ''}
                  </Td>
                  <Td className="font-mono text-muted">{entry.ip ?? '-'}</Td>
                </Tr>
              ))}
            </tbody>
          </TableWrap>
        </Card>
      )}

      {audit.data ? (
        <Pagination
          offset={offset}
          limit={PAGE_SIZE}
          total={audit.data.total}
          count={items.length}
          noun="entries"
          onOffsetChange={setOffset}
        />
      ) : null}
    </div>
  )
}

function ToolsTab() {
  const tools = useTools()
  return (
    <div className="space-y-4">
      <p className="text-sm text-muted">
        External binaries Backvault shells out to. Missing tools only affect the drivers that need them.
      </p>
      {tools.isLoading ? (
        <SkeletonRows rows={5} />
      ) : (
        <Card bodyClassName="p-0">
          <TableWrap>
            <thead>
              <tr>
                <Th>Tool</Th>
                <Th>Status</Th>
                <Th>Path</Th>
                <Th>Version</Th>
                <Th>Used by</Th>
              </tr>
            </thead>
            <tbody>
              {(tools.data ?? []).map((tool) => (
                <Tr key={tool.name}>
                  <Td className="font-mono text-[12.5px]">{tool.name}</Td>
                  <Td>
                    <span
                      className={`inline-flex items-center gap-1.5 text-xs ${
                        tool.available ? 'text-success' : 'text-warning'
                      }`}
                    >
                      {tool.available ? (
                        <CircleCheck className="size-3.5" aria-hidden="true" />
                      ) : (
                        <CircleX className="size-3.5" aria-hidden="true" />
                      )}
                      {tool.available ? 'available' : 'missing'}
                    </span>
                  </Td>
                  <Td className="font-mono text-[12px] text-muted">{tool.path ?? '-'}</Td>
                  <Td className="text-muted">{tool.version ?? '-'}</Td>
                  <Td className="text-muted">
                    {tool.usedBy?.join(', ') || '-'}
                    {!tool.available ? (
                      <span className="mt-0.5 block text-[11.5px] text-warning">
                        Install it on the Backvault host to use{' '}
                        {(tool.usedBy?.length ?? 0) > 1 ? 'those drivers' : 'that driver'}.
                      </span>
                    ) : null}
                  </Td>
                </Tr>
              ))}
            </tbody>
          </TableWrap>
        </Card>
      )}
    </div>
  )
}

function TransferTab() {
  const [yaml, setYaml] = useState('')
  const [fileName, setFileName] = useState('')
  const [includeSecrets, setIncludeSecrets] = useState(false)
  const fileInput = useRef<HTMLInputElement>(null)
  const importConfig = useImportConfig()
  const toast = useToast()

  const pickFile = async (file: File | undefined) => {
    if (!file) return
    try {
      const text = await file.text()
      setYaml(text)
      setFileName(file.name)
    } catch (error) {
      toast.error('The file could not be read', errorMessage(error))
    }
  }

  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <Card title="Export" description="Sources, destinations, jobs and channels as YAML">
        <label className="mb-3 flex cursor-pointer items-center gap-2 text-sm text-text">
          <Checkbox checked={includeSecrets} label="Include secrets" onChange={setIncludeSecrets} />
          Include secrets (audited)
        </label>
        <a
          href={downloadUrl('/export', includeSecrets ? { includeSecrets: 1 } : undefined)}
          download="backvault-config.yaml"
        >
          <Button variant="primary" icon={<Download className="size-4" />}>
            Download export
          </Button>
        </a>
        <p className="mt-3 text-xs text-muted">
          Without the checkbox every secret is replaced by {'********'} and cannot be imported back as is.
        </p>
      </Card>

      <Card title="Import" description="Upsert objects by slug or name">
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <input
            ref={fileInput}
            type="file"
            accept=".yaml,.yml,application/yaml,text/yaml"
            className="hidden"
            onChange={(event) => {
              const file = event.target.files?.[0]
              event.target.value = ''
              void pickFile(file)
            }}
          />
          <Button icon={<FileUp className="size-4" />} onClick={() => fileInput.current?.click()}>
            Choose a YAML file
          </Button>
          <span className="truncate text-xs text-muted">
            {fileName ? fileName : 'No file chosen, paste below instead'}
          </span>
        </div>
        <Textarea
          aria-label="Configuration YAML"
          rows={10}
          className="font-mono text-[12.5px]"
          placeholder="Paste a Backvault export here"
          value={yaml}
          onChange={(event) => {
            setYaml(event.target.value)
            setFileName('')
          }}
        />
        <div className="mt-3 flex gap-2">
          <Button
            icon={<Upload className="size-4" />}
            disabled={!yaml.trim()}
            loading={importConfig.isPending}
            onClick={() =>
              importConfig.mutate(
                { yaml, dryRun: true },
                {
                  onSuccess: (result) =>
                    toast.info('Dry run finished', `${result.total} objects would change`),
                  onError: (error) => toast.error('The import failed', errorMessage(error)),
                },
              )
            }
          >
            Dry run
          </Button>
          <Button
            variant="primary"
            disabled={!yaml.trim()}
            loading={importConfig.isPending}
            onClick={() =>
              importConfig.mutate(
                { yaml, dryRun: false },
                {
                  onSuccess: (result) => toast.success('Import applied', `${result.total} objects`),
                  onError: (error) => toast.error('The import failed', errorMessage(error)),
                },
              )
            }
          >
            Apply import
          </Button>
        </div>
        {importConfig.data ? (
          <ul className="mt-3 space-y-1 text-xs">
            {importConfig.data.changes.map((entry, index) => (
              <li key={`${entry.kind}-${entry.name}-${index}`} className="flex justify-between gap-2">
                <span className="text-text">
                  {entry.kind} · {entry.name}
                </span>
                <span className="font-mono text-muted">{entry.action}</span>
              </li>
            ))}
          </ul>
        ) : null}
      </Card>
    </div>
  )
}

function AboutTab() {
  const version = useVersion()
  const me = useMe()
  const [changingPassword, setChangingPassword] = useState(false)
  const userId = me.data?.user?.id

  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <Card title="Backvault" description="Every backup, accounted for.">
        <dl className="space-y-2 text-sm">
          <Row label="Version">{version.data?.version ?? '-'}</Row>
          <Row label="Commit">{version.data?.commit ?? '-'}</Row>
          <Row label="Built">{version.data?.buildDate ?? '-'}</Row>
          <Row label="Go">{version.data?.goVersion ?? '-'}</Row>
          <Row label="Started">
            {version.data?.startedAt ? <RelativeTime value={version.data.startedAt} /> : '-'}
          </Row>
          <Row label="Signed in as">
            {me.data ? `${me.data.user.name} (${me.data.user.role})` : '-'}
          </Row>
        </dl>
        {userId ? (
          <div className="mt-4 border-t border-border pt-3">
            <Button icon={<KeyRound className="size-4" />} onClick={() => setChangingPassword(true)}>
              Change password
            </Button>
          </div>
        ) : null}
      </Card>
      <Card title="Links" description="Where to look next">
        <ul className="space-y-2 text-sm">
          <li>
            <a className="text-accent hover:underline" href="/api/v1/healthz">
              Health endpoint
            </a>
          </li>
          <li>
            <a className="text-accent hover:underline" href="/metrics">
              Prometheus metrics
            </a>
          </li>
          <li>
            <Link className="text-accent hover:underline" to="/docs">
              Documentation
            </Link>
            <span className="text-muted">
              {' '}
              covers every driver, restore and the push API, and ships inside this build
            </span>
          </li>
          <li>
            <a
              className="text-accent hover:underline"
              href="https://github.com/arthurr0/backvault"
              target="_blank"
              rel="noopener noreferrer"
            >
              Source on GitHub
            </a>
          </li>
        </ul>
      </Card>
      {changingPassword && userId ? (
        <PasswordDialog userId={userId} isSelf onClose={() => setChangingPassword(false)} />
      ) : null}
    </div>
  )
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-3">
      <dt className="text-muted">{label}</dt>
      <dd className="text-right font-mono text-[12.5px] text-text">{children}</dd>
    </div>
  )
}
