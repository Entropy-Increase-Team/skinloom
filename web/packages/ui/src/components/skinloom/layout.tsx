import type * as React from 'react'

import { cn } from '../../lib/utils'

/**
 * The top of every app page (web/design.md §9.3): the page's name, one line saying what
 * it is for, and at most a primary and a secondary action. No greetings.
 */
export function PageHeader({
  title,
  description,
  actions,
  className,
}: {
  title: React.ReactNode
  description?: React.ReactNode
  actions?: React.ReactNode
  className?: string
}) {
  return (
    <div data-slot="page-header" className={cn('sl-page-header flex flex-wrap items-end gap-x-4 gap-y-3', className)}>
      <div className="flex min-w-0 flex-1 flex-col gap-1.5">
        <h1 className="text-2xl font-medium">{title}</h1>
        {description && <p className="text-sm text-muted-foreground">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
    </div>
  )
}

/**
 * A figure (web/design.md §10.3): a label, the number and, if given, what changed and
 * against what ("比上周多 48"). No sparklines or decoration; an icon only when it means
 * something, like the gold coins of the score.
 */
export function StatCard({
  label,
  value,
  note,
  icon,
  className,
}: {
  label: React.ReactNode
  value: React.ReactNode
  note?: React.ReactNode
  icon?: React.ReactNode
  className?: string
}) {
  return (
    <div data-slot="stat-card" className={cn('flex flex-col gap-2 rounded-xl border bg-card p-5 text-card-foreground', className)}>
      <p className="flex items-center gap-1.5 text-caption text-muted-foreground">
        {icon}
        {label}
      </p>
      <p className="text-3xl leading-none font-medium tabular-nums">{value}</p>
      {note && <p className="text-xs text-faint-foreground">{note}</p>}
    </div>
  )
}

/**
 * How much of a limit is used (storage, score): a bar and the numbers. Turns gold from
 * 80% and red when full (web/design.md §10.3).
 */
export function UsageMeter({
  label,
  value,
  max,
  format = (n) => String(n),
  className,
}: {
  label: React.ReactNode
  value: number
  max: number
  format?: (n: number) => string
  className?: string
}) {
  const ratio = max > 0 ? Math.min(1, value / max) : 0
  const tone = ratio >= 1 ? 'bg-destructive' : ratio >= 0.8 ? 'bg-warning' : 'bg-primary'
  return (
    <div data-slot="usage-meter" className={cn('flex flex-col gap-2', className)}>
      <div className="flex items-baseline justify-between gap-3 text-sm">
        <span className="text-muted-foreground">{label}</span>
        <span className="font-mono text-xs text-muted-foreground tabular-nums">
          {format(value)} / {format(max)}
        </span>
      </div>
      <div
        role="meter"
        aria-valuemin={0}
        aria-valuemax={max}
        aria-valuenow={value}
        className="h-1.5 overflow-hidden rounded-full bg-muted"
      >
        <div className={cn('h-full rounded-full transition-[width] duration-300 ease-out', tone)} style={{ width: `${ratio * 100}%` }} />
      </div>
    </div>
  )
}

export type Status = 'ok' | 'warn' | 'error' | 'off'

/** A status as a small dot and its words (system status, report states). */
export function StatusDot({ status, children, className }: { status: Status; children: React.ReactNode; className?: string }) {
  const dot = { ok: 'bg-success', warn: 'bg-warning', error: 'bg-destructive', off: 'bg-faint-foreground' }[status]
  return (
    <span data-slot="status-dot" data-status={status} className={cn('inline-flex items-center gap-1.5 text-caption text-muted-foreground', className)}>
      <span aria-hidden className={cn('size-1.5 shrink-0 rounded-full', dot)} />
      {children}
    </span>
  )
}

/**
 * Markdown the server already rendered to HTML (raw HTML and script links are removed
 * there, see internal/modules/notify), styled by .sl-markdown.
 */
export function Markdown({ html, className }: { html: string; className?: string }) {
  return <div data-slot="markdown" className={cn('sl-markdown', className)} dangerouslySetInnerHTML={{ __html: html }} />
}
