'use client'

import { XIcon } from 'lucide-react'
import * as React from 'react'

import { useIsMobile } from '../../hooks/use-mobile'
import { cn } from '../../lib/utils'
import { Button } from '../ui/button'
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from '../ui/dialog'
import { Drawer, DrawerClose, DrawerContent, DrawerDescription, DrawerFooter, DrawerHeader, DrawerTitle, DrawerTrigger } from '../ui/drawer'
import { SrLabel } from './labels'

const ResponsiveDialogContext = React.createContext(false)

/**
 * Dialog on desktop (md and up), bottom Drawer on phones (< 768px) (web/design.md §9.4).
 */
export function ResponsiveDialog({
  open,
  onOpenChange,
  children,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  children: React.ReactNode
}) {
  const isMobile = useIsMobile()
  return (
    <ResponsiveDialogContext.Provider value={isMobile}>
      {isMobile ? (
        <Drawer open={open} onOpenChange={onOpenChange} swipeDirection="down" showSwipeHandle>
          {children}
        </Drawer>
      ) : (
        <Dialog open={open} onOpenChange={onOpenChange}>
          {children}
        </Dialog>
      )}
    </ResponsiveDialogContext.Provider>
  )
}

export function ResponsiveDialogTrigger({
  ...props
}: Omit<React.ComponentProps<typeof DialogTrigger>, 'handle' | 'payload'>) {
  const isMobile = React.useContext(ResponsiveDialogContext)
  if (isMobile) {
    return <DrawerTrigger {...props} />
  }
  return <DialogTrigger {...props} />
}

export function ResponsiveDialogClose({
  ...props
}: Omit<React.ComponentProps<typeof DialogClose>, 'handle'>) {
  const isMobile = React.useContext(ResponsiveDialogContext)
  if (isMobile) {
    return <DrawerClose {...props} />
  }
  return <DialogClose {...props} />
}

export function ResponsiveDialogContent({
  className,
  showCloseButton = true,
  children,
}: {
  className?: string
  showCloseButton?: boolean
  children: React.ReactNode
}) {
  const isMobile = React.useContext(ResponsiveDialogContext)
  if (isMobile) {
    return (
      <DrawerContent
        data-slot="responsive-dialog-content"
        className={cn('max-h-[90dvh]', className, 'overflow-visible sm:max-w-none')}
      >
        {showCloseButton && (
          <DrawerClose
            data-slot="responsive-dialog-close"
            render={<Button variant="ghost" className="absolute top-4 right-3 z-10 pointer-coarse:size-10" size="icon-sm" />}
          >
            <XIcon />
            <SrLabel k="close" />
          </DrawerClose>
        )}
        <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-4">{children}</div>
      </DrawerContent>
    )
  }
  return (
    <DialogContent data-slot="responsive-dialog-content" showCloseButton={showCloseButton} className={className}>
      {children}
    </DialogContent>
  )
}

export function ResponsiveDialogHeader({ className, ...props }: React.ComponentProps<'div'>) {
  const isMobile = React.useContext(ResponsiveDialogContext)
  if (isMobile) {
    return <DrawerHeader className={cn('p-0 pr-10 text-left pointer-coarse:pr-12', className)} {...props} />
  }
  return <DialogHeader className={className} {...props} />
}

export function ResponsiveDialogTitle({ className, children }: { className?: string; children: React.ReactNode }) {
  const isMobile = React.useContext(ResponsiveDialogContext)
  if (isMobile) {
    return <DrawerTitle className={className}>{children}</DrawerTitle>
  }
  return <DialogTitle className={className}>{children}</DialogTitle>
}

export function ResponsiveDialogDescription({ className, children }: { className?: string; children: React.ReactNode }) {
  const isMobile = React.useContext(ResponsiveDialogContext)
  if (isMobile) {
    return <DrawerDescription className={className}>{children}</DrawerDescription>
  }
  return <DialogDescription className={className}>{children}</DialogDescription>
}

export function ResponsiveDialogFooter({ className, ...props }: React.ComponentProps<'div'>) {
  const isMobile = React.useContext(ResponsiveDialogContext)
  if (isMobile) {
    return <DrawerFooter className={cn('px-0 pb-[max(0.25rem,env(safe-area-inset-bottom))] pt-2', className)} {...props} />
  }
  return <DialogFooter className={className} {...props} />
}
