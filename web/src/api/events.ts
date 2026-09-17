import { useEffect, useRef, useState } from 'react'
import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import { API_BASE } from './client'
import { qk } from './keys'
import type { Artifact, DashboardStats, Job, ListResponse, Run } from './types'

type ConnectionState = 'connecting' | 'open' | 'closed'

function mergeRun(qc: QueryClient, run: Run) {
  qc.setQueryData(qk.run(run.id), run)
  qc.setQueriesData<ListResponse<Run>>({ queryKey: ['runs'] }, (prev) => {
    if (!prev || !Array.isArray(prev.items)) return prev
    const idx = prev.items.findIndex((r) => r.id === run.id)
    if (idx === -1) return { items: [run, ...prev.items].slice(0, 200), total: prev.total + 1 }
    const items = prev.items.slice()
    items[idx] = run
    return { ...prev, items }
  })
  qc.setQueryData<DashboardStats>(qk.dashboard, (prev) => {
    if (!prev) return prev
    const idx = prev.recentRuns.findIndex((r) => r.id === run.id)
    const recentRuns =
      idx === -1
        ? [run, ...prev.recentRuns].slice(0, 12)
        : prev.recentRuns.map((r) => (r.id === run.id ? run : r))
    const runsRunning = recentRuns.filter((r) => r.status === 'running' || r.status === 'queued').length
    return { ...prev, recentRuns, runsRunning }
  })
  qc.setQueriesData<Run[]>({ queryKey: ['jobs', run.jobSlug, 'runs'] }, (prev) => {
    if (!Array.isArray(prev)) return prev
    const idx = prev.findIndex((r) => r.id === run.id)
    if (idx === -1) return [run, ...prev]
    const next = prev.slice()
    next[idx] = run
    return next
  })
  if (run.status !== 'running' && run.status !== 'queued') {
    void qc.invalidateQueries({ queryKey: qk.jobs })
    void qc.invalidateQueries({ queryKey: ['artifacts'] })
    void qc.invalidateQueries({ queryKey: qk.dashboard })
  }
}

function mergeJob(qc: QueryClient, job: Job) {
  qc.setQueryData(qk.job(job.slug), job)
  qc.setQueryData(qk.job(job.id), job)
  qc.setQueryData<Job[]>(qk.jobs, (prev) => {
    if (!Array.isArray(prev)) return prev
    const idx = prev.findIndex((j) => j.id === job.id)
    if (idx === -1) return [...prev, job]
    const next = prev.slice()
    next[idx] = job
    return next
  })
}

function mergeArtifact(qc: QueryClient, artifact: Artifact) {
  qc.setQueryData(qk.artifact(artifact.id), artifact)
  qc.setQueriesData<ListResponse<Artifact>>({ queryKey: ['artifacts'] }, (prev) => {
    if (!prev || !Array.isArray(prev.items)) return prev
    const idx = prev.items.findIndex((a) => a.id === artifact.id)
    if (idx === -1) return { items: [artifact, ...prev.items], total: prev.total + 1 }
    const items = prev.items.slice()
    items[idx] = artifact
    return { ...prev, items }
  })
}

export function useEventStream(enabled: boolean): ConnectionState {
  const qc = useQueryClient()
  const [state, setState] = useState<ConnectionState>('closed')

  useEffect(() => {
    if (!enabled) return
    let closed = false
    let source: EventSource | null = null
    let retry: ReturnType<typeof setTimeout> | undefined
    let attempts = 0

    const parse = <T,>(raw: string): T | null => {
      try {
        return JSON.parse(raw) as T
      } catch {
        return null
      }
    }

    const connect = () => {
      if (closed) return
      setState('connecting')
      source = new EventSource(`${API_BASE}/events/stream`, { withCredentials: true })
      source.onopen = () => {
        attempts = 0
        setState('open')
      }
      source.addEventListener('run.updated', (ev) => {
        const run = parse<Run>((ev as MessageEvent<string>).data)
        if (run) mergeRun(qc, run)
      })
      source.addEventListener('job.updated', (ev) => {
        const job = parse<Job>((ev as MessageEvent<string>).data)
        if (job) mergeJob(qc, job)
      })
      source.addEventListener('artifact.updated', (ev) => {
        const artifact = parse<Artifact>((ev as MessageEvent<string>).data)
        if (artifact) mergeArtifact(qc, artifact)
      })
      source.onerror = () => {
        source?.close()
        source = null
        setState('closed')
        if (closed) return
        attempts += 1
        retry = setTimeout(connect, Math.min(1000 * 2 ** attempts, 30_000))
      }
    }

    connect()
    return () => {
      closed = true
      if (retry) clearTimeout(retry)
      source?.close()
    }
  }, [enabled, qc])

  return enabled ? state : 'closed'
}

export interface LogStream {
  lines: string[]
  done: boolean
  connected: boolean
}

const ESCAPE = String.fromCharCode(27)
const ANSI_PATTERN = new RegExp(`${ESCAPE}\\[[0-9;]*[A-Za-z]`, 'g')

export function stripAnsi(input: string): string {
  return input.replace(ANSI_PATTERN, '')
}

export function useRunLogStream(runId: string | undefined, live: boolean): LogStream {
  const [state, setState] = useState<{ runId: string; lines: string[]; done: boolean }>({
    runId: runId ?? '',
    lines: [],
    done: false,
  })
  const [connected, setConnected] = useState(false)
  const buffer = useRef<string[]>([])

  useEffect(() => {
    if (!runId || live) return
    let aborted = false
    const controller = new AbortController()
    buffer.current = []
    fetch(`${API_BASE}/runs/${runId}/log`, {
      credentials: 'include',
      headers: { 'X-Requested-With': 'backvault' },
      signal: controller.signal,
    })
      .then((res) => (res.ok ? res.text() : ''))
      .then((text) => {
        if (aborted) return
        const initial = stripAnsi(text).split('\n')
        while (initial.length && initial[initial.length - 1] === '') initial.pop()
        setState({ runId, lines: initial, done: true })
      })
      .catch(() => undefined)
    return () => {
      aborted = true
      controller.abort()
    }
  }, [runId, live])

  useEffect(() => {
    if (!runId || !live) return
    buffer.current = []
    const source = new EventSource(`${API_BASE}/runs/${runId}/log/stream`, { withCredentials: true })
    source.onopen = () => {
      setState({ runId, lines: [], done: false })
      setConnected(true)
    }
    source.addEventListener('line', (ev) => {
      buffer.current.push(stripAnsi((ev as MessageEvent<string>).data))
    })
    source.addEventListener('done', () => {
      setState((prev) => ({ ...prev, done: true }))
      source.close()
      setConnected(false)
    })
    source.onerror = () => setConnected(false)
    const flush = setInterval(() => {
      if (!buffer.current.length) return
      const chunk = buffer.current
      buffer.current = []
      setState((prev) => ({ ...prev, lines: [...prev.lines, ...chunk] }))
    }, 200)
    return () => {
      source.close()
      clearInterval(flush)
      setConnected(false)
    }
  }, [runId, live])

  const fresh = state.runId === (runId ?? '')
  return {
    lines: fresh ? state.lines : [],
    done: fresh ? state.done : false,
    connected: live && connected,
  }
}
