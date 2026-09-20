import { useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { ArrowLeft, Lock, Save, Server, TriangleAlert } from 'lucide-react'
import { errorMessage, fieldErrors } from '@/api/client'
import {
  useChannels,
  useDestinations,
  useJob,
  useSaveJob,
  useSettings,
  useSources,
  useTimezones,
} from '@/api/hooks'
import { ALL_EVENTS, SECRET_MASK, type Compression, type Encryption, type Job, type Retention } from '@/api/types'
import { PushPanel } from '@/components/PushPanel'
import { CronBuilder } from '@/components/form/CronBuilder'
import { TagInput } from '@/components/form/TagInput'
import { Button } from '@/components/ui/Button'
import { Card } from '@/components/ui/Card'
import { Checkbox, Input, Select, Switch, Textarea } from '@/components/ui/Input'
import { FieldShell } from '@/components/ui/Field'
import { EmptyState } from '@/components/ui/EmptyState'
import { SecretInput } from '@/components/form/SecretInput'
import { SkeletonRows } from '@/components/ui/Skeleton'
import { useToast } from '@/components/ui/Toast'
import { formatNumber, slugify, titleCase } from '@/lib/format'
import { usePageMeta } from '@/lib/pageMeta'
import { useCan } from '@/lib/permissions'
import { EMPTY_RETENTION, describeRetention, estimateKeptCount, isRetentionEmpty } from '@/lib/retention'
import { cn, passphraseStrength } from '@/lib/utils'

type JobDraft = {
  name: string
  slug: string
  description: string
  sourceId: string
  destinationIds: string[]
  schedule: string
  timezone: string
  enabled: boolean
  compression: Compression
  compressionLevel: number
  encryption: Encryption
  encryptionPassphrase: string
  retention: Retention
  notificationChannelIds: string[]
  notifyOn: string[]
  timeoutMinutes: number
  retries: number
  retryDelaySeconds: number
  preCommand: string
  postCommand: string
  verifyAfterUpload: boolean
  expectedIntervalMinutes: number
  tags: string[]
}

const EMPTY_DRAFT: JobDraft = {
  name: '',
  slug: '',
  description: '',
  sourceId: '',
  destinationIds: [],
  schedule: '0 2 * * *',
  timezone: 'UTC',
  enabled: true,
  compression: 'zstd',
  compressionLevel: 0,
  encryption: 'none',
  encryptionPassphrase: '',
  retention: { ...EMPTY_RETENTION, keepLast: 7, keepDaily: 7, keepWeekly: 4, keepMonthly: 6 },
  notificationChannelIds: [],
  notifyOn: [],
  timeoutMinutes: 0,
  retries: 2,
  retryDelaySeconds: 30,
  preCommand: '',
  postCommand: '',
  verifyAfterUpload: true,
  expectedIntervalMinutes: 0,
  tags: [],
}

const SECTIONS = [
  { id: 'basics', label: 'Basics' },
  { id: 'source', label: 'Source' },
  { id: 'destinations', label: 'Destinations' },
  { id: 'schedule', label: 'Schedule' },
  { id: 'pack', label: 'Pack' },
  { id: 'retention', label: 'Retention' },
  { id: 'notifications', label: 'Notifications' },
  { id: 'advanced', label: 'Advanced' },
]

function fromJob(job: Job): JobDraft {
  return {
    name: job.name,
    slug: job.slug,
    description: job.description ?? '',
    sourceId: job.sourceId,
    destinationIds: job.destinationIds ?? [],
    schedule: job.schedule ?? '',
    timezone: job.timezone || 'UTC',
    enabled: job.enabled,
    compression: job.compression ?? 'none',
    compressionLevel: job.compressionLevel ?? 0,
    encryption: job.encryption ?? 'none',
    encryptionPassphrase: job.encryptionPassphrase ?? '',
    retention: { ...EMPTY_RETENTION, ...(job.retention ?? {}) },
    notificationChannelIds: job.notificationChannelIds ?? [],
    notifyOn: job.notifyOn ?? [],
    timeoutMinutes: job.timeoutMinutes ?? 0,
    retries: job.retries ?? 0,
    retryDelaySeconds: job.retryDelaySeconds ?? 0,
    preCommand: job.preCommand ?? '',
    postCommand: job.postCommand ?? '',
    verifyAfterUpload: job.verifyAfterUpload ?? false,
    expectedIntervalMinutes: job.expectedIntervalMinutes ?? 0,
    tags: job.tags ?? [],
  }
}

function JobEditorDenied({ slug }: { slug?: string }) {
  const isEdit = Boolean(slug)
  usePageMeta(isEdit ? 'Edit job' : 'New job', [
    { label: 'Jobs', to: '/jobs' },
    { label: isEdit ? 'Edit' : 'New' },
  ])
  return (
    <EmptyState
      icon={<Lock className="size-6" />}
      title="This page is read-only for your account"
      description="You need an administrator account to edit jobs. Everything else stays available to read."
      action={
        <Link to={slug ? `/jobs/${slug}` : '/jobs'}>
          <Button>{slug ? 'Back to the job' : 'Back to jobs'}</Button>
        </Link>
      }
    />
  )
}

export function JobEditorPage() {
  const { slug } = useParams()
  const isEdit = Boolean(slug)
  const can = useCan()
  const job = useJob(slug)
  const settings = useSettings()

  if (!can.admin) return <JobEditorDenied slug={slug} />
  if (isEdit && job.isLoading) return <SkeletonRows rows={8} />
  if (isEdit && (job.isError || !job.data)) {
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
  if (!isEdit && settings.isLoading) return <SkeletonRows rows={8} />

  return (
    <JobForm
      key={job.data?.id ?? 'new'}
      job={job.data ?? null}
      defaultTimezone={settings.data?.defaultTimezone || 'UTC'}
    />
  )
}

function JobForm({ job, defaultTimezone }: { job: Job | null; defaultTimezone: string }) {
  const isEdit = Boolean(job)
  const slug = job?.slug
  const sources = useSources()
  const destinations = useDestinations()
  const channels = useChannels()
  const timezones = useTimezones()
  const save = useSaveJob(slug)
  const navigate = useNavigate()
  const toast = useToast()

  const [draft, setDraft] = useState<JobDraft>(() =>
    job ? fromJob(job) : { ...EMPTY_DRAFT, timezone: defaultTimezone },
  )
  const [slugTouched, setSlugTouched] = useState(Boolean(job))

  usePageMeta(isEdit ? `Edit ${job?.name ?? ''}` : 'New job', [
    { label: 'Jobs', to: '/jobs' },
    ...(job ? [{ label: job.name, to: `/jobs/${job.slug}` }] : []),
    { label: isEdit ? 'Edit' : 'New' },
  ])

  const selectedSource = useMemo(
    () => (sources.data ?? []).find((item) => item.id === draft.sourceId),
    [sources.data, draft.sourceId],
  )
  const isPush = selectedSource?.kind === 'push'

  const patch = (next: Partial<JobDraft>) => setDraft((prev) => ({ ...prev, ...next }))
  const patchRetention = (next: Partial<Retention>) =>
    setDraft((prev) => ({ ...prev, retention: { ...prev.retention, ...next } }))

  const errors = fieldErrors(save.error)
  const storedPassphrase = isEdit && draft.encryptionPassphrase === SECRET_MASK
  const strength = passphraseStrength(draft.encryptionPassphrase)

  const submit = () => {
    const payload = {
      ...draft,
      slug: draft.slug || slugify(draft.name),
      schedule: isPush ? '' : draft.schedule.trim(),
      encryptionPassphrase:
        draft.encryption === 'age' ? draft.encryptionPassphrase : '',
    }
    save.mutate(payload, {
      onSuccess: (saved) => {
        toast.success(isEdit ? 'Job saved' : 'Job created', saved.name)
        navigate(`/jobs/${saved.slug}`)
      },
      onError: (error) => toast.error('The job could not be saved', errorMessage(error)),
    })
  }

  const noSources = !sources.isLoading && (sources.data ?? []).length === 0
  const noDestinations = !destinations.isLoading && (destinations.data ?? []).length === 0

  return (
    <div className="space-y-4 pb-24">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <Link to={job ? `/jobs/${job.slug}` : '/jobs'}>
            <Button size="sm" icon={<ArrowLeft className="size-4" />}>
              Back
            </Button>
          </Link>
          <h2 className="text-lg font-semibold text-text">
            {isEdit ? `Edit ${job?.name ?? 'job'}` : 'New job'}
          </h2>
        </div>
        <nav className="hidden flex-wrap gap-1 lg:flex" aria-label="Job sections">
          {SECTIONS.map((section) => (
            <a
              key={section.id}
              href={`#section-${section.id}`}
              className="rounded-lg px-2 py-1 text-[12.5px] text-muted hover:bg-surface-3 hover:text-text"
            >
              {section.label}
            </a>
          ))}
        </nav>
      </div>

      {noSources || noDestinations ? (
        <div className="rounded-xl border border-warning/40 bg-warning/10 p-3 text-sm text-text">
          <p className="flex items-center gap-2 font-medium">
            <TriangleAlert className="size-4 text-warning" aria-hidden="true" />
            Missing building blocks
          </p>
          <p className="mt-1 text-muted">
            {noSources ? 'Create a source' : ''}
            {noSources && noDestinations ? ' and a destination' : ''}
            {!noSources && noDestinations ? 'Create a destination' : ''} before saving this job.
          </p>
          <div className="mt-2 flex gap-2">
            {noSources ? (
              <Link to="/sources?new=1">
                <Button size="sm">Add source</Button>
              </Link>
            ) : null}
            {noDestinations ? (
              <Link to="/destinations?new=1">
                <Button size="sm">Add destination</Button>
              </Link>
            ) : null}
          </div>
        </div>
      ) : null}

      <section id="section-basics">
        <Card title="Basics" description="How this job is identified">
          <div className="grid gap-4 sm:grid-cols-2">
            <FieldShell label="Name" htmlFor="job-name" required error={errors.name}>
              <Input
                id="job-name"
                value={draft.name}
                onChange={(event) => {
                  const name = event.target.value
                  patch({ name, slug: slugTouched ? draft.slug : slugify(name) })
                }}
                placeholder="Production database nightly"
              />
            </FieldShell>
            <FieldShell
              label="Slug"
              htmlFor="job-slug"
              required
              error={errors.slug}
              help="Used in URLs, the ingest endpoint and artifact filenames."
            >
              <Input
                id="job-slug"
                className="font-mono"
                value={draft.slug}
                onChange={(event) => {
                  setSlugTouched(true)
                  patch({ slug: slugify(event.target.value) })
                }}
                placeholder="production-db-nightly"
              />
            </FieldShell>
            <FieldShell label="Description" htmlFor="job-description" className="sm:col-span-2">
              <Textarea
                id="job-description"
                rows={2}
                value={draft.description}
                onChange={(event) => patch({ description: event.target.value })}
                placeholder="What this job protects and who to tell when it breaks"
              />
            </FieldShell>
            <FieldShell label="Tags" htmlFor="job-tags" help="Used for filtering in the jobs list.">
              <TagInput
                id="job-tags"
                value={draft.tags}
                onChange={(tags) => patch({ tags })}
                placeholder="production, database"
              />
            </FieldShell>
            <FieldShell label="Enabled" htmlFor="job-enabled" help="Disabled jobs never run on schedule.">
              <div className="flex h-9 items-center gap-2.5">
                <Switch
                  id="job-enabled"
                  label="Enabled"
                  checked={draft.enabled}
                  onChange={(enabled) => patch({ enabled })}
                />
                <span className="text-sm text-muted">{draft.enabled ? 'Scheduled' : 'Paused'}</span>
              </div>
            </FieldShell>
          </div>
        </Card>
      </section>

      <section id="section-source">
        <Card
          title="Source"
          description="What gets backed up"
          actions={
            <Link to="/sources?new=1">
              <Button size="sm">New source</Button>
            </Link>
          }
        >
          {sources.isLoading ? (
            <SkeletonRows rows={2} />
          ) : (sources.data ?? []).length === 0 ? (
            <EmptyState
              title="No sources yet"
              description="A source describes the database, directory or host to back up."
              action={
                <Link to="/sources?new=1">
                  <Button variant="primary">Create a source</Button>
                </Link>
              }
            />
          ) : (
            <div className="grid gap-3 sm:grid-cols-2">
              <FieldShell label="Source" htmlFor="job-source" required error={errors.sourceId}>
                <Select
                  id="job-source"
                  value={draft.sourceId}
                  onChange={(event) => patch({ sourceId: event.target.value })}
                >
                  <option value="">Select a source</option>
                  {(sources.data ?? []).map((source) => (
                    <option key={source.id} value={source.id}>
                      {source.name} ({source.kind})
                    </option>
                  ))}
                </Select>
              </FieldShell>
              {selectedSource ? (
                <div className="rounded-lg border border-border bg-surface-2 p-3 text-sm">
                  <p className="font-medium text-text">{selectedSource.name}</p>
                  <p className="mt-0.5 text-xs text-muted">
                    {titleCase(selectedSource.kind)}
                    {selectedSource.description ? ` · ${selectedSource.description}` : ''}
                  </p>
                  <p className="mt-1.5 inline-flex items-center gap-1.5 text-xs text-muted">
                    <Server className="size-3.5 shrink-0 text-soft" aria-hidden="true" />
                    {selectedSource.hostId
                      ? `Runs on ${selectedSource.hostName || 'a remote host'}`
                      : 'Runs on this server'}
                  </p>
                </div>
              ) : null}
            </div>
          )}
        </Card>
      </section>

      <section id="section-destinations">
        <Card
          title="Destinations"
          description="Every destination receives the same artifact"
          actions={
            <Link to="/destinations?new=1">
              <Button size="sm">New destination</Button>
            </Link>
          }
        >
          {destinations.isLoading ? (
            <SkeletonRows rows={2} />
          ) : (destinations.data ?? []).length === 0 ? (
            <EmptyState
              title="No destinations yet"
              description="Add a local directory, an S3 bucket or an SFTP host."
              action={
                <Link to="/destinations?new=1">
                  <Button variant="primary">Create a destination</Button>
                </Link>
              }
            />
          ) : (
            <div className="grid gap-2 sm:grid-cols-2">
              {(destinations.data ?? []).map((destination) => {
                const checked = draft.destinationIds.includes(destination.id)
                return (
                  <label
                    key={destination.id}
                    className={cn(
                      'flex cursor-pointer items-start gap-3 rounded-lg border p-3 transition-colors',
                      checked
                        ? 'border-accent bg-accent-soft'
                        : 'border-border bg-surface hover:border-border-strong',
                    )}
                  >
                    <Checkbox
                      checked={checked}
                      label={destination.name}
                      onChange={(next) =>
                        patch({
                          destinationIds: next
                            ? [...draft.destinationIds, destination.id]
                            : draft.destinationIds.filter((id) => id !== destination.id),
                        })
                      }
                    />
                    <span className="min-w-0">
                      <span className="block truncate text-[13px] font-medium text-text">
                        {destination.name}
                      </span>
                      <span className="block text-[11.5px] text-muted">
                        {destination.kind} · {formatNumber(destination.artifactCount)} artifacts
                      </span>
                    </span>
                  </label>
                )
              })}
              {errors.destinationIds ? (
                <p className="text-xs text-danger sm:col-span-2">{errors.destinationIds}</p>
              ) : null}
            </div>
          )}
        </Card>
      </section>

      <section id="section-schedule">
        <Card
          title="Schedule"
          description={isPush ? 'Push jobs are never scheduled' : 'When this job runs automatically'}
        >
          {isPush ? (
            <div className="space-y-4">
              <FieldShell
                label="Expected interval (minutes)"
                htmlFor="job-expected"
                help="Backvault marks the job overdue when no successful push arrives within this window. 0 disables the check."
              >
                <Input
                  id="job-expected"
                  type="number"
                  min={0}
                  className="font-mono"
                  value={draft.expectedIntervalMinutes}
                  onChange={(event) =>
                    patch({ expectedIntervalMinutes: Number(event.target.value) || 0 })
                  }
                />
              </FieldShell>
              <PushPanel
                slug={draft.slug || slugify(draft.name)}
                expectedIntervalMinutes={draft.expectedIntervalMinutes}
              />
            </div>
          ) : (
            <CronBuilder
              value={draft.schedule}
              onChange={(schedule) => patch({ schedule })}
              timezone={draft.timezone}
              onTimezoneChange={(timezone) => patch({ timezone })}
              timezones={timezones.data ?? []}
              error={errors.schedule}
            />
          )}
        </Card>
      </section>

      <section id="section-pack">
        <Card title="Pack" description="Compression and encryption applied before upload">
          <div className="grid gap-4 sm:grid-cols-2">
            <FieldShell label="Compression" htmlFor="job-compression">
              <Select
                id="job-compression"
                value={draft.compression}
                onChange={(event) => patch({ compression: event.target.value as Compression })}
              >
                <option value="none">None</option>
                <option value="gzip">gzip</option>
                <option value="zstd">zstd</option>
              </Select>
            </FieldShell>
            <FieldShell
              label="Compression level"
              htmlFor="job-compression-level"
              help="0 uses the driver default."
            >
              <Input
                id="job-compression-level"
                type="number"
                min={0}
                max={22}
                className="font-mono"
                disabled={draft.compression === 'none'}
                value={draft.compressionLevel}
                onChange={(event) => patch({ compressionLevel: Number(event.target.value) || 0 })}
              />
            </FieldShell>
            <FieldShell label="Encryption" htmlFor="job-encryption">
              <Select
                id="job-encryption"
                value={draft.encryption}
                onChange={(event) => patch({ encryption: event.target.value as Encryption })}
              >
                <option value="none">None</option>
                <option value="age">age (passphrase)</option>
              </Select>
            </FieldShell>
            {draft.encryption === 'age' ? (
              <FieldShell
                label="Passphrase"
                htmlFor="job-passphrase"
                required
                error={errors.encryptionPassphrase}
                help={
                  storedPassphrase
                    ? 'The stored passphrase is kept. Replace it only if you also keep the old one, existing archives still need it.'
                    : `${strength.label}. ${strength.hint}`
                }
              >
                <div className="space-y-2">
                  <SecretInput
                    id="job-passphrase"
                    storedMask={isEdit}
                    value={draft.encryptionPassphrase}
                    onChange={(encryptionPassphrase) => patch({ encryptionPassphrase })}
                  />
                  {storedPassphrase ? null : (
                    <div className="flex gap-1">
                      {[0, 1, 2, 3].map((index) => (
                        <span
                          key={index}
                          className={cn(
                            'h-1 flex-1 rounded-full',
                            index < strength.score
                              ? strength.score >= 3
                                ? 'bg-success'
                                : strength.score === 2
                                  ? 'bg-warning'
                                  : 'bg-danger'
                              : 'bg-surface-3',
                          )}
                        />
                      ))}
                    </div>
                  )}
                </div>
              </FieldShell>
            ) : null}
          </div>
        </Card>
      </section>

      <section id="section-retention">
        <Card title="Retention" description="What Backvault keeps on every destination">
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
              <FieldShell key={key} label={label} htmlFor={`retention-${key}`}>
                <Input
                  id={`retention-${key}`}
                  type="number"
                  min={0}
                  className="font-mono"
                  value={draft.retention[key]}
                  onChange={(event) => patchRetention({ [key]: Number(event.target.value) || 0 })}
                />
              </FieldShell>
            ))}
          </div>
          <div className="mt-4 rounded-xl border border-border bg-surface-2 p-3">
            <p className="text-sm text-text">
              {describeRetention(draft.retention, isRetentionEmpty(draft.retention))}
            </p>
            {!isRetentionEmpty(draft.retention) ? (
              <p className="mt-1.5 text-xs text-muted">
                At most {formatNumber(estimateKeptCount(draft.retention))} artifacts per destination
                survive the rules above.
              </p>
            ) : null}
            <div className="mt-2 flex flex-wrap gap-1.5">
              <Button
                size="sm"
                onClick={() =>
                  patch({
                    retention: { ...EMPTY_RETENTION, keepLast: 7, keepDaily: 7, keepWeekly: 4, keepMonthly: 6 },
                  })
                }
              >
                Standard
              </Button>
              <Button
                size="sm"
                onClick={() =>
                  patch({
                    retention: {
                      ...EMPTY_RETENTION,
                      keepLast: 24,
                      keepHourly: 24,
                      keepDaily: 14,
                      keepWeekly: 8,
                      keepMonthly: 12,
                      keepYearly: 3,
                    },
                  })
                }
              >
                Long term
              </Button>
              <Button size="sm" onClick={() => patch({ retention: { ...EMPTY_RETENTION } })}>
                Use defaults from settings
              </Button>
            </div>
          </div>
        </Card>
      </section>

      <section id="section-notifications">
        <Card
          title="Notifications"
          description="Channels and the events that reach them"
          actions={
            <Link to="/notifications">
              <Button size="sm">Manage channels</Button>
            </Link>
          }
        >
          {(channels.data ?? []).length === 0 ? (
            <EmptyState
              title="No channels configured"
              description="Add an email, webhook, Slack, Telegram or ntfy channel to be told when a run fails."
              action={
                <Link to="/notifications">
                  <Button variant="primary">Add a channel</Button>
                </Link>
              }
            />
          ) : (
            <div className="space-y-4">
              <div className="grid gap-2 sm:grid-cols-2">
                {(channels.data ?? []).map((channel) => {
                  const checked = draft.notificationChannelIds.includes(channel.id)
                  return (
                    <label
                      key={channel.id}
                      className={cn(
                        'flex cursor-pointer items-center gap-3 rounded-lg border p-3 transition-colors',
                        checked
                          ? 'border-accent bg-accent-soft'
                          : 'border-border bg-surface hover:border-border-strong',
                      )}
                    >
                      <Checkbox
                        checked={checked}
                        label={channel.name}
                        onChange={(next) =>
                          patch({
                            notificationChannelIds: next
                              ? [...draft.notificationChannelIds, channel.id]
                              : draft.notificationChannelIds.filter((id) => id !== channel.id),
                          })
                        }
                      />
                      <span className="min-w-0">
                        <span className="block truncate text-[13px] font-medium text-text">
                          {channel.name}
                        </span>
                        <span className="text-[11.5px] text-muted">{channel.kind}</span>
                      </span>
                    </label>
                  )
                })}
              </div>

              <div>
                <p className="mb-2 text-[13px] font-medium text-text">Events</p>
                <p className="mb-2 text-xs text-muted">
                  Leave every event unchecked to use the defaults from Settings.
                </p>
                <div className="grid gap-1.5 sm:grid-cols-2 lg:grid-cols-4">
                  {ALL_EVENTS.map((event) => {
                    const checked = draft.notifyOn.includes(event)
                    return (
                      <label
                        key={event}
                        className="flex cursor-pointer items-center gap-2 rounded-lg border border-border px-2.5 py-2"
                      >
                        <Checkbox
                          checked={checked}
                          label={event}
                          onChange={(next) =>
                            patch({
                              notifyOn: next
                                ? [...draft.notifyOn, event]
                                : draft.notifyOn.filter((item) => item !== event),
                            })
                          }
                        />
                        <span className="font-mono text-[12px] text-text">{event}</span>
                      </label>
                    )
                  })}
                </div>
              </div>
            </div>
          )}
        </Card>
      </section>

      <section id="section-advanced">
        <Card title="Advanced" description="Timeouts, retries and hooks">
          <div className="grid gap-4 sm:grid-cols-2">
            <FieldShell
              label="Timeout (minutes)"
              htmlFor="job-timeout"
              help="0 means the default of 6 hours."
            >
              <Input
                id="job-timeout"
                type="number"
                min={0}
                className="font-mono"
                value={draft.timeoutMinutes}
                onChange={(event) => patch({ timeoutMinutes: Number(event.target.value) || 0 })}
              />
            </FieldShell>
            <FieldShell label="Retries per destination" htmlFor="job-retries">
              <Input
                id="job-retries"
                type="number"
                min={0}
                className="font-mono"
                value={draft.retries}
                onChange={(event) => patch({ retries: Number(event.target.value) || 0 })}
              />
            </FieldShell>
            <FieldShell
              label="Retry delay (seconds)"
              htmlFor="job-retry-delay"
              help="Doubles after each attempt, capped at 10 minutes."
            >
              <Input
                id="job-retry-delay"
                type="number"
                min={0}
                className="font-mono"
                value={draft.retryDelaySeconds}
                onChange={(event) => patch({ retryDelaySeconds: Number(event.target.value) || 0 })}
              />
            </FieldShell>
            <FieldShell label="Verify after upload" htmlFor="job-verify">
              <div className="flex h-9 items-center gap-2.5">
                <Switch
                  id="job-verify"
                  label="Verify after upload"
                  checked={draft.verifyAfterUpload}
                  onChange={(verifyAfterUpload) => patch({ verifyAfterUpload })}
                />
                <span className="text-sm text-muted">
                  {draft.verifyAfterUpload ? 'Size and checksum are checked' : 'Skipped'}
                </span>
              </div>
            </FieldShell>
            <FieldShell
              label="Pre-command"
              htmlFor="job-pre"
              className="sm:col-span-2"
              help="Runs on the Backvault host before the dump. A non-zero exit fails the run."
            >
              <Input
                id="job-pre"
                className="font-mono"
                spellCheck={false}
                value={draft.preCommand}
                onChange={(event) => patch({ preCommand: event.target.value })}
              />
            </FieldShell>
            <FieldShell
              label="Post-command"
              htmlFor="job-post"
              className="sm:col-span-2"
              help="Runs after the upload and retention stages."
            >
              <Input
                id="job-post"
                className="font-mono"
                spellCheck={false}
                value={draft.postCommand}
                onChange={(event) => patch({ postCommand: event.target.value })}
              />
            </FieldShell>
          </div>
        </Card>
      </section>

      <div className="fixed inset-x-0 bottom-0 z-20 border-t border-border bg-surface px-4 py-3 shadow-pop md:pl-64">
        <div className="mx-auto flex max-w-[1400px] items-center justify-between gap-3">
          <p className="truncate text-xs text-muted">
            {draft.name || 'Unnamed job'} · {draft.destinationIds.length} destinations ·{' '}
            {draft.schedule && !isPush ? draft.schedule : 'manual'}
          </p>
          <div className="flex items-center gap-2">
            <Link to={job ? `/jobs/${job.slug}` : '/jobs'}>
              <Button>Cancel</Button>
            </Link>
            <Button
              variant="primary"
              icon={<Save className="size-4" />}
              loading={save.isPending}
              onClick={submit}
            >
              {isEdit ? 'Save job' : 'Create job'}
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}
