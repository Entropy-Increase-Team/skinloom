"use client"

import { cn } from "#lib/utils"
import { useUiLabels } from "#components/skinloom/labels"
import { Loader2Icon } from "lucide-react"

function Spinner({ className, ...props }: React.ComponentProps<"svg">) {
  const t = useUiLabels()
  return (
    <Loader2Icon data-slot="spinner" role="status" aria-label={t.loading} className={cn("size-4 animate-spin", className)} {...props} />
  )
}

export { Spinner }
