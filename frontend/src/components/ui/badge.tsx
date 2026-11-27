import type { HTMLAttributes } from 'react'
import { cn } from '../../lib/utils.ts'

export type BadgeVariant = 'default' | 'secondary' | 'accent' | 'success' | 'warning' | 'danger' | 'outline'

export interface BadgeProps extends HTMLAttributes<HTMLSpanElement> {
  variant?: BadgeVariant
}

const variantClasses: Record<BadgeVariant, string> = {
  default: 'bg-bg-subtle text-text-secondary border-border-default',
  secondary: 'bg-bg-subtle text-text-muted border-transparent',
  accent: 'bg-accent-soft text-accent border-accent-border',
  success: 'bg-status-success-soft text-status-success border-status-success/20',
  warning: 'bg-status-warning-soft text-status-warning border-status-warning/25',
  danger: 'bg-status-danger-soft text-status-danger border-status-danger/25',
  outline: 'bg-transparent text-text-secondary border-border-strong',
}

export function Badge({ className, variant = 'default', ...props }: BadgeProps) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[11px] font-medium leading-tight whitespace-nowrap',
        variantClasses[variant],
        className,
      )}
      {...props}
    />
  )
}
