import { createServer } from 'node:http'
import { randomUUID, createHash } from 'node:crypto'
import { destinationSpecs, notifierSpecs, sourceSpecs, timezones, tools } from './data.mjs'

const PORT = Number(process.env.PORT ?? 8080)
const SECRET_MASK = '********'
const START = new Date()

const state = {
  users: [],
  sessions: new Map(),
  tokens: [],
  sources: [],
  destinations: [],
  jobs: [],
  runs: [],
  artifacts: [],
  channels: [],
  audit: [],
  settings: {
    siteName: 'Backvault',
    baseUrl: 'http://localhost:5173',
    defaultTimezone: 'Europe/Warsaw',
    maxConcurrentRuns: 2,
    defaultRetention: {
      keepLast: 7,
      keepHourly: 0,
      keepDaily: 7,
      keepWeekly: 4,
      keepMonthly: 6,
      keepYearly: 1,
      maxAgeDays: 400,
    },
    runHistoryDays: 90,
    auditHistoryDays: 365,
    overdueCheckMinutes: 15,
    defaultNotifyOn: ['run.failed', 'run.warning', 'job.overdue'],
  },
}

const eventClients = new Set()
const logClients = new Map()
const runLogs = new Map()

let counter = 0
function id(prefix) {
  counter += 1
  return `${prefix}_${Date.now().toString(36)}${counter.toString(36).padStart(3, '0')}`
}

function iso(date) {
  return new Date(date).toISOString()
}

function adminUser() {
  return state.users.find((item) => item.role === 'admin') ?? null
}

function scopesForRole(role) {
  return role === 'admin' ? ['admin', 'read', 'run', 'ingest'] : ['read']
}

function sha256(value) {
  return createHash('sha256').update(value).digest('hex')
}

function specsFor(type) {
  if (type === 'source') return sourceSpecs
  if (type === 'destination') return destinationSpecs
  return notifierSpecs
}

function secretFields(type, kind) {
  const spec = specsFor(type).find((item) => item.kind === kind)
  if (!spec) return []
  return spec.fields.filter((field) => field.secret || field.type === 'secret').map((field) => field.name)
}

function maskConfig(type, kind, config) {
  const out = { ...(config ?? {}) }
  for (const field of secretFields(type, kind)) {
    if (out[field] !== undefined && out[field] !== '') out[field] = SECRET_MASK
  }
  return out
}

function mergeSecrets(type, kind, next, previous) {
  const out = { ...(next ?? {}) }
  for (const field of secretFields(type, kind)) {
    if (out[field] === SECRET_MASK) {
      if (previous && previous[field] !== undefined) out[field] = previous[field]
      else delete out[field]
    }
  }
  return out
}

function broadcast(event, data) {
  const payload = `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`
  for (const client of eventClients) client.write(payload)
}

function appendLog(runId, line) {
  const lines = runLogs.get(runId) ?? []
  lines.push(line)
  runLogs.set(runId, lines)
  for (const client of logClients.get(runId) ?? []) {
    client.write(`event: line\ndata: ${line}\n\n`)
  }
}

function finishLog(runId) {
  for (const client of logClients.get(runId) ?? []) {
    client.write('event: done\ndata: {}\n\n')
  }
}

function stamp(text) {
  return `${new Date().toISOString()} ${text}`
}

function jobView(job) {
  const source = state.sources.find((item) => item.id === job.sourceId)
  const destinations = job.destinationIds
    .map((did) => state.destinations.find((item) => item.id === did))
    .filter(Boolean)
  const jobRuns = state.runs.filter((run) => run.jobId === job.id)
  const last = jobRuns[0]
  const jobArtifacts = state.artifacts.filter(
    (artifact) => artifact.jobId === job.id && artifact.status === 'present',
  )
  return {
    ...job,
    sourceName: source?.name ?? '',
    sourceKind: source?.kind ?? '',
    destinationNames: destinations.map((item) => item.name),
    lastRun: last
      ? {
          id: last.id,
          kind: last.kind,
          status: last.status,
          startedAt: last.startedAt,
          finishedAt: last.finishedAt,
          durationMs: last.durationMs,
          bytes: last.bytes,
          error: last.error,
        }
      : undefined,
    artifactCount: jobArtifacts.length,
    totalBytes: jobArtifacts.reduce((sum, artifact) => sum + artifact.size, 0),
  }
}

function destinationView(destination) {
  const items = state.artifacts.filter(
    (artifact) => artifact.destinationId === destination.id && artifact.status === 'present',
  )
  return {
    ...destination,
    config: maskConfig('destination', destination.kind, destination.config),
    jobCount: state.jobs.filter((job) => job.destinationIds.includes(destination.id)).length,
    usedBytes: items.reduce((sum, artifact) => sum + artifact.size, 0),
    artifactCount: items.length,
  }
}

function sourceView(source) {
  return {
    ...source,
    config: maskConfig('source', source.kind, source.config),
    jobCount: state.jobs.filter((job) => job.sourceId === source.id).length,
  }
}

function channelView(channel) {
  return { ...channel, config: maskConfig('notifier', channel.kind, channel.config) }
}

function audit(action, objectType, objectId, objectName, actorLabel) {
  state.audit.unshift({
    id: id('aud'),
    time: iso(Date.now()),
    actorId: 'usr_admin',
    actorLabel: actorLabel ?? (adminUser()?.email ?? 'system'),
    action,
    objectType,
    objectId,
    objectName,
    ip: '127.0.0.1',
  })
}

function nextRunFor(job) {
  if (!job.schedule || !job.enabled) return undefined
  const base = Date.now()
  const match = /^(\S+)\s+(\S+)/.exec(job.schedule)
  const hours = match && match[2] !== '*' ? Number(match[2].replace(/[^0-9].*$/, '')) : null
  const date = new Date(base)
  if (hours !== null && !Number.isNaN(hours)) {
    date.setHours(hours, 0, 0, 0)
    if (date.getTime() <= base) date.setDate(date.getDate() + 1)
  } else {
    date.setTime(base + 42 * 60 * 1000)
  }
  return iso(date)
}

const STAGES = {
  backup: ['prepare', 'dump', 'pack', 'upload', 'verify', 'retention', 'notify'],
  prune: ['prepare', 'retention', 'notify'],
  verify: ['prepare', 'verify', 'notify'],
  restore: ['prepare', 'download', 'unpack', 'restore', 'notify'],
  ingest: ['prepare', 'spool', 'upload', 'retention', 'notify'],
}

function createRun(job, kind, trigger, meta) {
  const run = {
    id: id('run'),
    jobId: job.id,
    jobSlug: job.slug,
    jobName: job.name,
    kind,
    trigger,
    status: 'queued',
    queuedAt: iso(Date.now()),
    durationMs: 0,
    bytes: 0,
    rawBytes: 0,
    stages: [],
    artifactIds: [],
    attempt: 1,
    meta,
    createdBy: adminUser()?.email ?? 'admin',
  }
  state.runs.unshift(run)
  runLogs.set(run.id, [])
  broadcast('run.updated', run)
  simulateRun(run, job, kind)
  return run
}

function simulateRun(run, job, kind) {
  const destinations = job.destinationIds
    .map((did) => state.destinations.find((item) => item.id === did))
    .filter(Boolean)
  const names = STAGES[kind] ?? STAGES.backup
  const stageNames = []
  for (const name of names) {
    if (name === 'upload') {
      for (const destination of destinations) stageNames.push(`upload:${destination.slug ?? destination.name}`)
    } else if (name === 'verify' && !job.verifyAfterUpload) {
      stageNames.push('verify')
    } else {
      stageNames.push(name)
    }
  }
  run.stages = stageNames.map((name) => ({ name, status: 'pending' }))
  const startedAt = Date.now() + 400
  const rawBytes = 180 * 1024 * 1024 + Math.floor(Math.random() * 90 * 1024 * 1024)
  const bytes = Math.floor(rawBytes / 3.4)
  const filename = `${job.slug}/${job.slug}-${new Date().toISOString().slice(0, 10).replace(/-/g, '')}-${new Date()
    .toISOString()
    .slice(11, 19)
    .replace(/:/g, '')}.dump.zst`

  setTimeout(() => {
    run.status = 'running'
    run.startedAt = iso(startedAt)
    appendLog(run.id, stamp(`level=INFO msg="run started" job=${job.slug} kind=${kind}`))
    broadcast('run.updated', { ...run })
  }, 400)

  let delay = 900
  stageNames.forEach((name, index) => {
    const stepDuration = name.startsWith('upload') ? 2200 : 1200
    setTimeout(() => {
      run.stages = run.stages.map((stage) =>
        stage.name === name ? { ...stage, status: 'running', startedAt: iso(Date.now()) } : stage,
      )
      if (name === 'dump') {
        appendLog(run.id, stamp(`level=INFO msg="starting dump" source=${job.sourceId}`))
        appendLog(run.id, stamp('level=INFO msg="pg_dump running" database=app_production format=custom'))
      } else if (name === 'pack') {
        appendLog(run.id, stamp(`level=INFO msg="packing" compression=${job.compression} encryption=${job.encryption}`))
      } else if (name.startsWith('upload')) {
        appendLog(run.id, stamp(`level=INFO msg="uploading" destination=${name.slice(7)} bytes=${bytes}`))
      } else if (name === 'verify') {
        appendLog(run.id, stamp('level=INFO msg="verifying stored size and checksum"'))
      } else if (name === 'retention') {
        appendLog(run.id, stamp('level=INFO msg="applying retention" kept=14 pruned=1'))
      } else if (name === 'notify') {
        appendLog(run.id, stamp('level=INFO msg="notifying channels" channels=1'))
      } else {
        appendLog(run.id, stamp(`level=INFO msg="stage ${name}"`))
      }
      broadcast('run.updated', { ...run })
    }, delay)

    delay += stepDuration

    setTimeout(() => {
      run.stages = run.stages.map((stage) =>
        stage.name === name
          ? {
              ...stage,
              status: name === 'verify' && !job.verifyAfterUpload ? 'skipped' : 'success',
              finishedAt: iso(Date.now()),
              message:
                name === 'dump'
                  ? `${(rawBytes / 1024 / 1024).toFixed(1)} MiB read`
                  : name === 'pack'
                    ? `${(bytes / 1024 / 1024).toFixed(1)} MiB packed`
                    : undefined,
            }
          : stage,
      )
      if (name === 'pack') {
        run.rawBytes = rawBytes
        run.bytes = bytes
        run.filename = filename
        run.sha256 = sha256(`${run.id}${filename}`)
      }
      if (name.startsWith('upload')) {
        const destinationName = name.slice(7)
        const destination = destinations.find(
          (item) => (item.slug ?? item.name) === destinationName,
        )
        if (destination && kind === 'backup') {
          const artifact = {
            id: id('art'),
            jobId: job.id,
            jobSlug: job.slug,
            jobName: job.name,
            runId: run.id,
            destinationId: destination.id,
            destinationName: destination.name,
            destinationKind: destination.kind,
            path: filename,
            filename: filename.split('/').pop(),
            size: bytes,
            sha256: run.sha256,
            compression: job.compression,
            encryption: job.encryption,
            sourceKind: state.sources.find((item) => item.id === job.sourceId)?.kind ?? 'files',
            extension: 'dump',
            status: 'present',
            createdAt: iso(Date.now()),
          }
          state.artifacts.unshift(artifact)
          run.artifactIds.push(artifact.id)
          broadcast('artifact.updated', artifact)
        }
      }
      appendLog(run.id, stamp(`level=INFO msg="stage finished" stage=${name}`))
      broadcast('run.updated', { ...run })

      if (index === stageNames.length - 1) {
        run.status = 'success'
        run.finishedAt = iso(Date.now())
        run.durationMs = Date.now() - startedAt
        appendLog(run.id, stamp(`level=INFO msg="run finished" status=success bytes=${run.bytes}`))
        broadcast('run.updated', { ...run })
        broadcast('job.updated', jobView(job))
        finishLog(run.id)
      }
    }, delay)
    delay += 260
  })
}

function seed() {
  const now = Date.now()
  const day = 24 * 60 * 60 * 1000

  state.sources = [
    {
      id: 'src_pg',
      name: 'Production PostgreSQL',
      kind: 'postgres',
      description: 'Main application database on db01',
      config: {
        host: 'db01.internal',
        port: 5432,
        database: 'app_production',
        user: 'backvault',
        password: 'super-secret',
        sslmode: 'require',
        format: 'custom',
      },
      tags: ['production', 'database'],
      createdAt: iso(now - 40 * day),
      updatedAt: iso(now - 3 * day),
      lastTestAt: iso(now - 2 * day),
      lastTestOk: true,
      jobCount: 1,
    },
    {
      id: 'src_files',
      name: 'Uploads directory',
      kind: 'files',
      description: 'User uploaded media on web01',
      config: { paths: ['/var/www/app/storage/uploads'], exclude: ['*.tmp', 'cache/*'], one_file_system: true },
      tags: ['production'],
      createdAt: iso(now - 30 * day),
      updatedAt: iso(now - 30 * day),
      lastTestAt: iso(now - 5 * day),
      lastTestOk: true,
      jobCount: 1,
    },
    {
      id: 'src_mysql',
      name: 'Billing MariaDB',
      kind: 'mysql',
      description: 'Invoices and payments',
      config: { host: '10.0.0.14', port: 3306, database: 'billing', user: 'root', password: 'hunter2', single_transaction: true },
      tags: ['database'],
      createdAt: iso(now - 20 * day),
      updatedAt: iso(now - 9 * day),
      lastTestAt: iso(now - 9 * day),
      lastTestOk: false,
      lastTestError: 'dial tcp 10.0.0.14:3306: connect: connection refused',
      jobCount: 1,
    },
    {
      id: 'src_push',
      name: 'Edge server push',
      kind: 'push',
      description: 'Artifacts pushed by the edge01 cron job',
      config: { note: 'edge01 runs backup-files.sh every six hours' },
      tags: ['push'],
      createdAt: iso(now - 12 * day),
      updatedAt: iso(now - 12 * day),
      jobCount: 1,
    },
  ]

  state.destinations = [
    {
      id: 'dst_local',
      name: 'Local disk',
      slug: 'local-disk',
      kind: 'local',
      description: 'Fast local copy for quick restores',
      config: { path: '/var/lib/backvault/backups' },
      tags: ['local'],
      createdAt: iso(now - 40 * day),
      updatedAt: iso(now - 40 * day),
      lastTestAt: iso(now - day),
      lastTestOk: true,
      jobCount: 0,
      usedBytes: 0,
      artifactCount: 0,
    },
    {
      id: 'dst_s3',
      name: 'Wasabi eu-central',
      slug: 'wasabi',
      kind: 's3',
      description: 'Offsite object storage',
      config: {
        endpoint: 'https://s3.eu-central-1.wasabisys.com',
        region: 'eu-central-1',
        bucket: 'acme-backups',
        prefix: 'backvault/',
        access_key: 'AKIA000000000000',
        secret_key: 'wasabi-secret',
        path_style: false,
      },
      tags: ['offsite'],
      createdAt: iso(now - 38 * day),
      updatedAt: iso(now - 10 * day),
      lastTestAt: iso(now - day),
      lastTestOk: true,
      jobCount: 0,
      usedBytes: 0,
      artifactCount: 0,
    },
    {
      id: 'dst_sftp',
      name: 'Hetzner Storage Box',
      slug: 'storagebox',
      kind: 'sftp',
      description: 'Cheap long term storage',
      config: { host: 'u123456.your-storagebox.de', port: 23, user: 'u123456', auth: 'password', password: 'storage-secret', base_path: '/backups' },
      tags: ['offsite'],
      createdAt: iso(now - 25 * day),
      updatedAt: iso(now - 25 * day),
      lastTestAt: iso(now - 6 * day),
      lastTestOk: true,
      jobCount: 0,
      usedBytes: 0,
      artifactCount: 0,
    },
  ]

  state.channels = [
    {
      id: 'ch_mail',
      name: 'Ops mailbox',
      kind: 'email',
      config: { host: 'smtp.example.com', port: 587, user: 'backvault', password: 'mail-secret', tls: 'starttls', from: 'backvault@example.com', to: ['ops@example.com'] },
      enabled: true,
      events: ['run.failed', 'run.warning', 'job.overdue'],
      createdAt: iso(now - 30 * day),
      updatedAt: iso(now - 30 * day),
      lastSentAt: iso(now - 2 * day),
    },
    {
      id: 'ch_slack',
      name: 'Slack #backups',
      kind: 'slack',
      config: { webhook_url: 'https://hooks.slack.com/services/T000/B000/xxx', channel: '#backups' },
      enabled: true,
      events: [],
      createdAt: iso(now - 18 * day),
      updatedAt: iso(now - 18 * day),
      lastSentAt: iso(now - 6 * 60 * 60 * 1000),
    },
  ]

  const retention = { keepLast: 7, keepHourly: 0, keepDaily: 7, keepWeekly: 4, keepMonthly: 6, keepYearly: 1, maxAgeDays: 400 }

  state.jobs = [
    {
      id: 'job_pg',
      slug: 'production-postgres-nightly',
      name: 'Production PostgreSQL nightly',
      description: 'Full dump of the application database, encrypted before upload',
      sourceId: 'src_pg',
      destinationIds: ['dst_local', 'dst_s3'],
      schedule: '0 2 * * *',
      timezone: 'Europe/Warsaw',
      enabled: true,
      compression: 'zstd',
      compressionLevel: 0,
      encryption: 'age',
      encryptionPassphrase: SECRET_MASK,
      retention,
      notificationChannelIds: ['ch_mail', 'ch_slack'],
      notifyOn: ['run.failed', 'run.warning'],
      timeoutMinutes: 120,
      retries: 2,
      retryDelaySeconds: 30,
      preCommand: '',
      postCommand: '',
      verifyAfterUpload: true,
      expectedIntervalMinutes: 0,
      tags: ['production', 'database'],
      createdAt: iso(now - 40 * day),
      updatedAt: iso(now - 3 * day),
      overdue: false,
      artifactCount: 0,
      totalBytes: 0,
    },
    {
      id: 'job_files',
      slug: 'uploads-weekly',
      name: 'Uploads weekly archive',
      description: 'Tar of user uploads pushed to the storage box',
      sourceId: 'src_files',
      destinationIds: ['dst_sftp'],
      schedule: '0 3 * * 0',
      timezone: 'Europe/Warsaw',
      enabled: true,
      compression: 'gzip',
      compressionLevel: 6,
      encryption: 'none',
      encryptionPassphrase: '',
      retention: { ...retention, keepLast: 4, keepDaily: 0, keepWeekly: 8 },
      notificationChannelIds: ['ch_slack'],
      notifyOn: [],
      timeoutMinutes: 0,
      retries: 1,
      retryDelaySeconds: 60,
      preCommand: '',
      postCommand: '',
      verifyAfterUpload: false,
      expectedIntervalMinutes: 0,
      tags: ['production'],
      createdAt: iso(now - 30 * day),
      updatedAt: iso(now - 30 * day),
      overdue: false,
      artifactCount: 0,
      totalBytes: 0,
    },
    {
      id: 'job_billing',
      slug: 'billing-hourly',
      name: 'Billing database hourly',
      description: 'Frequent dumps of the billing database',
      sourceId: 'src_mysql',
      destinationIds: ['dst_local'],
      schedule: '0 * * * *',
      timezone: 'UTC',
      enabled: false,
      compression: 'zstd',
      compressionLevel: 3,
      encryption: 'none',
      encryptionPassphrase: '',
      retention: { ...retention, keepLast: 24, keepHourly: 24 },
      notificationChannelIds: ['ch_mail'],
      notifyOn: [],
      timeoutMinutes: 30,
      retries: 0,
      retryDelaySeconds: 0,
      preCommand: '',
      postCommand: '',
      verifyAfterUpload: true,
      expectedIntervalMinutes: 0,
      tags: ['database'],
      createdAt: iso(now - 20 * day),
      updatedAt: iso(now - 2 * day),
      overdue: true,
      artifactCount: 0,
      totalBytes: 0,
    },
    {
      id: 'job_edge',
      slug: 'edge01-push',
      name: 'Edge server pushed archives',
      description: 'Receives artifacts from edge01 through the ingest API',
      sourceId: 'src_push',
      destinationIds: ['dst_s3'],
      schedule: '',
      timezone: 'UTC',
      enabled: true,
      compression: 'none',
      compressionLevel: 0,
      encryption: 'none',
      encryptionPassphrase: '',
      retention: { ...retention, keepLast: 10 },
      notificationChannelIds: [],
      notifyOn: [],
      timeoutMinutes: 0,
      retries: 0,
      retryDelaySeconds: 0,
      preCommand: '',
      postCommand: '',
      verifyAfterUpload: false,
      expectedIntervalMinutes: 360,
      tags: ['push'],
      createdAt: iso(now - 12 * day),
      updatedAt: iso(now - 12 * day),
      overdue: false,
      artifactCount: 0,
      totalBytes: 0,
    },
  ]

  for (const job of state.jobs) job.nextRunAt = nextRunFor(job)

  const statuses = ['success', 'success', 'success', 'success', 'warning', 'failed']
  for (let i = 0; i < 138; i += 1) {
    const job = state.jobs[i % 3]
    const started = now - i * 7 * 60 * 60 * 1000 - Math.floor(Math.random() * 3600_000)
    const status = i === 4 ? 'failed' : statuses[(i * 7) % statuses.length]
    const duration = 40_000 + Math.floor(Math.random() * 260_000)
    const rawBytes = 120 * 1024 * 1024 + Math.floor(Math.random() * 400 * 1024 * 1024)
    const bytes = Math.floor(rawBytes / 3.2)
    const stamp2 = new Date(started)
    const base = `${job.slug}-${stamp2.toISOString().slice(0, 10).replace(/-/g, '')}-${stamp2
      .toISOString()
      .slice(11, 19)
      .replace(/:/g, '')}`
    const filename = `${base}.dump${job.compression === 'zstd' ? '.zst' : job.compression === 'gzip' ? '.gz' : ''}${
      job.encryption === 'age' ? '.age' : ''
    }`
    const run = {
      id: `run_seed_${i}`,
      jobId: job.id,
      jobSlug: job.slug,
      jobName: job.name,
      kind: i % 11 === 5 ? 'prune' : 'backup',
      trigger: i % 9 === 0 ? 'manual' : 'schedule',
      status,
      queuedAt: iso(started - 2000),
      startedAt: iso(started),
      finishedAt: iso(started + duration),
      durationMs: duration,
      bytes: status === 'failed' ? 0 : bytes,
      rawBytes: status === 'failed' ? 0 : rawBytes,
      sha256: sha256(base),
      filename,
      error: status === 'failed' ? 'upload to wasabi failed: RequestTimeout after 3 attempts' : undefined,
      stages: [
        { name: 'prepare', status: 'success', startedAt: iso(started), finishedAt: iso(started + 400) },
        { name: 'dump', status: 'success', startedAt: iso(started + 400), finishedAt: iso(started + duration / 2) },
        { name: 'pack', status: 'success', startedAt: iso(started + duration / 2), finishedAt: iso(started + duration * 0.7) },
        {
          name: 'upload:local-disk',
          status: status === 'failed' ? 'failed' : 'success',
          startedAt: iso(started + duration * 0.7),
          finishedAt: iso(started + duration),
          message: status === 'failed' ? 'RequestTimeout' : undefined,
        },
        { name: 'retention', status: status === 'failed' ? 'skipped' : 'success' },
        { name: 'notify', status: 'success' },
      ],
      artifactIds: [],
      attempt: 1,
      createdBy: 'scheduler',
    }
    runLogs.set(
      run.id,
      [
        stamp(`level=INFO msg="run started" job=${job.slug}`),
        stamp('level=INFO msg="dump finished"'),
        status === 'failed'
          ? stamp('level=ERROR msg="upload failed" destination=wasabi error="RequestTimeout"')
          : stamp('level=INFO msg="upload finished"'),
        stamp(`level=INFO msg="run finished" status=${status}`),
      ],
    )
    state.runs.push(run)

    if (status !== 'failed' && run.kind === 'backup') {
      for (const did of job.destinationIds) {
        const destination = state.destinations.find((item) => item.id === did)
        if (!destination) continue
        state.artifacts.push({
          id: `art_seed_${i}_${did}`,
          jobId: job.id,
          jobSlug: job.slug,
          jobName: job.name,
          runId: run.id,
          destinationId: destination.id,
          destinationName: destination.name,
          destinationKind: destination.kind,
          path: `${job.slug}/${filename}`,
          filename,
          size: bytes,
          sha256: run.sha256,
          compression: job.compression,
          encryption: job.encryption,
          sourceKind: state.sources.find((item) => item.id === job.sourceId)?.kind ?? 'files',
          extension: 'dump',
          status: i > 30 ? 'pruned' : 'present',
          createdAt: iso(started + duration),
          verifiedAt: i % 4 === 0 ? iso(started + duration + 60_000) : undefined,
        })
        run.artifactIds.push(`art_seed_${i}_${did}`)
      }
    }
  }

  state.runs.sort((a, b) => new Date(b.queuedAt).getTime() - new Date(a.queuedAt).getTime())

  const auditSeeds = [
    ['job.update', 'job', 'job_pg', 'Production PostgreSQL nightly'],
    ['destination.create', 'destination', 'dst_sftp', 'Hetzner Storage Box'],
    ['token.create', 'token', 'tok_1', 'edge01 push agent'],
    ['job.run', 'job', 'job_files', 'Application files daily'],
    ['artifact.delete', 'artifact', 'art_seed_9_dst_local', 'application-files-20250104.tar.zst'],
    ['settings.update', 'settings', 'settings', 'Server settings'],
    ['source.update', 'source', 'src_pg', 'Production PostgreSQL'],
  ]
  state.audit = Array.from({ length: 84 }, (_, i) => {
    const [action, objectType, objectId, objectName] = auditSeeds[i % auditSeeds.length]
    return {
      id: `aud_${i + 1}`,
      time: iso(now - Math.floor(i * 0.7 * day) - i * 37 * 60_000),
      actorId: 'usr_seed',
      actorLabel: i % 6 === 0 ? 'token: edge01 push agent' : 'ops@example.com',
      action,
      objectType,
      objectId,
      objectName,
      ip: i % 6 === 0 ? '203.0.113.24' : '10.0.0.8',
    }
  })

  state.users = [
    {
      id: 'usr_viewer',
      email: 'viewer@example.com',
      name: 'Val Viewer',
      role: 'viewer',
      createdAt: iso(now - 20 * day),
      lastLoginAt: iso(now - 3 * 60 * 60 * 1000),
      password: 'viewer-password',
    },
  ]

  state.tokens = [
    {
      id: 'tok_1',
      name: 'edge01 push agent',
      prefix: 'bvt_9f2a',
      scopes: ['ingest'],
      jobSlugs: ['edge01-push'],
      createdAt: iso(now - 9 * day),
      createdBy: 'ops@example.com',
      lastUsedAt: iso(now - 4 * 60 * 60 * 1000),
    },
    {
      id: 'tok_2',
      name: 'monitoring',
      prefix: 'bvt_11c7',
      scopes: ['read'],
      jobSlugs: [],
      createdAt: iso(now - 15 * day),
      createdBy: 'ops@example.com',
    },
  ]
}

function dashboard() {
  const now = Date.now()
  const day = 24 * 60 * 60 * 1000
  const daily = []
  for (let i = 29; i >= 0; i -= 1) {
    const date = new Date(now - i * day)
    const key = date.toISOString().slice(0, 10)
    const dayRuns = state.runs.filter((run) => (run.startedAt ?? run.queuedAt).slice(0, 10) === key)
    daily.push({
      date: key,
      success: dayRuns.filter((run) => run.status === 'success').length || (i % 3 === 0 ? 2 : 3),
      failed: dayRuns.filter((run) => run.status === 'failed').length || (i % 9 === 0 ? 1 : 0),
      bytes:
        dayRuns.reduce((sum, run) => sum + run.bytes, 0) ||
        Math.floor((2 + Math.sin(i / 3) + 2) * 1024 * 1024 * 1024),
    })
  }
  const present = state.artifacts.filter((artifact) => artifact.status === 'present')
  const jobs = state.jobs.map(jobView)
  return {
    jobs: jobs.length,
    jobsEnabled: jobs.filter((job) => job.enabled).length,
    jobsOverdue: jobs.filter((job) => job.overdue).length,
    jobsFailing: jobs.filter((job) => job.lastRun?.status === 'failed').length,
    runsRunning: state.runs.filter((run) => run.status === 'running' || run.status === 'queued').length,
    runs24hSuccess: state.runs.filter(
      (run) => run.status === 'success' && now - new Date(run.queuedAt).getTime() < day,
    ).length,
    runs24hFailed: state.runs.filter(
      (run) => run.status === 'failed' && now - new Date(run.queuedAt).getTime() < day,
    ).length,
    artifacts: present.length,
    totalBytes: present.reduce((sum, artifact) => sum + artifact.size, 0),
    destinations: state.destinations.map((destination) => {
      const items = present.filter((artifact) => artifact.destinationId === destination.id)
      return {
        destinationId: destination.id,
        destinationName: destination.name,
        kind: destination.kind,
        bytes: items.reduce((sum, artifact) => sum + artifact.size, 0),
        artifacts: items.length,
      }
    }),
    daily,
    recentRuns: state.runs.slice(0, 10),
    upcoming: jobs
      .filter((job) => job.nextRunAt)
      .sort((a, b) => new Date(a.nextRunAt).getTime() - new Date(b.nextRunAt).getTime())
      .slice(0, 5),
    problemJobs: jobs.filter((job) => job.overdue || job.lastRun?.status === 'failed').slice(0, 5),
  }
}

function send(res, status, body, headers = {}) {
  const payload = typeof body === 'string' ? body : JSON.stringify(body)
  res.writeHead(status, {
    'Content-Type': typeof body === 'string' ? 'text/plain; charset=utf-8' : 'application/json',
    'Cache-Control': 'no-store',
    ...headers,
  })
  res.end(payload)
}

function fail(res, status, code, message, fields) {
  send(res, status, { error: { code, message, fields } })
}

function list(items, query) {
  const requested = Number(query.get('limit') ?? 50)
  const limit = Math.min(Number.isFinite(requested) && requested > 0 ? requested : 50, 500)
  const parsedOffset = Number(query.get('offset') ?? 0)
  const offset = Number.isFinite(parsedOffset) && parsedOffset > 0 ? parsedOffset : 0
  return { items: items.slice(offset, offset + limit), total: items.length }
}

function timeParam(query, name) {
  const raw = query.get(name)
  if (!raw) return null
  const parsed = new Date(raw.length === 10 ? `${raw}T00:00:00.000Z` : raw).getTime()
  return Number.isNaN(parsed) ? null : parsed
}

async function readBody(req) {
  const chunks = []
  for await (const chunk of req) chunks.push(chunk)
  const text = Buffer.concat(chunks).toString('utf8')
  if (!text) return {}
  try {
    return JSON.parse(text)
  } catch {
    return { raw: text }
  }
}

function sessionUser(req) {
  const cookie = req.headers.cookie ?? ''
  const match = /backvault_session=([^;]+)/.exec(cookie)
  if (match) {
    const userId = state.sessions.get(match[1])
    const user = state.users.find((item) => item.id === userId)
    if (user) return user
  }
  const auth = req.headers.authorization ?? ''
  if (auth.startsWith('Bearer ')) return adminUser() ?? state.users[0] ?? null
  return null
}

function sse(req, res) {
  res.writeHead(200, {
    'Content-Type': 'text/event-stream',
    'Cache-Control': 'no-cache, no-transform',
    Connection: 'keep-alive',
    'X-Accel-Buffering': 'no',
  })
  res.write('retry: 2000\n\n')
}

const server = createServer(async (req, res) => {
  const url = new URL(req.url, `http://${req.headers.host ?? 'localhost'}`)
  const path = url.pathname.replace(/^\/api\/v1/, '')
  const query = url.searchParams
  const method = req.method ?? 'GET'

  res.setHeader('Access-Control-Allow-Origin', req.headers.origin ?? '*')
  res.setHeader('Access-Control-Allow-Credentials', 'true')
  res.setHeader('Access-Control-Allow-Headers', 'Content-Type, X-Requested-With, Authorization')
  res.setHeader('Access-Control-Allow-Methods', 'GET, POST, PUT, DELETE, OPTIONS')
  if (method === 'OPTIONS') {
    res.writeHead(204)
    res.end()
    return
  }

  const user = sessionUser(req)
  const open =
    path === '/healthz' ||
    path === '/readyz' ||
    path === '/setup/status' ||
    path === '/setup' ||
    path === '/auth/login'

  if (!open && !user) {
    fail(res, 401, 'unauthenticated', 'Your session has expired')
    return
  }

  if (user && user.role !== 'admin' && path !== '/auth/logout') {
    const ownPassword = method === 'PUT' && path === `/users/${user.id}/password`
    const adminOnlyRead =
      path === '/users' ||
      path.startsWith('/users/') ||
      path === '/tokens' ||
      path.startsWith('/tokens/') ||
      path === '/audit' ||
      path === '/export'
    if (!ownPassword && (adminOnlyRead || method !== 'GET')) {
      return fail(res, 403, 'forbidden', 'This account does not have the required scope')
    }
  }

  const body = method === 'GET' || method === 'DELETE' ? {} : await readBody(req)
  const segments = path.split('/').filter(Boolean)

  if (path === '/healthz' || path === '/readyz') return send(res, 200, { ok: true })

  if (path === '/setup/status') return send(res, 200, { needsSetup: !adminUser() })

  if (path === '/setup' && method === 'POST') {
    if (adminUser()) return fail(res, 409, 'conflict', 'Setup has already been completed')
    const created = {
      id: id('usr'),
      email: body.email,
      name: body.name,
      role: 'admin',
      createdAt: iso(Date.now()),
      lastLoginAt: iso(Date.now()),
      password: body.password,
    }
    state.users.push(created)
    const token = randomUUID()
    state.sessions.set(token, created.id)
    audit('setup.complete', 'user', created.id, created.email, created.email)
    return send(
      res,
      200,
      { user: { ...created, password: undefined } },
      { 'Set-Cookie': `backvault_session=${token}; Path=/; HttpOnly; SameSite=Lax` },
    )
  }

  if (path === '/auth/login' && method === 'POST') {
    const found = state.users.find((item) => item.email === body.email)
    if (!found || (found.password && found.password !== body.password)) {
      return fail(res, 401, 'invalid_credentials', 'Email or password is not correct')
    }
    found.lastLoginAt = iso(Date.now())
    const token = randomUUID()
    state.sessions.set(token, found.id)
    return send(
      res,
      200,
      { user: { ...found, password: undefined } },
      { 'Set-Cookie': `backvault_session=${token}; Path=/; HttpOnly; SameSite=Lax` },
    )
  }

  if (path === '/auth/logout' && method === 'POST') {
    const cookie = /backvault_session=([^;]+)/.exec(req.headers.cookie ?? '')
    if (cookie) state.sessions.delete(cookie[1])
    return send(res, 200, { ok: true }, { 'Set-Cookie': 'backvault_session=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax' })
  }

  if (path === '/auth/me') {
    return send(res, 200, {
      user: { ...user, password: undefined },
      authType: 'session',
      scopes: scopesForRole(user.role),
    })
  }

  if (path === '/meta/version') {
    return send(res, 200, {
      version: '0.1.0-mock',
      commit: 'c0ffee1',
      buildDate: START.toISOString().slice(0, 10),
      goVersion: 'go1.27.0',
      startedAt: START.toISOString(),
    })
  }
  if (path === '/meta/sources') return send(res, 200, { items: sourceSpecs, total: sourceSpecs.length })
  if (path === '/meta/destinations') return send(res, 200, { items: destinationSpecs, total: destinationSpecs.length })
  if (path === '/meta/notifiers') return send(res, 200, { items: notifierSpecs, total: notifierSpecs.length })
  if (path === '/meta/tools') return send(res, 200, { items: tools, total: tools.length })
  if (path === '/meta/timezones') return send(res, 200, { items: timezones, total: timezones.length })

  if (path === '/dashboard') return send(res, 200, dashboard())

  if (path === '/events/stream') {
    sse(req, res)
    eventClients.add(res)
    const heartbeat = setInterval(() => res.write(': ping\n\n'), 20000)
    req.on('close', () => {
      clearInterval(heartbeat)
      eventClients.delete(res)
    })
    return undefined
  }

  if (segments[0] === 'sources') {
    if (segments.length === 1 && method === 'GET') {
      return send(res, 200, list(state.sources.map(sourceView), query))
    }
    if (segments.length === 1 && method === 'POST') {
      const created = {
        id: id('src'),
        name: body.name,
        kind: body.kind,
        description: body.description ?? '',
        config: body.config ?? {},
        tags: body.tags ?? [],
        createdAt: iso(Date.now()),
        updatedAt: iso(Date.now()),
        jobCount: 0,
      }
      state.sources.push(created)
      audit('source.create', 'source', created.id, created.name)
      return send(res, 201, sourceView(created))
    }
    if (segments[1] === 'test' && method === 'POST') {
      return send(res, 200, { ok: true, message: `Connected to ${body.kind} in the mock backend`, durationMs: 148 })
    }
    const source = state.sources.find((item) => item.id === segments[1])
    if (!source) return fail(res, 404, 'not_found', 'Source not found')
    if (segments[2] === 'test' && method === 'POST') {
      source.lastTestAt = iso(Date.now())
      source.lastTestOk = true
      source.lastTestError = ''
      return send(res, 200, { ok: true, message: 'Connection succeeded', durationMs: 212 })
    }
    if (method === 'GET') return send(res, 200, sourceView(source))
    if (method === 'PUT') {
      source.name = body.name ?? source.name
      source.description = body.description ?? ''
      source.tags = body.tags ?? []
      source.config = mergeSecrets('source', source.kind, body.config ?? {}, source.config)
      source.updatedAt = iso(Date.now())
      audit('source.update', 'source', source.id, source.name)
      return send(res, 200, sourceView(source))
    }
    if (method === 'DELETE') {
      if (state.jobs.some((job) => job.sourceId === source.id)) {
        return fail(res, 409, 'conflict', 'Jobs still reference this source')
      }
      state.sources = state.sources.filter((item) => item.id !== source.id)
      audit('source.delete', 'source', source.id, source.name)
      return send(res, 204, '')
    }
  }

  if (segments[0] === 'destinations') {
    if (segments.length === 1 && method === 'GET') {
      return send(res, 200, list(state.destinations.map(destinationView), query))
    }
    if (segments.length === 1 && method === 'POST') {
      const created = {
        id: id('dst'),
        name: body.name,
        slug: String(body.name ?? 'destination').toLowerCase().replace(/[^a-z0-9]+/g, '-'),
        kind: body.kind,
        description: body.description ?? '',
        config: body.config ?? {},
        tags: body.tags ?? [],
        createdAt: iso(Date.now()),
        updatedAt: iso(Date.now()),
        jobCount: 0,
        usedBytes: 0,
        artifactCount: 0,
      }
      state.destinations.push(created)
      audit('destination.create', 'destination', created.id, created.name)
      return send(res, 201, destinationView(created))
    }
    if (segments[1] === 'test' && method === 'POST') {
      return send(res, 200, { ok: true, message: `Reached the ${body.kind} destination`, durationMs: 173 })
    }
    const destination = state.destinations.find((item) => item.id === segments[1])
    if (!destination) return fail(res, 404, 'not_found', 'Destination not found')
    if (segments[2] === 'test' && method === 'POST') {
      destination.lastTestAt = iso(Date.now())
      destination.lastTestOk = true
      return send(res, 200, { ok: true, message: 'Write and delete succeeded', durationMs: 264 })
    }
    if (segments[2] === 'browse') {
      const prefix = query.get('prefix') ?? ''
      const items = state.artifacts
        .filter((artifact) => artifact.destinationId === destination.id)
        .filter((artifact) => artifact.path.startsWith(prefix))
        .slice(0, 40)
        .map((artifact) => ({
          path: artifact.path,
          size: artifact.size,
          modTime: artifact.createdAt,
          isDir: false,
        }))
      const folders = prefix
        ? []
        : [...new Set(state.artifacts.map((artifact) => artifact.path.split('/')[0]))].map((name) => ({
            path: `${name}/`,
            size: 0,
            modTime: iso(Date.now()),
            isDir: true,
          }))
      return send(res, 200, { items: [...folders, ...items], total: items.length + folders.length })
    }
    if (method === 'GET') return send(res, 200, destinationView(destination))
    if (method === 'PUT') {
      destination.name = body.name ?? destination.name
      destination.description = body.description ?? ''
      destination.tags = body.tags ?? []
      destination.config = mergeSecrets('destination', destination.kind, body.config ?? {}, destination.config)
      destination.updatedAt = iso(Date.now())
      audit('destination.update', 'destination', destination.id, destination.name)
      return send(res, 200, destinationView(destination))
    }
    if (method === 'DELETE') {
      if (state.jobs.some((job) => job.destinationIds.includes(destination.id))) {
        return fail(res, 409, 'conflict', 'Jobs still reference this destination')
      }
      state.destinations = state.destinations.filter((item) => item.id !== destination.id)
      audit('destination.delete', 'destination', destination.id, destination.name)
      return send(res, 204, '')
    }
  }

  if (segments[0] === 'jobs') {
    if (segments.length === 1 && method === 'GET') {
      return send(res, 200, list(state.jobs.map(jobView), query))
    }
    if (segments.length === 1 && method === 'POST') {
      const created = {
        id: id('job'),
        slug: body.slug || String(body.name ?? 'job').toLowerCase().replace(/[^a-z0-9]+/g, '-'),
        name: body.name,
        description: body.description ?? '',
        sourceId: body.sourceId,
        destinationIds: body.destinationIds ?? [],
        schedule: body.schedule ?? '',
        timezone: body.timezone || 'UTC',
        enabled: body.enabled ?? true,
        compression: body.compression ?? 'none',
        compressionLevel: body.compressionLevel ?? 0,
        encryption: body.encryption ?? 'none',
        encryptionPassphrase: body.encryptionPassphrase ? SECRET_MASK : '',
        retention: body.retention ?? state.settings.defaultRetention,
        notificationChannelIds: body.notificationChannelIds ?? [],
        notifyOn: body.notifyOn ?? [],
        timeoutMinutes: body.timeoutMinutes ?? 0,
        retries: body.retries ?? 0,
        retryDelaySeconds: body.retryDelaySeconds ?? 0,
        preCommand: body.preCommand ?? '',
        postCommand: body.postCommand ?? '',
        verifyAfterUpload: body.verifyAfterUpload ?? false,
        expectedIntervalMinutes: body.expectedIntervalMinutes ?? 0,
        tags: body.tags ?? [],
        createdAt: iso(Date.now()),
        updatedAt: iso(Date.now()),
        overdue: false,
        artifactCount: 0,
        totalBytes: 0,
      }
      if (!created.name) return fail(res, 400, 'validation_failed', 'The job needs a name', { name: 'required' })
      if (!created.sourceId) {
        return fail(res, 400, 'validation_failed', 'Select a source', { sourceId: 'required' })
      }
      if (!created.destinationIds.length) {
        return fail(res, 400, 'validation_failed', 'Select at least one destination', {
          destinationIds: 'required',
        })
      }
      created.nextRunAt = nextRunFor(created)
      state.jobs.push(created)
      audit('job.create', 'job', created.id, created.name)
      broadcast('job.updated', jobView(created))
      return send(res, 201, jobView(created))
    }

    const job = state.jobs.find((item) => item.id === segments[1] || item.slug === segments[1])
    if (!job) return fail(res, 404, 'not_found', 'Job not found')

    if (segments[2] === 'run' && method === 'POST') {
      if (state.runs.some((run) => run.jobId === job.id && (run.status === 'running' || run.status === 'queued'))) {
        return fail(res, 423, 'locked', 'This job is already running')
      }
      const run = createRun(job, 'backup', 'manual')
      audit('job.run', 'job', job.id, job.name)
      return send(res, 202, { run })
    }
    if (segments[2] === 'prune' && method === 'POST') {
      const run = createRun(job, 'prune', 'manual')
      return send(res, 202, { run })
    }
    if ((segments[2] === 'enable' || segments[2] === 'disable') && method === 'POST') {
      job.enabled = segments[2] === 'enable'
      job.nextRunAt = nextRunFor(job)
      job.updatedAt = iso(Date.now())
      audit(`job.${segments[2]}`, 'job', job.id, job.name)
      broadcast('job.updated', jobView(job))
      return send(res, 200, jobView(job))
    }
    if (segments[2] === 'duplicate' && method === 'POST') {
      const copy = {
        ...job,
        id: id('job'),
        slug: `${job.slug}-copy`,
        name: `${job.name} (copy)`,
        enabled: false,
        createdAt: iso(Date.now()),
        updatedAt: iso(Date.now()),
      }
      state.jobs.push(copy)
      audit('job.duplicate', 'job', copy.id, copy.name)
      return send(res, 201, jobView(copy))
    }
    if (segments[2] === 'runs') {
      return send(res, 200, list(state.runs.filter((run) => run.jobId === job.id), query))
    }
    if (segments[2] === 'artifacts') {
      return send(res, 200, list(state.artifacts.filter((artifact) => artifact.jobId === job.id), query))
    }
    if (method === 'GET') return send(res, 200, jobView(job))
    if (method === 'PUT') {
      Object.assign(job, {
        ...body,
        id: job.id,
        encryptionPassphrase:
          body.encryptionPassphrase === SECRET_MASK || !body.encryptionPassphrase
            ? job.encryptionPassphrase
            : SECRET_MASK,
        updatedAt: iso(Date.now()),
      })
      job.nextRunAt = nextRunFor(job)
      audit('job.update', 'job', job.id, job.name)
      broadcast('job.updated', jobView(job))
      return send(res, 200, jobView(job))
    }
    if (method === 'DELETE') {
      state.jobs = state.jobs.filter((item) => item.id !== job.id)
      if (query.get('deleteArtifacts') === '1') {
        state.artifacts = state.artifacts.filter((artifact) => artifact.jobId !== job.id)
      }
      audit('job.delete', 'job', job.id, job.name)
      return send(res, 204, '')
    }
  }

  if (segments[0] === 'runs') {
    if (segments.length === 1) {
      let items = state.runs
      if (query.get('job')) items = items.filter((run) => run.jobSlug === query.get('job'))
      if (query.get('status')) items = items.filter((run) => run.status === query.get('status'))
      if (query.get('kind')) items = items.filter((run) => run.kind === query.get('kind'))
      const since = timeParam(query, 'since')
      if (since !== null) items = items.filter((run) => new Date(run.queuedAt).getTime() >= since)
      const until = timeParam(query, 'until')
      if (until !== null) items = items.filter((run) => new Date(run.queuedAt).getTime() <= until)
      return send(res, 200, list(items, query))
    }
    const run = state.runs.find((item) => item.id === segments[1])
    if (!run) return fail(res, 404, 'not_found', 'Run not found')
    if (segments[2] === 'cancel' && method === 'POST') {
      run.status = 'canceled'
      run.finishedAt = iso(Date.now())
      appendLog(run.id, stamp('level=WARN msg="run canceled by operator"'))
      finishLog(run.id)
      broadcast('run.updated', { ...run })
      return send(res, 200, run)
    }
    if (segments[2] === 'log' && segments[3] === 'stream') {
      sse(req, res)
      const clients = logClients.get(run.id) ?? new Set()
      clients.add(res)
      logClients.set(run.id, clients)
      const heartbeat = setInterval(() => res.write(': ping\n\n'), 20000)
      if (run.status !== 'running' && run.status !== 'queued') {
        res.write('event: done\ndata: {}\n\n')
      }
      req.on('close', () => {
        clearInterval(heartbeat)
        clients.delete(res)
      })
      return undefined
    }
    if (segments[2] === 'log') {
      return send(res, 200, (runLogs.get(run.id) ?? []).join('\n'))
    }
    if (method === 'GET') return send(res, 200, run)
  }

  if (segments[0] === 'artifacts') {
    if (segments.length === 1) {
      let items = state.artifacts
      if (query.get('job')) items = items.filter((artifact) => artifact.jobSlug === query.get('job'))
      if (query.get('destination')) {
        items = items.filter((artifact) => artifact.destinationId === query.get('destination'))
      }
      if (query.get('status')) items = items.filter((artifact) => artifact.status === query.get('status'))
      if (query.get('run')) items = items.filter((artifact) => artifact.runId === query.get('run'))
      if (query.get('q')) {
        const needle = query.get('q').toLowerCase()
        items = items.filter((artifact) => artifact.filename.toLowerCase().includes(needle))
      }
      const since = timeParam(query, 'since')
      if (since !== null) {
        items = items.filter((artifact) => new Date(artifact.createdAt).getTime() >= since)
      }
      const until = timeParam(query, 'until')
      if (until !== null) {
        items = items.filter((artifact) => new Date(artifact.createdAt).getTime() <= until)
      }
      return send(res, 200, list(items, query))
    }
    const artifact = state.artifacts.find((item) => item.id === segments[1])
    if (!artifact) return fail(res, 404, 'not_found', 'Artifact not found')
    if (segments[2] === 'download') {
      return send(res, 200, `mock artifact ${artifact.filename}\n`, {
        'Content-Disposition': `attachment; filename="${artifact.filename}"`,
      })
    }
    if (segments[2] === 'verify' && method === 'POST') {
      const job = state.jobs.find((item) => item.id === artifact.jobId)
      const run = createRun(job ?? state.jobs[0], 'verify', 'manual', { artifactId: artifact.id })
      artifact.verifiedAt = iso(Date.now())
      broadcast('artifact.updated', artifact)
      return send(res, 202, { run })
    }
    if (segments[2] === 'restore' && method === 'POST') {
      const job = state.jobs.find((item) => item.id === artifact.jobId)
      const run = createRun(job ?? state.jobs[0], 'restore', 'manual', {
        artifactId: artifact.id,
        mode: body.mode ?? 'path',
        targetPath: body.targetPath ?? '',
      })
      audit('artifact.restore', 'artifact', artifact.id, artifact.filename)
      return send(res, 202, { run })
    }
    if (method === 'GET') return send(res, 200, artifact)
    if (method === 'DELETE') {
      artifact.status = 'deleted'
      artifact.deletedAt = iso(Date.now())
      audit('artifact.delete', 'artifact', artifact.id, artifact.filename)
      broadcast('artifact.updated', artifact)
      return send(res, 204, '')
    }
  }

  if (segments[0] === 'notifications' && segments[1] === 'channels') {
    if (segments.length === 2 && method === 'GET') {
      return send(res, 200, list(state.channels.map(channelView), query))
    }
    if (segments.length === 2 && method === 'POST') {
      const created = {
        id: id('ch'),
        name: body.name,
        kind: body.kind,
        config: body.config ?? {},
        enabled: body.enabled ?? true,
        events: body.events ?? [],
        createdAt: iso(Date.now()),
        updatedAt: iso(Date.now()),
      }
      state.channels.push(created)
      audit('channel.create', 'channel', created.id, created.name)
      return send(res, 201, channelView(created))
    }
    if (segments[2] === 'test' && method === 'POST') {
      return send(res, 200, { ok: true, message: 'Test notification sent', durationMs: 320 })
    }
    const channel = state.channels.find((item) => item.id === segments[2])
    if (!channel) return fail(res, 404, 'not_found', 'Channel not found')
    if (segments[3] === 'test' && method === 'POST') {
      channel.lastSentAt = iso(Date.now())
      channel.lastError = ''
      return send(res, 200, { ok: true, message: 'Test notification sent', durationMs: 284 })
    }
    if (method === 'GET') return send(res, 200, channelView(channel))
    if (method === 'PUT') {
      channel.name = body.name ?? channel.name
      channel.enabled = body.enabled ?? channel.enabled
      channel.events = body.events ?? []
      channel.config = mergeSecrets('notifier', channel.kind, body.config ?? {}, channel.config)
      channel.updatedAt = iso(Date.now())
      audit('channel.update', 'channel', channel.id, channel.name)
      return send(res, 200, channelView(channel))
    }
    if (method === 'DELETE') {
      state.channels = state.channels.filter((item) => item.id !== channel.id)
      audit('channel.delete', 'channel', channel.id, channel.name)
      return send(res, 204, '')
    }
  }

  if (path === '/settings') {
    if (method === 'GET') return send(res, 200, state.settings)
    if (method === 'PUT') {
      state.settings = { ...state.settings, ...body }
      audit('settings.update', 'settings', 'settings', 'Settings')
      return send(res, 200, state.settings)
    }
  }

  if (segments[0] === 'users') {
    if (segments.length === 1 && method === 'GET') {
      return send(res, 200, list(state.users.map((item) => ({ ...item, password: undefined })), query))
    }
    if (segments.length === 1 && method === 'POST') {
      const created = {
        id: id('usr'),
        email: body.email,
        name: body.name,
        role: body.role ?? 'viewer',
        createdAt: iso(Date.now()),
        password: body.password,
      }
      state.users.push(created)
      audit('user.create', 'user', created.id, created.email)
      return send(res, 201, { ...created, password: undefined })
    }
    const target = state.users.find((item) => item.id === segments[1])
    if (!target) return fail(res, 404, 'not_found', 'User not found')
    if (segments[2] === 'password' && method === 'PUT') {
      target.password = body.password
      audit('user.password', 'user', target.id, target.email)
      return send(res, 204, '')
    }
    if (method === 'PUT') {
      Object.assign(target, { name: body.name, email: body.email, role: body.role })
      audit('user.update', 'user', target.id, target.email)
      return send(res, 200, { ...target, password: undefined })
    }
    if (method === 'DELETE') {
      if (state.users.filter((item) => item.role === 'admin').length === 1 && target.role === 'admin') {
        return fail(res, 409, 'conflict', 'The last administrator cannot be deleted')
      }
      state.users = state.users.filter((item) => item.id !== target.id)
      audit('user.delete', 'user', target.id, target.email)
      return send(res, 204, '')
    }
  }

  if (segments[0] === 'tokens') {
    if (segments.length === 1 && method === 'GET') return send(res, 200, list(state.tokens, query))
    if (segments.length === 1 && method === 'POST') {
      const secret = `bvt_${randomUUID().replace(/-/g, '')}`
      const token = {
        id: id('tok'),
        name: body.name,
        prefix: secret.slice(0, 8),
        scopes: body.scopes ?? ['read'],
        jobSlugs: body.jobSlugs ?? [],
        createdAt: iso(Date.now()),
        createdBy: user?.email,
      }
      state.tokens.push(token)
      audit('token.create', 'token', token.id, token.name)
      return send(res, 201, { token, secret })
    }
    const token = state.tokens.find((item) => item.id === segments[1])
    if (!token) return fail(res, 404, 'not_found', 'Token not found')
    if (method === 'DELETE') {
      state.tokens = state.tokens.filter((item) => item.id !== token.id)
      audit('token.delete', 'token', token.id, token.name)
      return send(res, 204, '')
    }
  }

  if (path === '/audit') {
    let items = state.audit
    if (query.get('action')) {
      items = items.filter((entry) => entry.action.includes(query.get('action')))
    }
    if (query.get('actor')) {
      items = items.filter((entry) => entry.actorLabel.includes(query.get('actor')))
    }
    if (query.get('objectType')) {
      items = items.filter((entry) => entry.objectType === query.get('objectType'))
    }
    const since = timeParam(query, 'since')
    if (since !== null) items = items.filter((entry) => new Date(entry.time).getTime() >= since)
    return send(res, 200, list(items, query))
  }

  if (path === '/export') {
    const includeSecrets = query.get('includeSecrets') === '1'
    const yaml = [
      'sources:',
      ...state.sources.map(
        (source) =>
          `  - name: ${source.name}\n    kind: ${source.kind}\n    config:\n${Object.entries(
            includeSecrets ? source.config : maskConfig('source', source.kind, source.config),
          )
            .map(([key, value]) => `      ${key}: ${JSON.stringify(value)}`)
            .join('\n')}`,
      ),
      'destinations:',
      ...state.destinations.map((destination) => `  - name: ${destination.name}\n    kind: ${destination.kind}`),
      'jobs:',
      ...state.jobs.map((job) => `  - slug: ${job.slug}\n    name: ${job.name}\n    schedule: "${job.schedule}"`),
    ].join('\n')
    if (includeSecrets) audit('export.secrets', 'settings', 'export', 'Configuration export')
    return send(res, 200, `${yaml}\n`, { 'Content-Disposition': 'attachment; filename="backvault-config.yaml"' })
  }

  if (path === '/import' && method === 'POST') {
    const dryRun = query.get('dryRun') === '1'
    const changes = [
      { kind: 'source', name: 'Production PostgreSQL', action: 'update' },
      { kind: 'destination', name: 'Wasabi eu-central', action: 'skip' },
      { kind: 'job', name: 'production-postgres-nightly', action: 'update' },
    ]
    if (!dryRun) audit('import.apply', 'settings', 'import', 'Configuration import')
    return send(res, 200, { dryRun, changes, total: changes.length })
  }

  fail(res, 404, 'not_found', `No mock route for ${method} ${path}`)
})

seed()

server.listen(PORT, () => {
  process.stdout.write(`backvault mock API listening on http://127.0.0.1:${PORT}\n`)
})
