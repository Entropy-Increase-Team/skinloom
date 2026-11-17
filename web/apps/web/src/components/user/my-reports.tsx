'use client'

import { listMyReports, type ReportPage, type ReportView } from '@skinloom/api-client'
import { Button, Empty, EmptyHeader, EmptyMedia, EmptyTitle, PixelBackdrop, RelativeTime, Skeleton, type Status, StatusDot, TexturePreview } from '@skinloom/ui'
import { Flag, ImageOff } from 'lucide-react'
import Link from 'next/link'
import { useTranslations } from 'next-intl'
import { useCallback, useEffect, useState } from 'react'

const STATUS: Record<ReportView['status'], Status> = { pending: 'warn', resolved: 'ok', rejected: 'off' }

/** The signed-in user's reports and what became of them. */
export function MyReports() {
  const t = useTranslations('myReports')
  const [reports, setReports] = useState<ReportView[] | null>(null)
  const [next, setNext] = useState(0)
  const [loading, setLoading] = useState(false)
  const [failed, setFailed] = useState(false)

  const load = useCallback(async (before?: number) => {
    setLoading(true)
    setFailed(false)
    try {
      const page = (await listMyReports({ limit: 20, ...(before ? { before } : {}) })).data as ReportPage
      setReports((prev) => (before && prev ? [...prev, ...page.reports] : page.reports))
      setNext(page.next)
    } catch {
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [])
  useEffect(() => {
    void load()
  }, [load])

  if (failed) return <p className="text-center text-sm text-destructive">{t('loadFailed')}</p>
  if (reports === null) return <Skeleton className="h-40 rounded-xl" />
  if (reports.length === 0) {
    return (
      <Empty className="border border-dashed py-16">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <Flag />
          </EmptyMedia>
          <EmptyTitle>{t('empty')}</EmptyTitle>
        </EmptyHeader>
      </Empty>
    )
  }
  return (
    <div className="flex flex-col gap-4">
      <ul className="flex flex-col divide-y overflow-hidden rounded-xl border bg-card">
        {reports.map((r) => (
          <li key={r.id} className="flex gap-4 p-4">
            <PixelBackdrop className="h-20 w-20 shrink-0 rounded-md">
              {r.texture ? (
                <TexturePreview texture={{ hash: r.texture.hash }} type={r.texture.type} height={64} />
              ) : (
                <ImageOff className="size-5 text-faint-foreground" aria-hidden />
              )}
            </PixelBackdrop>
            <div className="flex min-w-0 flex-1 flex-col gap-1.5">
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                {r.texture ? (
                  <Link href={`/skinlib/show/${r.texture.id}`} className="truncate text-sm font-medium hover:text-primary">
                    {r.texture.name}
                  </Link>
                ) : (
                  <span className="text-sm text-muted-foreground">{t('textureGone')}</span>
                )}
                <StatusDot status={STATUS[r.status]}>{t(`status.${r.status}`)}</StatusDot>
              </div>
              <p className="text-sm break-words whitespace-pre-line">{r.reason}</p>
              <p className="flex flex-wrap gap-x-1.5 text-caption text-faint-foreground">
                <span>{t.rich('submitted', { t: () => <RelativeTime date={r.createdAt} /> })}</span>
                {r.handledAt && (
                  <>
                    <span aria-hidden>·</span>
                    <span>{t.rich(`outcome.${r.action ?? 'reject'}`, { t: () => <RelativeTime date={r.handledAt!} /> })}</span>
                  </>
                )}
              </p>
            </div>
          </li>
        ))}
      </ul>
      {next > 0 && (
        <Button variant="outline" className="self-center" loading={loading} onClick={() => void load(next)}>
          {t('more')}
        </Button>
      )}
    </div>
  )
}
