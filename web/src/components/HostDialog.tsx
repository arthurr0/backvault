import { useEffect, useMemo, useRef, useState } from 'react'
import { ChevronRight, CircleCheck, CircleX, KeyRound, Plug, RotateCcw, TriangleAlert } from 'lucide-react'
import { errorMessage, fieldErrors } from '@/api/client'
import {
  useHostKeygen,
  useSaveHost,
  useTestUnsavedHost,
  useUnsavedHostKeygen,
} from '@/api/hooks'
import { SECRET_MASK, type Host, type HostAuth, type HostInput, type HostTestResult } from '@/api/types'
import { SecretInput } from '@/components/form/SecretInput'
import { TagInput } from '@/components/form/TagInput'
import { Button } from '@/components/ui/Button'
import { useConfirm } from '@/components/ui/Confirm'
import { CodeBlock, CopyButton } from '@/components/ui/Copyable'
import { Dialog } from '@/components/ui/Dialog'
import { FieldShell } from '@/components/ui/Field'
import { Input, Switch, Textarea } from '@/components/ui/Input'
import { useToast } from '@/components/ui/Toast'
import { formatDuration } from '@/lib/format'
import { cn } from '@/lib/utils'

const NOTABLE_TOOLS = ['tar', 'docker']

export function HostDialog({
  host,
  open,
  onClose,
}: {
  host: Host | null
  open: boolean
  onClose: () => void
}) {
  if (!open) return null
  return <HostForm key={host?.id ?? 'new'} host={host} onClose={onClose} />
}

interface Draft {
  name: string
  description: string
  address: string
  port: string
  user: string
  auth: HostAuth
  privateKey: string
  keyPassphrase: string
  password: string
  publicKey: string
  hostKey: string
  sudo: boolean
  connectTimeout: string
  tags: string[]
}

function draftOf(host: Host | null): Draft {
  return {
    name: host?.name ?? '',
    description: host?.description ?? '',
    address: host?.address ?? '',
    port: String(host?.port ?? 22),
    user: host?.user ?? 'root',
    auth: host?.auth ?? 'key',
    privateKey: host?.privateKey ?? '',
    keyPassphrase: host?.keyPassphrase ?? '',
    password: host?.password ?? '',
    publicKey: host?.publicKey ?? '',
    hostKey: host?.hostKey ?? '',
    sudo: host?.sudo ?? false,
    connectTimeout: String(host?.connectTimeoutSeconds ?? 15),
    tags: host?.tags ?? [],
  }
}

function authorizedKeysSnippet(publicKey: string): string {
  return `mkdir -p ~/.ssh && echo '${publicKey}' >> ~/.ssh/authorized_keys && chmod 700 ~/.ssh && chmod 600 ~/.ssh/authorized_keys`
}

function HostForm({ host, onClose }: { host: Host | null; onClose: () => void }) {
  const isEdit = Boolean(host?.id)
  const toast = useToast()
  const confirm = useConfirm()
  const save = useSaveHost(host?.id)
  const test = useTestUnsavedHost()
  const keygen = useUnsavedHostKeygen()
  const keygenSaved = useHostKeygen()

  const [draft, setDraft] = useState<Draft>(() => draftOf(host))
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const resultRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (test.data) resultRef.current?.scrollIntoView({ block: 'nearest' })
  }, [test.data])

  const patch = (next: Partial<Draft>) => setDraft((current) => ({ ...current, ...next }))
  const errors = fieldErrors(save.error)
  const keyStored = draft.privateKey === SECRET_MASK
  const generating = keygen.isPending || keygenSaved.isPending

  const input = useMemo<HostInput>(
    () => ({
      id: host?.id ?? '',
      name: draft.name.trim(),
      description: draft.description.trim(),
      address: draft.address.trim(),
      port: Number(draft.port) || 22,
      user: draft.user.trim(),
      auth: draft.auth,
      privateKey: draft.auth === 'key' ? draft.privateKey : '',
      keyPassphrase: draft.auth === 'key' ? draft.keyPassphrase : '',
      password: draft.auth === 'password' ? draft.password : '',
      publicKey: draft.publicKey,
      hostKey: draft.hostKey.trim(),
      sudo: draft.sudo,
      connectTimeoutSeconds: Number(draft.connectTimeout) || 0,
      tags: draft.tags,
    }),
    [draft, host?.id],
  )

  const generateKey = async () => {
    if (isEdit) {
      const ok = await confirm({
        title: 'Generate a new key pair',
        description:
          'The key stored for this host is replaced immediately. Add the new public key to authorized_keys before the next run.',
        confirmLabel: 'Generate key',
        tone: 'primary',
      })
      if (!ok) return
      keygenSaved.mutate(host?.id ?? '', {
        onSuccess: (saved) => {
          patch({ auth: 'key', privateKey: saved.privateKey, publicKey: saved.publicKey })
          toast.success('Key generated', 'Add the public key to authorized_keys on the host.')
        },
        onError: (error) => toast.error('The key could not be generated', errorMessage(error)),
      })
      return
    }
    if (draft.privateKey.trim()) {
      const ok = await confirm({
        title: 'Replace the private key',
        description: 'The key in the form is replaced by a freshly generated one.',
        confirmLabel: 'Generate key',
        tone: 'primary',
      })
      if (!ok) return
    }
    keygen.mutate(undefined, {
      onSuccess: (pair) => {
        patch({ auth: 'key', privateKey: pair.privateKey, publicKey: pair.publicKey })
        toast.success('Key generated', 'Add the public key to authorized_keys on the host.')
      },
      onError: (error) => toast.error('The key could not be generated', errorMessage(error)),
    })
  }

  const submit = () => {
    save.mutate(input, {
      onSuccess: (saved) => {
        toast.success(isEdit ? 'Saved' : 'Host created', saved.name)
        onClose()
      },
      onError: (error) => toast.error('The host could not be saved', errorMessage(error)),
    })
  }

  return (
    <Dialog
      open
      onClose={onClose}
      width="xl"
      title={isEdit ? 'Edit host' : 'New host'}
      description="A host is an SSH connection that sources can run on."
      footer={
        <>
          <Button
            className="mr-auto"
            icon={<Plug className="size-4" />}
            loading={test.isPending}
            onClick={() =>
              test.mutate(input, {
                onError: (error) => toast.error('The test could not be run', errorMessage(error)),
              })
            }
          >
            Test connection
          </Button>
          <Button onClick={onClose}>Cancel</Button>
          <Button
            variant="primary"
            disabled={!draft.name.trim() || !draft.address.trim()}
            loading={save.isPending}
            onClick={submit}
          >
            {isEdit ? 'Save' : 'Create'}
          </Button>
        </>
      }
    >
      <div className="space-y-5">
        <div className="grid gap-4 sm:grid-cols-2">
          <FieldShell label="Name" htmlFor="host-name" required error={errors.name}>
            <Input
              id="host-name"
              value={draft.name}
              placeholder="web01"
              onChange={(event) => patch({ name: event.target.value })}
            />
          </FieldShell>
          <FieldShell label="Tags" htmlFor="host-tags">
            <TagInput
              id="host-tags"
              value={draft.tags}
              onChange={(tags) => patch({ tags })}
              placeholder="production"
            />
          </FieldShell>
          <FieldShell label="Description" htmlFor="host-description" className="sm:col-span-2">
            <Textarea
              id="host-description"
              rows={2}
              value={draft.description}
              onChange={(event) => patch({ description: event.target.value })}
            />
          </FieldShell>
        </div>

        <div className="grid gap-4 border-t border-border pt-4 sm:grid-cols-4">
          <FieldShell
            label="Address"
            htmlFor="host-address"
            required
            error={errors.address}
            className="sm:col-span-2"
          >
            <Input
              id="host-address"
              spellCheck={false}
              placeholder="10.0.0.12 or web01.example.com"
              value={draft.address}
              onChange={(event) => patch({ address: event.target.value })}
            />
          </FieldShell>
          <FieldShell label="Port" htmlFor="host-port" error={errors.port}>
            <Input
              id="host-port"
              type="number"
              inputMode="numeric"
              min={1}
              max={65535}
              className="font-mono"
              value={draft.port}
              onChange={(event) => patch({ port: event.target.value })}
            />
          </FieldShell>
          <FieldShell label="User" htmlFor="host-user" required error={errors.user}>
            <Input
              id="host-user"
              spellCheck={false}
              value={draft.user}
              onChange={(event) => patch({ user: event.target.value })}
            />
          </FieldShell>
        </div>

        <div className="border-t border-border pt-4">
          <div role="radiogroup" aria-label="Authentication" className="flex flex-wrap items-center gap-2">
            <span className="mr-1 text-[13px] font-medium text-text">Authentication</span>
            {(
              [
                ['key', 'Key'],
                ['password', 'Password'],
              ] as Array<[HostAuth, string]>
            ).map(([value, label]) => (
              <button
                key={value}
                type="button"
                role="radio"
                aria-checked={draft.auth === value}
                onClick={() => patch({ auth: value })}
                className={cn(
                  'rounded-lg border px-3 py-1.5 text-[13px] transition-colors',
                  draft.auth === value
                    ? 'border-accent bg-accent-soft text-text'
                    : 'border-border bg-surface text-muted hover:border-border-strong hover:text-text',
                )}
              >
                {label}
              </button>
            ))}
          </div>

          {draft.auth === 'key' ? (
            <div className="mt-4 space-y-4">
              <div className="flex flex-wrap items-center gap-3">
                <Button
                  icon={<KeyRound className="size-4" />}
                  loading={generating}
                  onClick={() => void generateKey()}
                >
                  Generate key
                </Button>
                <p className="text-xs text-muted">
                  Backvault creates an ed25519 key pair, or you can paste your own private key below.
                </p>
              </div>

              <FieldShell
                label="Private key"
                htmlFor="host-private-key"
                error={errors.privateKey}
                help={keyStored ? undefined : 'OpenSSH or PEM format. It is stored encrypted.'}
              >
                {keyStored ? (
                  <div className="flex items-center gap-2">
                    <Input
                      id="host-private-key"
                      readOnly
                      value={SECRET_MASK}
                      className="font-mono"
                      aria-label="Stored private key"
                    />
                    <Button
                      size="sm"
                      icon={<RotateCcw className="size-3.5" />}
                      onClick={() => patch({ privateKey: '' })}
                    >
                      Replace
                    </Button>
                  </div>
                ) : (
                  <Textarea
                    id="host-private-key"
                    rows={5}
                    spellCheck={false}
                    className="font-mono text-[12px]"
                    placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                    value={draft.privateKey}
                    onChange={(event) => patch({ privateKey: event.target.value })}
                  />
                )}
              </FieldShell>

              {draft.publicKey ? (
                <div className="space-y-2 rounded-lg border border-border bg-surface-2 p-3">
                  <div className="flex items-start gap-2">
                    <p
                      className="scrollbar-thin min-w-0 flex-1 overflow-x-auto whitespace-pre font-mono text-[12px] text-text"
                      aria-label="Public key"
                    >
                      {draft.publicKey}
                    </p>
                    <CopyButton value={draft.publicKey} label="Copy the public key" />
                  </div>
                  <p className="text-xs text-muted">
                    Run this on the host as {draft.user.trim() || 'the target user'} to allow the key.
                  </p>
                  <CodeBlock code={authorizedKeysSnippet(draft.publicKey)} />
                </div>
              ) : null}

              <FieldShell
                label="Key passphrase"
                htmlFor="host-key-passphrase"
                error={errors.keyPassphrase}
                help="Leave empty when the key has no passphrase."
              >
                <SecretInput
                  id="host-key-passphrase"
                  storedMask={isEdit}
                  value={draft.keyPassphrase}
                  onChange={(value) => patch({ keyPassphrase: value })}
                />
              </FieldShell>
            </div>
          ) : (
            <div className="mt-4">
              <FieldShell label="Password" htmlFor="host-password" required error={errors.password}>
                <SecretInput
                  id="host-password"
                  storedMask={isEdit}
                  value={draft.password}
                  onChange={(value) => patch({ password: value })}
                />
              </FieldShell>
            </div>
          )}
        </div>

        <div className="border-t border-border pt-4">
          <button
            type="button"
            aria-expanded={advancedOpen}
            onClick={() => setAdvancedOpen((value) => !value)}
            className="mb-3 flex w-full items-center gap-1.5 border-b border-border pb-2 text-left"
          >
            <ChevronRight
              aria-hidden="true"
              className={cn('size-4 text-soft transition-transform', advancedOpen && 'rotate-90')}
            />
            <span className="text-[13px] font-semibold text-text">Advanced</span>
            <span className="text-xs text-soft">host key, sudo, timeout</span>
          </button>
          {advancedOpen ? (
            <div className="grid gap-4 sm:grid-cols-2">
              <FieldShell
                label="Host key fingerprint"
                htmlFor="host-key-fingerprint"
                error={errors.hostKey}
                help="Leave empty to trust the host on first use. The run log shows the fingerprint to pin here."
                className="sm:col-span-2"
              >
                <Input
                  id="host-key-fingerprint"
                  spellCheck={false}
                  className="font-mono"
                  placeholder="SHA256:..."
                  value={draft.hostKey}
                  onChange={(event) => patch({ hostKey: event.target.value })}
                />
              </FieldShell>
              <FieldShell
                label="Run commands through sudo"
                htmlFor="host-sudo"
                help="Commands run as sudo -n, so the user needs passwordless sudo."
              >
                <div className="flex h-9 items-center gap-2.5">
                  <Switch
                    id="host-sudo"
                    label="Run commands through sudo"
                    checked={draft.sudo}
                    onChange={(value) => patch({ sudo: value })}
                  />
                  <span className="text-sm text-muted">{draft.sudo ? 'Enabled' : 'Disabled'}</span>
                </div>
              </FieldShell>
              <FieldShell
                label="Connect timeout (seconds)"
                htmlFor="host-timeout"
                error={errors.connectTimeoutSeconds}
              >
                <Input
                  id="host-timeout"
                  type="number"
                  inputMode="numeric"
                  min={1}
                  className="font-mono"
                  value={draft.connectTimeout}
                  onChange={(event) => patch({ connectTimeout: event.target.value })}
                />
              </FieldShell>
            </div>
          ) : null}
        </div>

        <div ref={resultRef}>{test.data ? <TestPanel result={test.data} /> : null}</div>
      </div>
    </Dialog>
  )
}

export function TestPanel({ result }: { result: HostTestResult }) {
  const found = result.tools ?? []
  const missing = result.ok ? NOTABLE_TOOLS.filter((tool) => !found.includes(tool)) : []
  return (
    <div
      role="status"
      className={cn(
        'space-y-2 rounded-lg border p-3 text-sm',
        result.ok ? 'border-success/40 bg-success/10' : 'border-danger/40 bg-danger/10',
      )}
    >
      <div className={cn('flex items-start gap-2', result.ok ? 'text-success' : 'text-danger')}>
        {result.ok ? (
          <CircleCheck className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
        ) : (
          <CircleX className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
        )}
        <div className="min-w-0">
          <p className="font-medium">
            {result.ok ? 'Connection succeeded' : 'Connection failed'}
            {result.durationMs ? ` · ${formatDuration(result.durationMs)}` : ''}
          </p>
          {result.message ? <p className="mt-0.5 break-words text-text">{result.message}</p> : null}
        </div>
      </div>
      {result.os ? <p className="text-xs text-muted">Operating system: {result.os}</p> : null}
      {found.length ? (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-xs text-muted">Tools found</span>
          {found.map((tool) => (
            <span
              key={tool}
              className="rounded bg-surface-3 px-1.5 py-0.5 font-mono text-[11px] text-text"
            >
              {tool}
            </span>
          ))}
        </div>
      ) : null}
      {missing.length ? (
        <p className="flex items-start gap-1.5 text-xs text-warning">
          <TriangleAlert className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
          <span>
            Not found on the host: {missing.join(', ')}. Sources that need{' '}
            {missing.length === 1 ? 'it' : 'them'} cannot run here.
          </span>
        </p>
      ) : null}
    </div>
  )
}
