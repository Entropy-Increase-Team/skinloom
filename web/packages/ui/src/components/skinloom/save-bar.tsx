'use client'

import { cn } from '../../lib/utils'
import { Button } from '../ui/button'
import { fill, useUiLabels } from './labels'

/**
 * Settings pages: when there are unsaved changes, a bar rises at the bottom with how
 * many, Discard and Save (web/design.md §10.3). It leaves room for the iPhone home
 * indicator.
 */
export function SaveBar({
  count,
  saving = false,
  onSave,
  onDiscard,
  className,
}: {
  /** Changed values; the bar hides at 0. */
  count: number
  saving?: boolean
  onSave: () => void
  onDiscard: () => void
  className?: string
}) {
  const t = useUiLabels()
  if (count <= 0) return null
  return (
    <div
      data-slot="save-bar"
      role="region"
      aria-live="polite"
      className={cn(
        'sticky bottom-0 z-30 -mx-4 mt-6 flex items-center gap-3 border-t bg-background/95 px-4 pt-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] backdrop-blur animate-in fade-in-0 slide-in-from-bottom-2 duration-200 sm:mx-0 sm:rounded-xl sm:border sm:shadow-(--shadow-popover)',
        className,
      )}
    >
      <span className="flex-1 text-sm text-muted-foreground">{fill(t.unsaved, { count })}</span>
      <Button variant="ghost" onClick={onDiscard} disabled={saving}>
        {t.discard}
      </Button>
      <Button onClick={onSave} loading={saving}>
        {t.save}
      </Button>
    </div>
  )
}
