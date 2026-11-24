'use client'

import { type AdminReportPage, type AdminReportView, type ListReportsParams, listReports, reviewReport } from '@skinloom/api-client'
import {
  Alert,
  AlertAction,
  AlertDescription,
  Badge,
  Button,
  Empty,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
  PageHeader,
  PixelBackdrop,
  RelativeTime,
  Skeleton,
  type Status as Dot,
  StatusDot,
  TexturePreview,
  ToggleGroup,
  ToggleGroupItem,
  toast,
  useConfirm,
} from '@skinloom/ui'
import { Ban, Check, CircleAlert, FlagOff, ImageOff, Trash2, X } from 'lucide-react'
import Link from 'next/link'
import { useTranslations } from 'next-intl'
import { useCallback, useEffect, useRef, useState } from 'react'

import { problemOf } from '@/lib/problem'

type Status = NonNullable<ListReportsParams['status']> | ''
type Action = 'delete' | 'ban' | 'reject'

/**
 * The queue after a review came back as updated. Deleting a texture (textureId: what
 * the report was about) also settles the other pending reports about it, and none of
 * the reports about it has a texture any more: the list shows that without a reload.
 */
export function afterReview(list: AdminReportView[], updated: AdminReportView, action: Action, status: Status, textureId?: number): AdminReportView[] {
  const gone = action === 'delete' && textureId !== undefined
  const same = (x: AdminReportView) => gone && x.texture?.id === textureId
  if (status === 'pending') return list.filter((x) => x.id !== updated.id && !same(x))
  return list.map((x) => {
    if (x.id === updated.id) return updated
    if (!same(x)) return x
    if (x.status !== 'pending') return { ...x, texture: undefined }
    return { ...x, status: 'resolved', action: 'delete', texture: undefined, handledAt: updated.handledAt, handler: updated.handler }
  })
}

const DOT: Record<AdminReportView['status'], Dot> = { pending: 'warn', resolved: 'ok', rejected: 'off' }
const TABS = ['pending', 'resolved', 'rejected', ''] as const

/** The review queue: pending reports first, with the three actions. */
export function ReportsQueue() {
  const t = useTranslations('adminReports')
  const ta = useTranslations('admin')
  const tc = useTranslations('common')
  const confirm = useConfirm()
  const [status, setStatus] = useState<Status>('pending')
  const [reports, setReports] = useState<AdminReportView[] | null>(null)
  const [next, setNext] = useState(0)
  const [loading, setLoading] = useState(false)
  const [busy, setBusy] = useState<{ id: number; action: Action } | null>(null)
  const [failed, setFailed] = useState(false)
  const latest = useRef(0)

  const load = useCallback(
    async (before?: number) => {
      const seq = ++latest.current
      setLoading(true)
      setFailed(false)
      if (!before) setReports(null)
      try {
        const page = (await listReports({ limit: 20, ...(status ? { status } : {}), ...(before ? { before } : {}) })).data as AdminReportPage
        if (seq !== latest.current) return
        setReports((prev) => (before && prev ? [...prev, ...page.reports] : page.reports))
        setNext(page.next)
      } catch {
        if (seq === latest.current) setFailed(true)
      } finally {
        if (seq === latest.current) setLoading(false)
      }
    },
    [status],
  )
  useEffect(() => {
    void load()
  }, [load])

  async function review(r: AdminReportView, action: Action) {
    if (action === 'ban' || (action === 'delete' && r.texture)) {
      const name = r.texture?.name ?? `#${r.id}`
      const uploader = r.uploader?.nickname ?? t('deletedUser')
      const ok = await confirm({
        title: t(`confirmTitle.${action}`, { name, uploader }),
        description: t(`confirm.${action}`),
        confirmLabel: t(`do.${action}`),
      })
      if (!ok) return
    }
    setBusy({ id: r.id, action })
    try {
      const updated = (await reviewReport(r.id, { action })).data as AdminReportView
      setReports((list) => list && afterReview(list, updated, action, status, r.texture?.id))
      toast.success(t(`done.${action}`))
    } catch (err) {
      const p = problemOf(err)
      toast.error(p.status === 403 ? t('errors.forbidden') : p.status === 409 ? t('errors.reviewed') : t('errors.failed'))
      if (p.status === 409) void load()
    } finally {
      setBusy(null)
    }
  }

  const person = (u?: AdminReportView['reporter']) => (u ? `${u.nickname} · ${u.email}` : t('deletedUser'))

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={ta('reports')} description={t('intro')} />
      <div className="-mx-4 overflow-x-auto px-4 sm:mx-0 sm:px-0">
        <ToggleGroup
          value={[status || 'all']}
          onValueChange={(v) => {
            const picked = v[0]
            if (picked) setStatus(picked === 'all' ? '' : (picked as Status))
          }}
          spacing={1}
          aria-label={ta('status')}
          className="w-max rounded-lg bg-muted p-0.5"
        >
          {TABS.map((s) => (
            <ToggleGroupItem
              key={s || 'all'}
              value={s || 'all'}
              size="sm"
              className="rounded-md px-3 text-sm text-muted-foreground hover:bg-transparent hover:text-foreground aria-pressed:bg-card aria-pressed:font-medium aria-pressed:text-foreground aria-pressed:shadow-xs aria-pressed:ring-1 aria-pressed:ring-border"
            >
              {t(`status.${s || 'all'}`)}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>
      {failed && (
        <Alert variant="destructive" role="alert">
          <CircleAlert />
          <AlertDescription>{t('errors.load')}</AlertDescription>
          <AlertAction>
            <Button type="button" variant="outline" size="sm" onClick={() => void load(reports && reports.length > 0 ? next || undefined : undefined)}>
              {tc('retry')}
            </Button>
          </AlertAction>
        </Alert>
      )}
      {reports === null && !failed && <Skeleton className="h-48 rounded-xl" />}
      {reports?.length === 0 && (
        <Empty className="border border-dashed py-16">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <FlagOff />
            </EmptyMedia>
            <EmptyTitle>{status === 'pending' ? t('emptyPending') : t('empty')}</EmptyTitle>
          </EmptyHeader>
        </Empty>
      )}
      {reports && reports.length > 0 && (
        <ul className="flex flex-col divide-y rounded-xl border bg-card">
          {reports.map((r) => {
            const rowBusy = busy?.id === r.id
            return (
              <li key={r.id} className="flex flex-col gap-4 p-4 sm:flex-row sm:p-5">
                <PixelBackdrop className="h-28 w-full shrink-0 rounded-lg sm:w-36">
                  {r.texture ? (
                    <TexturePreview texture={{ id: r.texture.id }} type={r.texture.type} height={88} />
                  ) : (
                    <ImageOff className="size-5 text-faint-foreground" aria-hidden />
                  )}
                </PixelBackdrop>
                <div className="flex min-w-0 flex-1 flex-col gap-2">
                  <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                    {r.texture ? (
                      <Link href={`/skinlib/show/${r.texture.id}`} className="truncate font-medium hover:text-primary">
                        {r.texture.name}
                      </Link>
                    ) : (
                      <span className="text-muted-foreground">{t('textureGone')}</span>
                    )}
                    {r.texture && !r.texture.public && <Badge variant="outline">{t('private')}</Badge>}
                    <StatusDot status={DOT[r.status]}>{r.action ? t(`action.${r.action}`) : t(`status.${r.status}`)}</StatusDot>
                  </div>
                  <p className="text-sm break-words whitespace-pre-line">{r.reason}</p>
                  <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-caption">
                    <dt className="text-faint-foreground">{t('reporter')}</dt>
                    <dd className="truncate text-muted-foreground">
                      {person(r.reporter)} · <RelativeTime date={r.createdAt} />
                    </dd>
                    <dt className="text-faint-foreground">{t('uploader')}</dt>
                    <dd className="truncate text-muted-foreground">{person(r.uploader)}</dd>
                    {r.handledAt && (
                      <>
                        <dt className="text-faint-foreground">{t('handler')}</dt>
                        <dd className="truncate text-muted-foreground">
                          {r.handler?.nickname ?? t('token')} · <RelativeTime date={r.handledAt} />
                        </dd>
                      </>
                    )}
                  </dl>
                </div>
                {r.status === 'pending' && (
                  <div className="flex shrink-0 flex-wrap gap-2 sm:w-36 sm:flex-col sm:flex-nowrap">
                    <Button
                      size="sm"
                      variant={r.texture ? 'destructive-soft' : 'outline'}
                      loading={rowBusy && busy.action === 'delete'}
                      disabled={rowBusy}
                      onClick={() => void review(r, 'delete')}
                    >
                      {r.texture ? <Trash2 /> : <Check />}
                      {r.texture ? t('do.delete') : t('do.resolve')}
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      loading={rowBusy && busy.action === 'ban'}
                      disabled={rowBusy || !r.uploader}
                      onClick={() => void review(r, 'ban')}
                    >
                      <Ban />
                      {t('do.ban')}
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      loading={rowBusy && busy.action === 'reject'}
                      disabled={rowBusy}
                      onClick={() => void review(r, 'reject')}
                    >
                      <X />
                      {t('do.reject')}
                    </Button>
                  </div>
                )}
              </li>
            )
          })}
        </ul>
      )}
      {next > 0 && (
        <Button variant="outline" className="self-center" loading={loading} onClick={() => void load(next)}>
          {t('more')}
        </Button>
      )}
    </div>
  )
}
