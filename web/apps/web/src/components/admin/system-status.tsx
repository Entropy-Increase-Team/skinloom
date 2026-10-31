'use client'

import { getSystemStatus, type StatusCheck, type SystemStatus } from '@skinloom/api-client'
import { Button, Card, CardContent, CardHeader, CardTitle } from '@skinloom/ui'
import { CircleAlert, CircleCheck, CircleX, RefreshCw } from 'lucide-react'
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

  if (!status) return error ? <FormError message={error} /> : <p className="text-sm text-muted-foreground">{t('loading')}</p>

  const date = (iso?: string) => (iso ? new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(iso)) : '—')
  const number = (n: number) => new Intl.NumberFormat(locale).format(n)
  const s = status
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">{t('intro')}</p>
        <Button variant="outline" size="sm" disabled={loading} onClick={() => void load()}>
          <RefreshCw />
          {t('refresh')}
        </Button>
      </div>
      <FormError message={error} />

      <Card>
        <CardHeader>
          <CardTitle>{t('checks.title')}</CardTitle>
        </CardHeader>
        <CardContent>
          <ul className="flex flex-col gap-2">
            {s.checks.map((c) => (
              <Check key={c.id} check={c} />
            ))}
          </ul>
        </CardContent>
      </Card>

      <div className="grid gap-4 md:grid-cols-2">
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
  const icon =
    check.level === 'ok' ? (
      <CircleCheck className="size-4 shrink-0 text-primary" aria-hidden />
    ) : check.level === 'warn' ? (
      <CircleAlert className="size-4 shrink-0 text-amber-500" aria-hidden />
    ) : (
      <CircleX className="size-4 shrink-0 text-destructive" aria-hidden />
    )
  return (
    <li className="flex items-start gap-2 text-sm">
      {icon}
      <span>{t.has(`${check.id}.ok`) ? t(`${check.id}.${check.level === 'ok' ? 'ok' : 'problem'}`) : check.id}</span>
    </li>
  )
}

function Facts({ title, rows }: { title: string; rows: [string, ReactNode][] }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{title}</CardTitle>
      </CardHeader>
      <CardContent>
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-sm">
          {rows.map(([k, v]) => (
            <div key={k} className="contents">
              <dt className="text-muted-foreground">{k}</dt>
              <dd className="min-w-0 break-words tabular-nums">{v}</dd>
            </div>
          ))}
        </dl>
      </CardContent>
    </Card>
  )
}
