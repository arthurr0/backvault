export const SECRET_MASK = '********'

export type FieldType =
  | 'string'
  | 'text'
  | 'secret'
  | 'int'
  | 'bool'
  | 'select'
  | 'list'
  | 'path'
  | 'port'

export interface FieldOption {
  value: string
  label: string
}

export type ConfigValue = string | number | boolean | string[] | null | undefined

export type Config = Record<string, ConfigValue>

export interface Field {
  name: string
  label: string
  type: FieldType
  required: boolean
  secret: boolean
  default?: ConfigValue
  placeholder?: string
  help?: string
  options?: FieldOption[]
  group?: string
  advanced?: boolean
  showIf?: Record<string, ConfigValue>
}

export type Capability = 'restore' | 'browse' | 'ingest' | 'test'

export interface DriverSpec {
  kind: string
  label: string
  description: string
  icon: string
  category: string
  fields: Field[]
  capabilities: string[]
  tools?: string[]
  docsUrl?: string
}

export type Compression = 'none' | 'gzip' | 'zstd'
export type Encryption = 'none' | 'age'

export interface Retention {
  keepLast: number
  keepHourly: number
  keepDaily: number
  keepWeekly: number
  keepMonthly: number
  keepYearly: number
  maxAgeDays: number
}

export interface Source {
  id: string
  name: string
  kind: string
  description: string
  config: Config
  tags: string[]
  createdAt: string
  updatedAt: string
  lastTestAt?: string
  lastTestOk?: boolean
  lastTestError?: string
  jobCount: number
}

export interface Destination {
  id: string
  name: string
  kind: string
  description: string
  config: Config
  tags: string[]
  createdAt: string
  updatedAt: string
  lastTestAt?: string
  lastTestOk?: boolean
  lastTestError?: string
  jobCount: number
  usedBytes: number
  artifactCount: number
}

export const ALL_EVENTS = [
  'run.success',
  'run.failed',
  'run.warning',
  'job.overdue',
  'artifact.missing',
  'prune.done',
  'restore.done',
  'restore.failed',
] as const

export type NotifyEvent = (typeof ALL_EVENTS)[number]

export type RunKind = 'backup' | 'restore' | 'prune' | 'verify' | 'ingest'
export type RunTrigger = 'schedule' | 'manual' | 'api' | 'ingest' | 'retry'
export type RunStatus = 'queued' | 'running' | 'success' | 'warning' | 'failed' | 'canceled'
export type StageStatus = 'pending' | 'running' | 'success' | 'failed' | 'skipped'

export interface Stage {
  name: string
  status: StageStatus
  startedAt?: string
  finishedAt?: string
  message?: string
}

export interface RunSummary {
  id: string
  kind: RunKind
  status: RunStatus
  startedAt?: string
  finishedAt?: string
  durationMs: number
  bytes: number
  error?: string
}

export interface Run {
  id: string
  jobId: string
  jobSlug: string
  jobName: string
  kind: RunKind
  trigger: RunTrigger
  status: RunStatus
  queuedAt: string
  startedAt?: string
  finishedAt?: string
  durationMs: number
  bytes: number
  rawBytes: number
  sha256?: string
  filename?: string
  error?: string
  stages: Stage[]
  artifactIds: string[]
  attempt: number
  meta?: Record<string, string>
  createdBy?: string
}

export interface Job {
  id: string
  slug: string
  name: string
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
  createdAt: string
  updatedAt: string
  sourceName?: string
  sourceKind?: string
  destinationNames?: string[]
  lastRun?: RunSummary
  nextRunAt?: string
  overdue: boolean
  artifactCount: number
  totalBytes: number
}

export type ArtifactStatus = 'present' | 'missing' | 'pruned' | 'deleted'

export interface Artifact {
  id: string
  jobId: string
  jobSlug: string
  jobName?: string
  runId: string
  destinationId: string
  destinationName?: string
  destinationKind?: string
  path: string
  filename: string
  size: number
  sha256: string
  compression: Compression
  encryption: Encryption
  sourceKind: string
  extension: string
  status: ArtifactStatus
  createdAt: string
  verifiedAt?: string
  deletedAt?: string
  meta?: Record<string, string>
}

export interface NotificationChannel {
  id: string
  name: string
  kind: string
  config: Config
  enabled: boolean
  events: string[]
  createdAt: string
  updatedAt: string
  lastSentAt?: string
  lastError?: string
}

export type UserRole = 'admin' | 'viewer'

export interface User {
  id: string
  email: string
  name: string
  role: UserRole
  createdAt: string
  lastLoginAt?: string
}

export type Scope = 'admin' | 'read' | 'ingest' | 'run'

export interface APIToken {
  id: string
  name: string
  prefix: string
  scopes: string[]
  jobSlugs: string[]
  createdAt: string
  createdBy?: string
  lastUsedAt?: string
  expiresAt?: string
}

export interface AuditEntry {
  id: string
  time: string
  actorId: string
  actorLabel: string
  action: string
  objectType: string
  objectId: string
  objectName: string
  details?: Record<string, unknown>
  ip?: string
}

export interface Settings {
  siteName: string
  baseUrl: string
  defaultTimezone: string
  maxConcurrentRuns: number
  defaultRetention: Retention
  runHistoryDays: number
  auditHistoryDays: number
  overdueCheckMinutes: number
  defaultNotifyOn: string[]
}

export interface ToolStatus {
  name: string
  available: boolean
  path?: string
  version?: string
  usedBy: string[]
}

export interface DestinationUsage {
  destinationId: string
  destinationName: string
  kind: string
  bytes: number
  artifacts: number
}

export interface DailyStat {
  date: string
  success: number
  failed: number
  bytes: number
}

export interface DashboardStats {
  jobs: number
  jobsEnabled: number
  jobsOverdue: number
  jobsFailing: number
  runsRunning: number
  runs24hSuccess: number
  runs24hFailed: number
  artifacts: number
  totalBytes: number
  destinations: DestinationUsage[]
  daily: DailyStat[]
  recentRuns: Run[]
  upcoming: Job[]
  problemJobs: Job[]
}

export interface VersionInfo {
  version: string
  commit: string
  buildDate: string
  goVersion: string
  startedAt: string
}

export interface StorageObject {
  path: string
  size: number
  modTime: string
  isDir: boolean
}

export interface ListResponse<T> {
  items: T[]
  total: number
}

export interface TestResult {
  ok: boolean
  message: string
  durationMs: number
}

export interface SetupStatus {
  needsSetup: boolean
}

export interface MeResponse {
  user: User
  authType: 'session' | 'token'
  scopes: string[]
}

export interface TokenCreated {
  token: APIToken
  secret: string
}

export interface ApiErrorBody {
  error: {
    code: string
    message: string
    fields?: Record<string, string>
  }
}

export type RestoreMode = 'path' | 'source'

export interface RestoreRequest {
  mode: RestoreMode
  targetPath?: string
  extract?: boolean
  targetSourceId?: string
  passphrase?: string
  params?: Config
}

export interface ImportPlanEntry {
  kind: string
  name: string
  action: string
  note?: string
}

export interface ImportResult {
  dryRun: boolean
  changes: ImportPlanEntry[]
  total: number
}

export interface DocsHeading {
  level: number
  text: string
  id: string
}

export interface DocsIndexItem {
  path: string
  title: string
  section: string
  description: string
  headings: DocsHeading[]
}

export interface DocsIndexResponse {
  items: DocsIndexItem[]
  sections: string[]
}

export interface DocsLink {
  path: string
  title: string
}

export interface DocsPageResponse {
  path: string
  title: string
  section: string
  headings: DocsHeading[]
  markdown: string
  words: number
  prev: DocsLink | null
  next: DocsLink | null
}

export interface DocsSearchHit {
  path: string
  title: string
  section: string
  snippet: string
  score: number
}

export interface DocsSearchResponse {
  items: DocsSearchHit[]
}
