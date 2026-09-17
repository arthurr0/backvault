import { useState } from 'react'
import { Check, Copy } from 'lucide-react'
import { cn, copyText } from '@/lib/utils'
import { IconButton } from './Button'

export function CopyButton({
  value,
  label = 'Copy to clipboard',
  size = 'sm',
  className,
}: {
  value: string
  label?: string
  size?: 'sm' | 'md'
  className?: string
}) {
  const [copied, setCopied] = useState(false)
  return (
    <IconButton
      label={copied ? 'Copied' : label}
      size={size}
      className={className}
      onClick={async () => {
        const ok = await copyText(value)
        if (!ok) return
        setCopied(true)
        window.setTimeout(() => setCopied(false), 1400)
      }}
    >
      {copied ? <Check className="size-4 text-success" /> : <Copy className="size-4" />}
    </IconButton>
  )
}

export function CopyField({
  value,
  className,
  monospace = true,
  truncate = true,
}: {
  value: string
  className?: string
  monospace?: boolean
  truncate?: boolean
}) {
  return (
    <div className={cn('flex items-center gap-1', className)}>
      <span
        title={value}
        className={cn(
          'min-w-0 text-[13px] text-muted',
          monospace && 'font-mono',
          truncate && 'truncate',
        )}
      >
        {value}
      </span>
      <CopyButton value={value} />
    </div>
  )
}

export function CodeBlock({ code, className }: { code: string; className?: string }) {
  return (
    <div className={cn('relative rounded-lg border border-border bg-surface-2', className)}>
      <pre className="scrollbar-thin overflow-x-auto p-3 pr-11 font-mono text-[12.5px] leading-relaxed text-text">
        {code}
      </pre>
      <div className="absolute right-1.5 top-1.5">
        <CopyButton value={code} />
      </div>
    </div>
  )
}
