import * as DialogPrimitive from '@radix-ui/react-dialog'
import { X } from 'lucide-react'
import type { ComponentPropsWithoutRef, HTMLAttributes } from 'react'
import { cn } from '../../lib/utils.ts'

export const Sheet = DialogPrimitive.Root
export const SheetTrigger = DialogPrimitive.Trigger
export const SheetClose = DialogPrimitive.Close

export function SheetContent({
  className,
  children,
  side = 'right',
  showOverlay = true,
  ...props
}: ComponentPropsWithoutRef<typeof DialogPrimitive.Content> & {
  side?: 'right' | 'left'
  showOverlay?: boolean
}) {
  return (
    <DialogPrimitive.Portal>
      {showOverlay && (
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-[var(--overlay-scrim)] backdrop-blur-[2px] transition-opacity" />
      )}
      <DialogPrimitive.Content
        className={cn(
          'fixed inset-y-0 z-50 flex flex-col border-border-default bg-bg-surface text-text-primary shadow-[var(--shadow-lg)] outline-none',
          side === 'right' ? 'right-0 border-l' : 'left-0 border-r',
          className,
        )}
        {...props}
      >
        {children}
        <DialogPrimitive.Close
          aria-label="关闭抽屉"
          className="absolute right-3.5 top-3.5 inline-flex h-7 w-7 cursor-pointer items-center justify-center rounded-[var(--radius-sm)] text-text-muted transition-colors hover:bg-bg-subtle hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40"
        >
          <X className="h-4 w-4" />
        </DialogPrimitive.Close>
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  )
}

export function SheetHeader({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn('flex flex-col gap-1 border-b border-border-default px-5 py-4 pr-12', className)}
      {...props}
    />
  )
}

export function SheetTitle({
  className,
  ...props
}: ComponentPropsWithoutRef<typeof DialogPrimitive.Title>) {
  return (
    <DialogPrimitive.Title
      className={cn('text-base font-semibold text-text-primary', className)}
      {...props}
    />
  )
}

export function SheetDescription({
  className,
  ...props
}: ComponentPropsWithoutRef<typeof DialogPrimitive.Description>) {
  return (
    <DialogPrimitive.Description
      className={cn('text-xs text-text-muted', className)}
      {...props}
    />
  )
}
