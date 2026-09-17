import { useEffect, useMemo, useRef, useState } from 'react'
import { ArrowDown, Download, Pause, Play, Search, X } from 'lucide-react'
import { useRunLogStream } from '@/api/events'
import { downloadUrl } from '@/api/client'
import { Button, IconButton } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { cn } from '@/lib/utils'

function lineTone(line: string): string {
  const lower = line.toLowerCase()
  if (lower.includes('level=error') || lower.includes(' error ') || lower.startsWith('error')) {
    return 'text-danger'
  }
  if (lower.includes('level=warn') || lower.includes(' warn ')) return 'text-warning'
  if (lower.includes('level=debug')) return 'text-soft'
  return 'text-text'
}

export function LogViewer({ runId, live }: { runId: string; live: boolean }) {
  const [paused, setPaused] = useState(false)
  const [autoScroll, setAutoScroll] = useState(true)
  const [query, setQuery] = useState('')
  const stream = useRunLogStream(runId, live && !paused)
  const containerRef = useRef<HTMLDivElement>(null)

  const visible = useMemo(() => {
    if (!query.trim()) return stream.lines
    const needle = query.trim().toLowerCase()
    return stream.lines.filter((line) => line.toLowerCase().includes(needle))
  }, [stream.lines, query])

  useEffect(() => {
    if (!autoScroll) return
    const node = containerRef.current
    if (!node) return
    node.scrollTop = node.scrollHeight
  }, [visible, autoScroll])

  return (
    <div className="flex h-full flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2">
        <div className="relative min-w-40 flex-1">
          <Search
            className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-soft"
            aria-hidden="true"
          />
          <Input
            aria-label="Search the log"
            placeholder="Search the log"
            className="h-8 pl-8 text-[13px]"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
          {query ? (
            <button
              type="button"
              aria-label="Clear the log search"
              onClick={() => setQuery('')}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-soft hover:text-text"
            >
              <X className="size-3.5" />
            </button>
          ) : null}
        </div>
        {live ? (
          <Button
            size="sm"
            icon={paused ? <Play className="size-3.5" /> : <Pause className="size-3.5" />}
            onClick={() => setPaused((value) => !value)}
          >
            {paused ? 'Resume' : 'Pause'}
          </Button>
        ) : null}
        <Button
          size="sm"
          icon={<ArrowDown className="size-3.5" />}
          variant={autoScroll ? 'primary' : 'secondary'}
          onClick={() => setAutoScroll((value) => !value)}
        >
          Auto-scroll
        </Button>
        <a href={downloadUrl(`/runs/${runId}/log`)} download={`backvault-run-${runId}.log`}>
          <IconButton label="Download the log" size="sm">
            <Download className="size-4" />
          </IconButton>
        </a>
      </div>
      <div
        ref={containerRef}
        onScroll={(event) => {
          const node = event.currentTarget
          const atBottom = node.scrollHeight - node.scrollTop - node.clientHeight < 32
          if (!atBottom && autoScroll) setAutoScroll(false)
        }}
        className="scrollbar-thin h-[28rem] overflow-auto bg-surface-2 p-3 font-mono text-[12.5px] leading-[1.55]"
      >
        {visible.length === 0 ? (
          <p className="text-soft">
            {query ? 'No lines match this search.' : 'The log is empty so far.'}
          </p>
        ) : (
          visible.map((line, index) => (
            <div key={`${index}-${line.slice(0, 24)}`} className={cn('whitespace-pre-wrap', lineTone(line))}>
              {line || ' '}
            </div>
          ))
        )}
      </div>
      <div className="flex items-center justify-between gap-2 border-t border-border px-3 py-1.5 text-[11px] text-soft">
        <span>
          {visible.length} {visible.length === 1 ? 'line' : 'lines'}
          {query ? ` of ${stream.lines.length}` : ''}
        </span>
        <span>
          {live ? (paused ? 'Paused' : stream.connected ? 'Streaming' : 'Reconnecting') : 'Finished'}
        </span>
      </div>
    </div>
  )
}
