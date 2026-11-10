'use client'

import { Upload } from 'lucide-react'
import { useId, useRef, useState } from 'react'
import type * as React from 'react'

import { cn } from '../../lib/utils'
import { useUiLabels } from './labels'

/**
 * Drop a file or click to choose one (web/design.md §10.3). Shows `children` (a preview
 * of the chosen file) when given, and the error under it.
 */
export function Dropzone({
  accept,
  onFile,
  disabled = false,
  hint,
  error,
  children,
  className,
}: {
  /** As on <input type="file">, e.g. "image/png". */
  accept?: string
  onFile: (file: File) => void
  disabled?: boolean
  /** A line under the prompt: "PNG，64×64 或 64×32，最大 1 MB". */
  hint?: React.ReactNode
  error?: React.ReactNode
  children?: React.ReactNode
  className?: string
}) {
  const t = useUiLabels()
  const id = useId()
  const input = useRef<HTMLInputElement>(null)
  const [over, setOver] = useState(false)
  const take = (files: FileList | null) => {
    const f = files?.[0]
    if (f && !disabled) onFile(f)
  }
  return (
    <div className={cn('flex flex-col gap-2', className)}>
      <label
        htmlFor={id}
        data-slot="dropzone"
        data-over={over || undefined}
        aria-disabled={disabled || undefined}
        onDragOver={(e) => {
          e.preventDefault()
          setOver(true)
        }}
        onDragLeave={() => setOver(false)}
        onDrop={(e) => {
          e.preventDefault()
          setOver(false)
          take(e.dataTransfer.files)
        }}
        className={cn(
          'flex min-h-40 cursor-pointer flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-input bg-card p-6 text-center transition-colors duration-150 hover:bg-muted/40 has-[:focus-visible]:ring-3 has-[:focus-visible]:ring-ring/50',
          over && 'border-primary bg-primary/5',
          error ? 'border-destructive' : undefined,
          disabled && 'pointer-events-none opacity-50',
        )}
      >
        {children ?? (
          <>
            <span className="flex size-10 items-center justify-center rounded-lg bg-muted">
              <Upload className="size-5 text-muted-foreground" aria-hidden />
            </span>
            <span className="text-sm">
              {t.dropHint} <span className="font-medium text-primary">{t.choose}</span>
            </span>
            {hint && <span className="text-xs text-faint-foreground">{hint}</span>}
          </>
        )}
        <input ref={input} id={id} type="file" accept={accept} disabled={disabled} className="sr-only" onChange={(e) => take(e.target.files)} />
      </label>
      {error && (
        <p role="alert" className="text-caption text-destructive">
          {error}
        </p>
      )}
    </div>
  )
}
