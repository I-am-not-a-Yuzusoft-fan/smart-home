import type { LabelHTMLAttributes } from 'react'
import { cn } from '../../lib/utils.ts'

export function Label({ className, ...props }: LabelHTMLAttributes<HTMLLabelElement>) {
  return (
    <label
      className={cn('text-xs font-medium text-text-secondary leading-none select-none', className)}
      {...props}
    />
  )
}
