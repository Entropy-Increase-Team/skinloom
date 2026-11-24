'use client'

import { Check, Copy } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'

import { cn } from '../../lib/utils'
import { Button } from '../ui/button'
import { useUiLabels } from './labels'

async function writeClipboard(value: string): Promise<boolean> {
  if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(value)
      return true
    } catch {
      // Fall through to legacy textarea copy (e.g. non-HTTPS LAN self-hosting or denied permission).
    }
  }
  if (typeof document === 'undefined') return false
  const prev = document.activeElement instanceof HTMLElement ? document.activeElement : null
  const container = prev?.parentElement ?? document.body
  const area = document.createElement('textarea')
  area.value = value
  area.setAttribute('readonly', '')
  area.style.position = 'fixed'
  area.style.top = '0'
  area.style.left = '0'
  area.style.opacity = '0'
  area.style.pointerEvents = 'none'
  try {
    container.appendChild(area)
    area.focus({ preventScroll: true })
    area.select()
    return document.execCommand('copy')
  } catch {
    return false
  } finally {
    area.remove()
    prev?.focus({ preventScroll: true })
  }
}

function useCopy(value: string) {
  const [copied, setCopied] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined)
  useEffect(() => () => clearTimeout(timer.current), [])
  return {
    copied,
    copy: async () => {
      if (!(await writeClipboard(value))) return
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
  const copyLabel = copied ? t.copied : label ? `${t.copy}: ${label}` : t.copy
  return (
    <div data-slot="copy-field" className={cn('flex h-9 pointer-coarse:h-11 min-w-0 items-center gap-2 rounded-md border border-input bg-muted pr-1 pl-3', className)}>
      <code className="min-w-0 flex-1 truncate font-mono text-caption select-all" title={value}>
        {value}
      </code>
      <Button variant="ghost" size="icon-sm" onClick={() => void copy()} aria-label={copyLabel} title={copied ? t.copied : t.copy}>
        {copied ? <Check aria-hidden /> : <Copy aria-hidden />}
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
      <Button
        variant="ghost"
        size="icon-sm"
        className="absolute top-2 right-2"
        onClick={() => void copy()}
        aria-label={copied ? t.copied : t.copy}
        title={copied ? t.copied : t.copy}
      >
        {copied ? <Check aria-hidden /> : <Copy aria-hidden />}
      </Button>
      <span className="sr-only" aria-live="polite">
        {copied ? t.copied : ''}
      </span>
    </div>
  )
}
