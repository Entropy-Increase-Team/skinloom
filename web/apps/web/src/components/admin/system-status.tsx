'use client'

import { getSystemStatus, type StatusCheck, type SystemStatus } from '@skinloom/api-client'
import { Button, PageHeader, Skeleton, type Status, StatusDot } from '@skinloom/ui'
import { RefreshCw } from 'lucide-react'
import { useLocale, useTranslations } from 'next-intl'
import { type ReactNode, useCallback, useEffect, useState } from 'react'

import { FormError } from '@/components/auth/field'

/** Bytes as KB, MB, GB… with one decimal. */
export function formatBytes(n: number, locale: string): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: i === 0 ? 0 : 1 }).format(v)} ${units[i]}`
}

/** System status of the node answering (02 F40): findings first, then the figures. */
export function SystemStatusView() {
  const t = useTranslations('adminStatus')
  const locale = useLocale()
  const [status, setStatus] = useState<SystemStatus | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      setStatus((await getSystemStatus()).data as SystemStatus)
    } catch {
      setError(t('errors.load'))
    } finally {
      setLoading(false)
    }
  }, [t])
  useEffect(() => {
    void load()
  }, [load])

  const ta = useTranslations('admin')
  const header = (
    <PageHeader
      title={ta('systemStatus')}
      description={t('intro')}
      actions={
        <Button variant="outline" loading={loading} onClick={() => void load()}>
          <RefreshCw />
          {t('refresh')}
        </Button>
      }
    />
  )
  if (!status)
    return (
      <div className="flex flex-col gap-6">
        {header}
        {error ? <FormError message={error} /> : <Skeleton className="h-96 rounded-xl" />}
      </div>
    )

  const date = (iso?: string) => (iso ? new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(iso)) : '—')
  const number = (n: number) => new Intl.NumberFormat(locale).format(n)
  const s = status
  return (
    <div className="flex flex-col gap-6">
      {header}
      <FormError message={error} />

      <section className="flex flex-col gap-3" aria-labelledby="checks-title">
        <h2 id="checks-title" className="text-lg font-medium">
          {t('checks.title')}
        </h2>
        <ul className="flex flex-col divide-y rounded-xl border bg-card">
          {s.checks.map((c) => (
            <Check key={c.id} check={c} />
          ))}
        </ul>
      </section>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
        <Facts
          title={t('server.title')}
          rows={[
            [t('server.version'), s.server.version + (s.server.commit ? ` (${s.server.commit.slice(0, 7)})` : '')],
            [t('server.buildDate'), s.server.buildDate ? date(s.server.buildDate) : '—'],
            [t('server.runtime'), `${s.server.go} · ${s.server.os}/${s.server.arch}`],
            [t('server.mode'), `${s.server.role} · ${t('server.web')} ${s.server.web}`],
            [t('server.started'), date(s.server.startedAt)],
          ]}
        />
        <Facts
          title={t('database.title')}
          rows={[
            [t('database.engine'), `${s.database.dialect} ${s.database.version}`],
            [t('database.migration'), String(s.database.migration)],
            [t('database.pending'), number(s.database.pending)],
            [t('database.ping'), `${s.database.pingMillis.toFixed(1)} ms`],
          ]}
        />
        <Facts
          title={t('storage.title')}
          rows={[
            [t('storage.driver'), (t.has(`storage.drivers.${s.storage.driver}`) ? t(`storage.drivers.${s.storage.driver}`) : s.storage.driver)],
            [t('storage.files'), number(s.storage.files)],
            [t('storage.size'), formatBytes(s.storage.bytes, locale)],
          ]}
        />
        <Facts
          title={t('jobs.title')}
          rows={[
            [t('jobs.pending'), number(s.jobs.pending)],
            [t('jobs.overdue'), number(s.jobs.overdue)],
            [t('jobs.oldest'), date(s.jobs.oldestOverdue)],
            [t('jobs.failed'), number(s.jobs.failed)],
          ]}
        />
        <Facts
          title={t('mail.title')}
          rows={[
            [t('mail.driver'), s.mail.driver],
            [t('mail.ready'), s.mail.ready ? t('yes') : t('no')],
          ]}
        />
        <Facts
          title={t('config.title')}
          rows={[
            [t('config.version'), String(s.config.version)],
            [t('config.theme'), s.config.theme],
            [t('config.changed'), number(s.config.changed)],
            [t('config.plugins'), t('config.pluginsLater')],
          ]}
        />
      </div>
    </div>
  )
}

function Check({ check }: { check: StatusCheck }) {
  const t = useTranslations('adminStatus.checks')
  const level: Status = check.level === 'ok' ? 'ok' : check.level === 'warn' ? 'warn' : 'error'
  const text = t.has(`${check.id}.ok`) ? t(`${check.id}.${check.level === 'ok' ? 'ok' : 'problem'}`) : check.id
  return (
    <li className="px-4 py-3">
      <StatusDot status={level} className={level === 'ok' ? 'text-sm' : 'text-sm text-foreground'}>
        {text}
      </StatusDot>
    </li>
  )
}

function Facts({ title, rows }: { title: string; rows: [string, ReactNode][] }) {
  return (
    <section className="flex flex-col gap-3 rounded-xl border bg-card p-5">
      <h2 className="text-sm font-medium">{title}</h2>
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1.5 text-sm">
        {rows.map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="text-muted-foreground">{k}</dt>
            <dd className="min-w-0 break-words tabular-nums">{v}</dd>
          </div>
        ))}
      </dl>
    </section>
  )
}
