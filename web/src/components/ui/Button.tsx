import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from 'react'
import { LoaderCircle } from 'lucide-react'
import { cn } from '@/lib/utils'

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger' | 'subtle'
export type ButtonSize = 'sm' | 'md'

const VARIANTS: Record<ButtonVariant, string> = {
  primary:
    'bg-accent text-accent-contrast hover:bg-accent-hover border border-transparent font-medium shadow-sm',
  secondary: 'bg-surface text-text border border-border-strong hover:bg-surface-3',
  ghost: 'bg-transparent text-muted border border-transparent hover:bg-surface-3 hover:text-text',
  subtle: 'bg-surface-3 text-text border border-transparent hover:bg-border',
  danger: 'bg-danger text-white border border-transparent hover:opacity-90 font-medium',
}

const SIZES: Record<ButtonSize, string> = {
  sm: 'h-8 px-2.5 text-[13px] gap-1.5 rounded-lg',
  md: 'h-9 px-3.5 text-sm gap-2 rounded-lg',
}

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant
  size?: ButtonSize
  loading?: boolean
  icon?: ReactNode
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = 'secondary', size = 'md', loading, icon, className, children, disabled, ...rest },
  ref,
) {
  return (
    <button
      ref={ref}
      type={rest.type ?? 'button'}
      disabled={disabled || loading}
      className={cn(
        'inline-flex select-none items-center justify-center whitespace-nowrap transition-colors',
        'disabled:cursor-not-allowed disabled:opacity-50',
        VARIANTS[variant],
        SIZES[size],
        className,
      )}
      {...rest}
    >
      {loading ? (
        <LoaderCircle aria-hidden="true" className="size-4 animate-spin" />
      ) : (
        icon
      )}
      {children}
    </button>
  )
})

export interface IconButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  label: string
  variant?: ButtonVariant
  size?: ButtonSize
  children: ReactNode
}

export const IconButton = forwardRef<HTMLButtonElement, IconButtonProps>(function IconButton(
  { label, variant = 'ghost', size = 'md', className, children, ...rest },
  ref,
) {
  return (
    <button
      ref={ref}
      type={rest.type ?? 'button'}
      aria-label={label}
      title={label}
      className={cn(
        'inline-flex items-center justify-center transition-colors disabled:opacity-50',
        VARIANTS[variant],
        size === 'sm' ? 'size-8 rounded-lg' : 'size-9 rounded-lg',
        className,
      )}
      {...rest}
    >
      {children}
    </button>
  )
})
