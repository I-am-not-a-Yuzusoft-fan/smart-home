import { forwardRef, type InputHTMLAttributes } from 'react'
import { cn } from '../../lib/utils.ts'

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(
  ({ className, ...props }, ref) => (
    <input
      ref={ref}
      className={cn(
        'flex h-9 w-full rounded-[var(--radius-sm)] border border-border-default bg-bg-surface px-3 py-1.5 text-sm text-text-primary',
        'placeholder:text-text-muted/70 transition-colors',
        'focus-visible:outline-none focus-visible:border-accent focus-visible:ring-2 focus-visible:ring-accent/20',
        'disabled:cursor-not-allowed disabled:opacity-50',
        className,
      )}
      {...props}
    />
  ),
)
Input.displayName = 'Input'
