import { cn } from '@/lib/utils'
import type { ArtifactStatus, RunStatus, StageStatus } from '@/api/types'

type AnyStatus = RunStatus | ArtifactStatus | StageStatus | 'enabled' | 'disabled' | 'overdue' | 'ok'

const TONES: Record<string, { bg: string; text: string; dot: string; label: string }> = {
  success: { bg: 'bg-success/12', text: 'text-success', dot: 'bg-success', label: 'Success' },
  warning: { bg: 'bg-warning/15', text: 'text-warning', dot: 'bg-warning', label: 'Warning' },
  failed: { bg: 'bg-danger/12', text: 'text-danger', dot: 'bg-danger', label: 'Failed' },
  canceled: { bg: 'bg-soft/15', text: 'text-muted', dot: 'bg-soft', label: 'Canceled' },
  running: { bg: 'bg-running/14', text: 'text-running', dot: 'bg-running', label: 'Running' },
  queued: { bg: 'bg-info/12', text: 'text-info', dot: 'bg-info', label: 'Queued' },
  pending: { bg: 'bg-soft/12', text: 'text-soft', dot: 'bg-soft', label: 'Pending' },
  skipped: { bg: 'bg-soft/12', text: 'text-soft', dot: 'bg-soft', label: 'Skipped' },
  present: { bg: 'bg-success/12', text: 'text-success', dot: 'bg-success', label: 'Present' },
  missing: { bg: 'bg-danger/12', text: 'text-danger', dot: 'bg-danger', label: 'Missing' },
  pruned: { bg: 'bg-soft/15', text: 'text-muted', dot: 'bg-soft', label: 'Pruned' },
  deleted: { bg: 'bg-soft/15', text: 'text-muted', dot: 'bg-soft', label: 'Deleted' },
  enabled: { bg: 'bg-success/12', text: 'text-success', dot: 'bg-success', label: 'Enabled' },
  disabled: { bg: 'bg-soft/15', text: 'text-muted', dot: 'bg-soft', label: 'Disabled' },
  overdue: { bg: 'bg-warning/15', text: 'text-warning', dot: 'bg-warning', label: 'Overdue' },
  ok: { bg: 'bg-success/12', text: 'text-success', dot: 'bg-success', label: 'OK' },
}

export function StatusPill({
  status,
  label,
  size = 'md',
  className,
}: {
  status: AnyStatus | string
  label?: string
  size?: 'sm' | 'md'
  className?: string
}) {
  const tone = TONES[status] ?? {
    bg: 'bg-soft/12',
    text: 'text-muted',
    dot: 'bg-soft',
    label: status,
  }
  const animate = status === 'running'
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full font-medium',
        tone.bg,
        tone.text,
        size === 'sm' ? 'px-2 py-0.5 text-[11px]' : 'px-2.5 py-1 text-xs',
        className,
      )}
    >
      <span className={cn('size-1.5 rounded-full', tone.dot, animate && 'animate-pulse')} />
      {label ?? tone.label}
    </span>
  )
}

export function statusTone(status: string): string {
  return TONES[status]?.text ?? 'text-muted'
}
