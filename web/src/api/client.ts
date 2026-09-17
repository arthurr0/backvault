import type { ApiErrorBody } from './types'

export const API_BASE = '/api/v1'

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly fields: Record<string, string>

  constructor(status: number, code: string, message: string, fields?: Record<string, string>) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.fields = fields ?? {}
  }

  get isValidation(): boolean {
    return this.code === 'validation_failed' || this.status === 400
  }

  get isLocked(): boolean {
    return this.status === 423
  }

  get isConflict(): boolean {
    return this.status === 409
  }
}

const AUTH_PATHS = ['/auth/login', '/auth/me', '/setup', '/setup/status', '/auth/logout']

function isAuthPath(path: string): boolean {
  return AUTH_PATHS.some((p) => path === p || path.startsWith(`${p}?`))
}

function redirectToLogin() {
  const here = window.location.pathname + window.location.search
  if (window.location.pathname === '/login' || window.location.pathname === '/setup') return
  const next = here && here !== '/' ? `?next=${encodeURIComponent(here)}` : ''
  window.location.assign(`/login${next}`)
}

export interface RequestOptions {
  method?: string
  body?: unknown
  query?: Record<string, string | number | boolean | undefined | null>
  signal?: AbortSignal
  raw?: boolean
  headers?: Record<string, string>
}

export function buildUrl(path: string, query?: RequestOptions['query']): string {
  const url = `${API_BASE}${path}`
  if (!query) return url
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null || value === '') continue
    params.set(key, String(value))
  }
  const qs = params.toString()
  return qs ? `${url}?${qs}` : url
}

async function toApiError(res: Response): Promise<ApiError> {
  let code = 'internal'
  let message = `Request failed with status ${res.status}`
  let fields: Record<string, string> | undefined
  try {
    const data = (await res.json()) as Partial<ApiErrorBody>
    if (data && data.error) {
      code = data.error.code || code
      message = data.error.message || message
      fields = data.error.fields
    }
  } catch {
    if (res.status === 401) message = 'Your session has expired'
  }
  return new ApiError(res.status, code, message, fields)
}

export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { method = 'GET', body, query, signal, raw, headers } = options
  const init: RequestInit = {
    method,
    credentials: 'include',
    signal: signal ?? null,
    headers: {
      'X-Requested-With': 'backvault',
      Accept: raw ? 'text/plain, */*' : 'application/json',
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      ...headers,
    },
  }
  if (body !== undefined) init.body = JSON.stringify(body)

  const res = await fetch(buildUrl(path, query), init)

  if (res.status === 401 && !isAuthPath(path)) {
    redirectToLogin()
    throw new ApiError(401, 'unauthenticated', 'Your session has expired')
  }

  if (!res.ok) throw await toApiError(res)

  if (res.status === 204) return undefined as T
  if (raw) return (await res.text()) as unknown as T

  const text = await res.text()
  if (!text) return undefined as T
  return JSON.parse(text) as T
}

export const api = {
  get: <T,>(path: string, options?: Omit<RequestOptions, 'method' | 'body'>) =>
    request<T>(path, { ...options, method: 'GET' }),
  post: <T,>(path: string, body?: unknown, options?: Omit<RequestOptions, 'method' | 'body'>) =>
    request<T>(path, { ...options, method: 'POST', body }),
  put: <T,>(path: string, body?: unknown, options?: Omit<RequestOptions, 'method' | 'body'>) =>
    request<T>(path, { ...options, method: 'PUT', body }),
  del: <T,>(path: string, options?: Omit<RequestOptions, 'method' | 'body'>) =>
    request<T>(path, { ...options, method: 'DELETE' }),
}

export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message
  if (err instanceof Error) return err.message
  return 'Something went wrong'
}

export function fieldErrors(err: unknown): Record<string, string> {
  return err instanceof ApiError ? err.fields : {}
}

export function downloadUrl(path: string, query?: RequestOptions['query']): string {
  return buildUrl(path, query)
}
