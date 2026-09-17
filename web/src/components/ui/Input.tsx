import { forwardRef, type InputHTMLAttributes, type SelectHTMLAttributes, type TextareaHTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(
  function Input({ className, ...rest }, ref) {
    return <input ref={ref} className={cn('input-base', className)} {...rest} />
  },
)

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement>>(
  function Textarea({ className, ...rest }, ref) {
    return <textarea ref={ref} className={cn('input-base min-h-20 resize-y', className)} {...rest} />
  },
)

export const Select = forwardRef<HTMLSelectElement, SelectHTMLAttributes<HTMLSelectElement>>(
  function Select({ className, children, ...rest }, ref) {
    return (
      <select ref={ref} className={cn('input-base cursor-pointer pr-8', className)} {...rest}>
        {children}
      </select>
    )
  },
)

export interface SwitchProps {
  checked: boolean
  onChange: (value: boolean) => void
  id?: string
  label?: string
  disabled?: boolean
}

export function Switch({ checked, onChange, id, label, disabled }: SwitchProps) {
  return (
    <button
      type="button"
      id={id}
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={cn(
        'relative inline-flex h-6 w-11 shrink-0 items-center rounded-full border transition-colors',
        checked ? 'border-transparent bg-accent' : 'border-border-strong bg-surface-3',
        disabled && 'cursor-not-allowed opacity-50',
      )}
    >
      <span
        className={cn(
          'ml-0.5 size-5 rounded-full bg-paper shadow transition-transform',
          checked ? 'translate-x-5' : 'translate-x-0',
        )}
        style={{ background: checked ? 'var(--ink)' : 'var(--paper)' }}
      />
    </button>
  )
}

export interface CheckboxProps {
  checked: boolean
  onChange: (value: boolean) => void
  id?: string
  label?: string
  disabled?: boolean
}

export function Checkbox({ checked, onChange, id, label, disabled }: CheckboxProps) {
  return (
    <input
      id={id}
      type="checkbox"
      aria-label={label}
      checked={checked}
      disabled={disabled}
      onChange={(e) => onChange(e.target.checked)}
      className="size-4 cursor-pointer rounded border-border-strong accent-[var(--brass)]"
    />
  )
}
