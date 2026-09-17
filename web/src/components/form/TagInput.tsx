import { useRef, useState, type ClipboardEvent, type KeyboardEvent } from 'react'
import { X } from 'lucide-react'
import { cn } from '@/lib/utils'

export interface TagInputProps {
  value: string[]
  onChange: (value: string[]) => void
  placeholder?: string
  id?: string
  disabled?: boolean
  invalid?: boolean
}

function splitPaste(text: string): string[] {
  return text
    .split(/[\n\r\t,;]+/)
    .map((part) => part.trim())
    .filter(Boolean)
}

export function TagInput({ value, onChange, placeholder, id, disabled, invalid }: TagInputProps) {
  const [draft, setDraft] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  const add = (items: string[]) => {
    const next = [...value]
    for (const item of items) {
      if (item && !next.includes(item)) next.push(item)
    }
    onChange(next)
  }

  const commitDraft = () => {
    const trimmed = draft.trim()
    if (!trimmed) return
    add([trimmed])
    setDraft('')
  }

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter' || event.key === ',') {
      event.preventDefault()
      commitDraft()
      return
    }
    if (event.key === 'Backspace' && !draft && value.length) {
      onChange(value.slice(0, -1))
    }
  }

  const onPaste = (event: ClipboardEvent<HTMLInputElement>) => {
    const text = event.clipboardData.getData('text')
    if (!/[\n\r\t,;]/.test(text)) return
    event.preventDefault()
    add(splitPaste(text))
    setDraft('')
  }

  return (
    <div
      className={cn(
        'input-base flex min-h-9 flex-wrap items-center gap-1.5 py-1.5',
        invalid && 'border-danger',
        disabled && 'opacity-60',
      )}
      onClick={() => inputRef.current?.focus()}
    >
      {value.map((item) => (
        <span
          key={item}
          className="inline-flex max-w-full items-center gap-1 rounded-md bg-surface-3 px-1.5 py-0.5 font-mono text-[12px] text-text"
        >
          <span className="truncate">{item}</span>
          <button
            type="button"
            aria-label={`Remove ${item}`}
            disabled={disabled}
            onClick={(event) => {
              event.stopPropagation()
              onChange(value.filter((v) => v !== item))
            }}
            className="text-soft hover:text-danger"
          >
            <X className="size-3" />
          </button>
        </span>
      ))}
      <input
        ref={inputRef}
        id={id}
        disabled={disabled}
        value={draft}
        placeholder={value.length ? '' : placeholder}
        onChange={(event) => setDraft(event.target.value)}
        onKeyDown={onKeyDown}
        onPaste={onPaste}
        onBlur={commitDraft}
        className="min-w-[8rem] flex-1 bg-transparent text-sm outline-none placeholder:text-soft"
      />
    </div>
  )
}
