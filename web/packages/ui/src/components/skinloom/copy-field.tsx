'use client'

import { Check, Copy } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'

import { cn } from '../../lib/utils'
import { Button } from '../ui/button'
import { useUiLabels } from './labels'

function useCopy(value: string) {
  const [copied, setCopied] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined)
  useEffect(() => () => clearTimeout(timer.current), [])
  return {
    copied,
    copy: async () => {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      clearTimeout(timer.current)
      timer.current = setTimeout(() => setCopied(false), 1500)
    },
  }
}

/**
 * A value to copy: a UUID, a token, the authentication server address. The value is in
 * the mono font and selectable; the button turns into a check for a moment.
 */
export function CopyField({ value, label, className }: { value: string; label?: string; className?: string }) {
  const t = useUiLabels()
  const { copied, copy } = useCopy(value)
  return (
    <div data-slot="copy-field" className={cn('flex h-10 min-w-0 items-center gap-2 rounded-md border border-input bg-muted pr-1 pl-3', className)}>
      <code className="min-w-0 flex-1 truncate font-mono text-caption select-all" title={value} aria-label={label}>
        {value}
      </code>
      <Button variant="ghost" size="icon-sm" onClick={() => void copy()} aria-label={copied ? t.copied : t.copy} title={copied ? t.copied : t.copy}>
        {copied ? <Check /> : <Copy />}
      </Button>
      <span className="sr-only" aria-live="polite">
        {copied ? t.copied : ''}
      </span>
    </div>
  )
}

/** Several lines to copy: a command or a piece of configuration. */
export function CodeBlock({ code, className }: { code: string; className?: string }) {
  const t = useUiLabels()
  const { copied, copy } = useCopy(code)
  return (
    <div data-slot="code-block" className={cn('relative rounded-lg border bg-muted', className)}>
      <pre className="overflow-x-auto p-4 pr-12 font-mono text-caption leading-relaxed">
        <code>{code}</code>
      </pre>
      <Button variant="ghost" size="icon-sm" className="absolute top-2 right-2" onClick={() => void copy()} aria-label={copied ? t.copied : t.copy}>
        {copied ? <Check /> : <Copy />}
      </Button>
    </div>
  )
}
