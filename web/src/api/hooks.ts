import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseQueryOptions,
} from '@tanstack/react-query'
import { api } from './client'
import { qk } from './keys'
import type {
  APIToken,
  Artifact,
  AuditEntry,
  Config,
  DashboardStats,
  Destination,
  DocsIndexResponse,
  DocsPageResponse,
  DocsSearchResponse,
  DriverSpec,
  ImportResult,
  Job,
  ListResponse,
  MeResponse,
  NotificationChannel,
  RestoreRequest,
  Run,
  SetupStatus,
  Settings,
  Source,
  StorageObject,
  TestResult,
  TokenCreated,
  ToolStatus,
  User,
  VersionInfo,
} from './types'

type Filters = Record<string, string | number | boolean | undefined>

const listOf = <T,>(res: ListResponse<T> | undefined): T[] => res?.items ?? []

export function useSetupStatus() {
  return useQuery({
    queryKey: qk.setupStatus,
    queryFn: () => api.get<SetupStatus>('/setup/status'),
    retry: false,
    staleTime: 60_000,
  })
}

export function useMe(options?: { enabled?: boolean }) {
  return useQuery({
    queryKey: qk.me,
    queryFn: () => api.get<MeResponse>('/auth/me'),
    retry: false,
    staleTime: 60_000,
    enabled: options?.enabled ?? true,
  })
}

export function useLogin() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { email: string; password: string }) =>
      api.post<{ user: User }>('/auth/login', input),
    onSuccess: () => {
      void qc.invalidateQueries()
    },
  })
}

export function useLogout() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api.post<void>('/auth/logout'),
    onSuccess: () => {
      qc.clear()
    },
  })
}

export function useSetup() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { name: string; email: string; password: string }) =>
      api.post<{ user: User }>('/setup', input),
    onSuccess: () => {
      void qc.invalidateQueries()
    },
  })
}

export function useVersion() {
  return useQuery({
    queryKey: qk.version,
    queryFn: () => api.get<VersionInfo>('/meta/version'),
    staleTime: Infinity,
  })
}

function specsQuery(path: string, key: readonly unknown[]) {
  return {
    queryKey: key,
    queryFn: () => api.get<ListResponse<DriverSpec>>(path).then(listOf),
    staleTime: Infinity,
  } satisfies UseQueryOptions<DriverSpec[]>
}

export function useSourceSpecs() {
  return useQuery(specsQuery('/meta/sources', qk.sourceSpecs))
}

export function useDestinationSpecs() {
  return useQuery(specsQuery('/meta/destinations', qk.destinationSpecs))
}

export function useNotifierSpecs() {
  return useQuery(specsQuery('/meta/notifiers', qk.notifierSpecs))
}

export function useTools() {
  return useQuery({
    queryKey: qk.tools,
    queryFn: () => api.get<ListResponse<ToolStatus>>('/meta/tools').then(listOf),
    staleTime: 300_000,
  })
}

export function useTimezones() {
  return useQuery({
    queryKey: qk.timezones,
    queryFn: () => api.get<ListResponse<string>>('/meta/timezones').then(listOf),
    staleTime: Infinity,
  })
}

export function useDashboard() {
  return useQuery({
    queryKey: qk.dashboard,
    queryFn: () => api.get<DashboardStats>('/dashboard'),
    refetchInterval: 60_000,
  })
}

export function useSources() {
  return useQuery({
    queryKey: qk.sources,
    queryFn: () => api.get<ListResponse<Source>>('/sources', { query: { limit: 500 } }).then(listOf),
  })
}

export function useSource(id: string | undefined) {
  return useQuery({
    queryKey: qk.source(id ?? ''),
    queryFn: () => api.get<Source>(`/sources/${id}`),
    enabled: Boolean(id),
  })
}

export interface SourceInput {
  name: string
  kind: string
  description?: string
  config: Config
  tags?: string[]
}

export function useSaveSource(id?: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: SourceInput) =>
      id ? api.put<Source>(`/sources/${id}`, input) : api.post<Source>('/sources', input),
    onSuccess: (saved) => {
      void qc.invalidateQueries({ queryKey: qk.sources })
      void qc.invalidateQueries({ queryKey: qk.jobs })
      qc.setQueryData(qk.source(saved.id), saved)
    },
  })
}

export function useDeleteSource() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del<void>(`/sources/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.sources })
    },
  })
}

export function useTestSource() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id?: string; kind: string; config: Config }) =>
      input.id
        ? api.post<TestResult>(`/sources/${input.id}/test`, { kind: input.kind, config: input.config })
        : api.post<TestResult>('/sources/test', { kind: input.kind, config: input.config }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.sources })
    },
  })
}

export function useDestinations() {
  return useQuery({
    queryKey: qk.destinations,
    queryFn: () =>
      api.get<ListResponse<Destination>>('/destinations', { query: { limit: 500 } }).then(listOf),
  })
}

export function useDestination(id: string | undefined) {
  return useQuery({
    queryKey: qk.destination(id ?? ''),
    queryFn: () => api.get<Destination>(`/destinations/${id}`),
    enabled: Boolean(id),
  })
}

export function useSaveDestination(id?: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: SourceInput) =>
      id
        ? api.put<Destination>(`/destinations/${id}`, input)
        : api.post<Destination>('/destinations', input),
    onSuccess: (saved) => {
      void qc.invalidateQueries({ queryKey: qk.destinations })
      void qc.invalidateQueries({ queryKey: qk.jobs })
      qc.setQueryData(qk.destination(saved.id), saved)
    },
  })
}

export function useDeleteDestination() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del<void>(`/destinations/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.destinations })
    },
  })
}

export function useTestDestination() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id?: string; kind: string; config: Config }) =>
      input.id
        ? api.post<TestResult>(`/destinations/${input.id}/test`, {
            kind: input.kind,
            config: input.config,
          })
        : api.post<TestResult>('/destinations/test', { kind: input.kind, config: input.config }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.destinations })
    },
  })
}

export function useDestinationBrowse(id: string | undefined, prefix: string) {
  return useQuery({
    queryKey: qk.destinationBrowse(id ?? '', prefix),
    queryFn: () =>
      api
        .get<ListResponse<StorageObject>>(`/destinations/${id}/browse`, { query: { prefix } })
        .then(listOf),
    enabled: Boolean(id),
  })
}

export function useJobs() {
  return useQuery({
    queryKey: qk.jobs,
    queryFn: () => api.get<ListResponse<Job>>('/jobs', { query: { limit: 500 } }).then(listOf),
  })
}

export function useJob(id: string | undefined) {
  return useQuery({
    queryKey: qk.job(id ?? ''),
    queryFn: () => api.get<Job>(`/jobs/${id}`),
    enabled: Boolean(id),
  })
}

export type JobInput = Partial<Omit<Job, 'id' | 'createdAt' | 'updatedAt'>>

export function useSaveJob(id?: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: JobInput) =>
      id ? api.put<Job>(`/jobs/${id}`, input) : api.post<Job>('/jobs', input),
    onSuccess: (saved) => {
      void qc.invalidateQueries({ queryKey: qk.jobs })
      void qc.invalidateQueries({ queryKey: qk.dashboard })
      qc.setQueryData(qk.job(saved.slug), saved)
    },
  })
}

export function useDeleteJob() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string; deleteArtifacts?: boolean }) =>
      api.del<void>(`/jobs/${input.id}`, {
        query: input.deleteArtifacts ? { deleteArtifacts: 1 } : undefined,
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.jobs })
      void qc.invalidateQueries({ queryKey: qk.dashboard })
    },
  })
}

export function useRunJob() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post<{ run: Run }>(`/jobs/${id}/run`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['runs'] })
      void qc.invalidateQueries({ queryKey: qk.jobs })
      void qc.invalidateQueries({ queryKey: qk.dashboard })
    },
  })
}

export function useSetJobEnabled() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string; enabled: boolean }) =>
      api.post<Job>(`/jobs/${input.id}/${input.enabled ? 'enable' : 'disable'}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.jobs })
      void qc.invalidateQueries({ queryKey: qk.dashboard })
    },
  })
}

export function usePruneJob() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post<{ run: Run }>(`/jobs/${id}/prune`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['runs'] })
      void qc.invalidateQueries({ queryKey: ['artifacts'] })
    },
  })
}

export function useDuplicateJob() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post<Job>(`/jobs/${id}/duplicate`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.jobs })
    },
  })
}

export function useJobRuns(id: string | undefined) {
  return useQuery({
    queryKey: qk.jobRuns(id ?? ''),
    queryFn: () => api.get<ListResponse<Run>>(`/jobs/${id}/runs`, { query: { limit: 50 } }).then(listOf),
    enabled: Boolean(id),
  })
}

export function useJobArtifacts(id: string | undefined) {
  return useQuery({
    queryKey: qk.jobArtifacts(id ?? ''),
    queryFn: () =>
      api.get<ListResponse<Artifact>>(`/jobs/${id}/artifacts`, { query: { limit: 100 } }).then(listOf),
    enabled: Boolean(id),
  })
}

export function useRuns(filters: Filters = {}) {
  return useQuery({
    queryKey: qk.runs(filters),
    queryFn: () => api.get<ListResponse<Run>>('/runs', { query: { limit: 100, ...filters } }),
  })
}

export function useRun(id: string | undefined) {
  return useQuery({
    queryKey: qk.run(id ?? ''),
    queryFn: () => api.get<Run>(`/runs/${id}`),
    enabled: Boolean(id),
  })
}

export function useCancelRun() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post<Run>(`/runs/${id}/cancel`),
    onSuccess: (run) => {
      qc.setQueryData(qk.run(run.id), run)
      void qc.invalidateQueries({ queryKey: ['runs'] })
    },
  })
}

export function useArtifacts(filters: Filters = {}) {
  return useQuery({
    queryKey: qk.artifacts(filters),
    queryFn: () => api.get<ListResponse<Artifact>>('/artifacts', { query: { limit: 100, ...filters } }),
  })
}

export function useArtifact(id: string | undefined) {
  return useQuery({
    queryKey: qk.artifact(id ?? ''),
    queryFn: () => api.get<Artifact>(`/artifacts/${id}`),
    enabled: Boolean(id),
  })
}

export function useDeleteArtifact() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del<void>(`/artifacts/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['artifacts'] })
      void qc.invalidateQueries({ queryKey: qk.destinations })
    },
  })
}

export function useVerifyArtifact() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post<{ run: Run }>(`/artifacts/${id}/verify`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['runs'] })
      void qc.invalidateQueries({ queryKey: ['artifacts'] })
    },
  })
}

export function useRestoreArtifact() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string } & RestoreRequest) => {
      const { id, ...body } = input
      return api.post<{ run: Run }>(`/artifacts/${id}/restore`, body)
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['runs'] })
    },
  })
}

export function useChannels() {
  return useQuery({
    queryKey: qk.channels,
    queryFn: () =>
      api
        .get<ListResponse<NotificationChannel>>('/notifications/channels', { query: { limit: 200 } })
        .then(listOf),
  })
}

export interface ChannelInput {
  name: string
  kind: string
  config: Config
  enabled: boolean
  events: string[]
}

export function useSaveChannel(id?: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: ChannelInput) =>
      id
        ? api.put<NotificationChannel>(`/notifications/channels/${id}`, input)
        : api.post<NotificationChannel>('/notifications/channels', input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.channels })
    },
  })
}

export function useDeleteChannel() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del<void>(`/notifications/channels/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.channels })
    },
  })
}

export function useTestChannel() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id?: string; kind: string; config: Config }) =>
      input.id
        ? api.post<TestResult>(`/notifications/channels/${input.id}/test`, {
            kind: input.kind,
            config: input.config,
          })
        : api.post<TestResult>('/notifications/channels/test', {
            kind: input.kind,
            config: input.config,
          }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.channels })
    },
  })
}

export function useSettings() {
  return useQuery({
    queryKey: qk.settings,
    queryFn: () => api.get<Settings>('/settings'),
  })
}

export function useSaveSettings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: Settings) => api.put<Settings>('/settings', input),
    onSuccess: (saved) => {
      qc.setQueryData(qk.settings, saved)
    },
  })
}

export function useUsers() {
  return useQuery({
    queryKey: qk.users,
    queryFn: () => api.get<ListResponse<User>>('/users').then(listOf),
  })
}

export function useSaveUser(id?: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { name: string; email: string; role: string; password?: string }) =>
      id ? api.put<User>(`/users/${id}`, input) : api.post<User>('/users', input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.users })
    },
  })
}

export function useDeleteUser() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del<void>(`/users/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.users })
    },
  })
}

export function useChangePassword() {
  return useMutation({
    mutationFn: (input: { id: string; currentPassword?: string; password: string }) =>
      api.put<void>(`/users/${input.id}/password`, {
        currentPassword: input.currentPassword,
        password: input.password,
      }),
  })
}

export function useTokens() {
  return useQuery({
    queryKey: qk.tokens,
    queryFn: () => api.get<ListResponse<APIToken>>('/tokens').then(listOf),
  })
}

export function useCreateToken() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { name: string; scopes: string[]; jobSlugs: string[]; expiresAt?: string }) =>
      api.post<TokenCreated>('/tokens', input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.tokens })
    },
  })
}

export function useDeleteToken() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del<void>(`/tokens/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.tokens })
    },
  })
}

export function useAudit(filters: Filters = {}) {
  return useQuery({
    queryKey: qk.audit(filters),
    queryFn: () => api.get<ListResponse<AuditEntry>>('/audit', { query: { limit: 100, ...filters } }),
  })
}

export function useImportConfig() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { yaml: string; dryRun: boolean }) =>
      fetch(`/api/v1/import${input.dryRun ? '?dryRun=1' : ''}`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'X-Requested-With': 'backvault', 'Content-Type': 'application/yaml' },
        body: input.yaml,
      }).then(async (res) => {
        const text = await res.text()
        if (!res.ok) throw new Error(text || `Import failed with status ${res.status}`)
        return JSON.parse(text) as ImportResult
      }),
    onSuccess: (_result, input) => {
      if (!input.dryRun) void qc.invalidateQueries()
    },
  })
}

export function useDocsIndex() {
  return useQuery({
    queryKey: qk.docsIndex,
    queryFn: () => api.get<DocsIndexResponse>('/docs'),
    staleTime: Infinity,
  })
}

export function useDocsPage(path: string | undefined) {
  return useQuery({
    queryKey: qk.docsPage(path ?? ''),
    queryFn: () => api.get<DocsPageResponse>('/docs/page', { query: { path } }),
    enabled: Boolean(path),
    staleTime: Infinity,
    retry: false,
  })
}

export function useDocsSearch(query: string) {
  const trimmed = query.trim()
  return useQuery({
    queryKey: qk.docsSearch(trimmed),
    queryFn: () => api.get<DocsSearchResponse>('/docs/search', { query: { q: trimmed } }).then((res) => res.items),
    enabled: trimmed.length > 1,
    staleTime: 60_000,
  })
}
