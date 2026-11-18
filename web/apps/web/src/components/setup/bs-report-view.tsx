'use client'

import type { BSReportEntryView, BSReportView as BSReportData } from '@skinloom/api-client'
import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
  ButtonLink,
  StatusDot,
  Tabs,
  TabsList,
  TabsTrigger,
} from '@skinloom/ui'
import { Download } from 'lucide-react'
import { useTranslations } from 'next-intl'
import { useMemo, useState } from 'react'

type FilterTab = 'all' | 'blocking' | 'left' | 'warning' | 'fixed' | 'dropped'

function badgeVariant(e: BSReportEntryView): 'destructive' | 'secondary' | 'outline' {
  if (e.severity === 'BLOCKING') return 'destructive'
  if (e.leftBehind || e.severity === 'WARNING') return 'secondary'
  return 'outline'
}

function dotStatus(e: BSReportEntryView): 'error' | 'warn' | 'ok' | 'off' {
  if (e.severity === 'BLOCKING') return 'error'
  if (e.leftBehind || e.severity === 'WARNING') return 'warn'
  if (e.severity === 'FIXED') return 'ok'
  return 'off'
}

export function BSReportView({
  report,
  jobId,
  hasReport,
}: {
  report: BSReportData
  jobId: string
  hasReport?: boolean
}) {
  const t = useTranslations('setup.bs.report')
  const [tab, setTab] = useState<FilterTab>('all')

  const counts = useMemo(() => {
    const c = { all: report.entries.length, blocking: 0, left: 0, warning: 0, fixed: 0, dropped: 0 }
    for (const e of report.entries) {
      if (e.severity === 'BLOCKING') c.blocking++
      else if (e.leftBehind) c.left++
      else if (e.severity === 'WARNING') c.warning++
      else if (e.severity === 'FIXED') c.fixed++
      else if (e.severity === 'DROPPED') c.dropped++
    }
    return c
  }, [report.entries])

  const filtered = useMemo(() => {
    return report.entries.filter((e) => {
      switch (tab) {
        case 'blocking':
          return e.severity === 'BLOCKING'
        case 'left':
          return Boolean(e.leftBehind)
        case 'warning':
          return !e.leftBehind && e.severity === 'WARNING'
        case 'fixed':
          return e.severity === 'FIXED'
        case 'dropped':
          return e.severity === 'DROPPED'
        default:
          return true
      }
    })
  }, [report.entries, tab])

  return (
    <div className="flex flex-col gap-4 rounded-xl border bg-card p-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-col gap-0.5">
          <h3 className="text-sm font-medium">{report.dryRun ? t('dryRunTitle') : t('finalTitle')}</h3>
          <p className="text-xs text-muted-foreground tabular-nums">
            {t('summary', {
              users: report.imported.users ?? 0,
              players: report.imported.players ?? 0,
              textures: report.imported.textures ?? 0,
              files: report.files,
              copied: report.filesCopied,
              superadmins: report.superadmins,
            })}
          </p>
        </div>
        {hasReport && (
          <ButtonLink
            variant="outline"
            size="sm"
            render={<a href={`/api/v1/setup/jobs/${encodeURIComponent(jobId)}/report.txt`} download />}
          >
            <Download className="size-3.5" aria-hidden />
            {t('downloadTxt')}
          </ButtonLink>
        )}
      </div>

      {report.blocked && (
        <Alert variant="destructive">
          <AlertTitle>{t('blockedTitle')}</AlertTitle>
          <AlertDescription>{t('blockedDesc')}</AlertDescription>
        </Alert>
      )}

      {report.failed && (
        <Alert variant="destructive">
          <AlertTitle>{t('failedTitle')}</AlertTitle>
          <AlertDescription>{report.failed}</AlertDescription>
        </Alert>
      )}

      <Tabs value={tab} onValueChange={(v) => setTab(v as FilterTab)}>
        <TabsList className="h-auto! w-full flex-wrap justify-start gap-1">
          <TabsTrigger value="all" className="py-1">{t('tabs.all', { count: counts.all })}</TabsTrigger>
          <TabsTrigger value="blocking" className="py-1">{t('tabs.blocking', { count: counts.blocking })}</TabsTrigger>
          <TabsTrigger value="left" className="py-1">{t('tabs.left', { count: counts.left })}</TabsTrigger>
          <TabsTrigger value="warning" className="py-1">{t('tabs.warning', { count: counts.warning })}</TabsTrigger>
          <TabsTrigger value="fixed" className="py-1">{t('tabs.fixed', { count: counts.fixed })}</TabsTrigger>
          <TabsTrigger value="dropped" className="py-1">{t('tabs.dropped', { count: counts.dropped })}</TabsTrigger>
        </TabsList>
      </Tabs>

      {filtered.length === 0 ? (
        <p className="py-6 text-center text-sm text-muted-foreground">{t('empty')}</p>
      ) : (
        <ul className="flex max-h-80 flex-col divide-y overflow-y-auto rounded-lg border">
          {filtered.map((e, idx) => (
            <li key={`${e.table}-${e.id ?? idx}-${idx}`} className="flex flex-col gap-1 px-3 py-2.5 text-sm">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant={badgeVariant(e)} className="font-mono text-xs">
                  <StatusDot status={dotStatus(e)} className="text-current">
                    {e.leftBehind ? t('badges.leftBehind') : e.severity}
                  </StatusDot>
                </Badge>
                <span className="font-mono text-xs text-muted-foreground">
                  {e.table}
                  {e.id ? `#${e.id}` : ''}
                </span>
              </div>
              <p className="text-xs leading-relaxed text-foreground">{e.message}</p>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
