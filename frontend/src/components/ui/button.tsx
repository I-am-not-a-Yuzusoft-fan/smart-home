import { forwardRef, type ButtonHTMLAttributes } from 'react'
import { cn } from '../../lib/utils.ts'

export type ButtonVariant = 'default' | 'secondary' | 'outline' | 'ghost' | 'destructive' | 'soft'
export type ButtonSize = 'xs' | 'sm' | 'md' | 'lg' | 'icon'

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant
  size?: ButtonSize
}

const variantClasses: Record<ButtonVariant, string> = {
  default:
    'bg-accent text-accent-foreground hover:bg-accent-hover shadow-xs border border-transparent',
  secondary:
    'bg-bg-subtle text-text-primary hover:bg-bg-hover border border-border-default',
  outline:
    'bg-bg-surface text-text-primary hover:bg-bg-subtle border border-border-default',
  ghost:
    'bg-transparent text-text-secondary hover:bg-bg-subtle hover:text-text-primary border border-transparent',
  destructive:
    'bg-status-danger text-white hover:opacity-90 border border-transparent',
  soft:
    'bg-accent-soft text-accent hover:bg-accent-border/40 border border-accent-border/60',
}

const sizeClasses: Record<ButtonSize, string> = {
  xs: 'h-7 px-2 text-xs gap-1 rounded-[var(--radius-sm)]',
  sm: 'h-8 px-3 text-xs gap-1.5 rounded-[var(--radius-sm)]',
  md: 'h-9 px-3.5 text-sm gap-2 rounded-[var(--radius-sm)]',
  lg: 'h-10 px-4 text-sm gap-2 rounded-[var(--radius-md)]',
  icon: 'h-8 w-8 p-0 rounded-[var(--radius-sm)]',
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant = 'outline', size = 'md', type = 'button', ...props }, ref) => {
    return (
      <button
        ref={ref}
        type={type}
        className={cn(
          'inline-flex shrink-0 cursor-pointer items-center justify-center font-medium transition-colors',
          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40',
          'disabled:pointer-events-none disabled:opacity-50',
          variantClasses[variant],
          sizeClasses[size],
          className,
        )}
        {...props}
      />
    )
  },
)
Button.displayName = 'Button'
