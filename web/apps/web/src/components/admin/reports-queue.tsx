'use client'

import { type AdminReportPage, type AdminReportView, type ListReportsParams, listReports, reviewReport } from '@skinloom/api-client'
import { Badge, Button } from '@skinloom/ui'
import Link from 'next/link'
import { useLocale, useTranslations } from 'next-intl'
import { useCallback, useEffect, useRef, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { TexturePreview } from '@/components/texture/texture-preview'
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

/** The review queue: pending reports first, with the three actions. */
export function ReportsQueue() {
  const t = useTranslations('adminReports')
  const locale = useLocale()
  const [status, setStatus] = useState<Status>('pending')
  const [reports, setReports] = useState<AdminReportView[]>([])
  const [next, setNext] = useState(0)
  const [loading, setLoading] = useState(false)
  const [busy, setBusy] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const latest = useRef(0)

  const load = useCallback(
    async (before?: number) => {
      const seq = ++latest.current
      setLoading(true)
      setError(null)
      try {
        const page = (await listReports({ limit: 20, ...(status ? { status } : {}), ...(before ? { before } : {}) })).data as AdminReportPage
        if (seq !== latest.current) return
        setReports((prev) => (before ? [...prev, ...page.reports] : page.reports))
        setNext(page.next)
      } catch {
        if (seq === latest.current) setError(t('errors.load'))
      } finally {
        if (seq === latest.current) setLoading(false)
      }
    },
    [status, t],
  )
  useEffect(() => {
    void load()
  }, [load])

  async function review(r: AdminReportView, action: Action) {
    const asks = action === 'ban' || (action === 'delete' && r.texture)
    if (asks && !window.confirm(t(`confirm.${action}`, { name: r.texture?.name ?? `#${r.id}`, uploader: r.uploader?.nickname ?? '—' }))) return
    setBusy(r.id)
    setError(null)
    try {
      const updated = (await reviewReport(r.id, { action })).data as AdminReportView
      setReports((list) => afterReview(list, updated, action, status, r.texture?.id))
    } catch (err) {
      const p = problemOf(err)
      setError(p.status === 403 ? t('errors.forbidden') : p.status === 409 ? t('errors.reviewed') : t('errors.failed'))
      if (p.status === 409) void load()
    } finally {
      setBusy(0)
    }
  }

  const date = (iso?: string) => (iso ? new Intl.DateTimeFormat(locale, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(iso)) : '—')
  const tab = (active: boolean) =>
    `rounded-full px-3 py-1 text-sm transition-colors ${active ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:bg-muted hover:text-foreground'}`

  return (
    <div className="flex flex-col gap-4">
      <div className="flex gap-1" role="tablist">
        {(['pending', 'resolved', 'rejected', ''] as const).map((s) => (
          <button key={s || 'all'} type="button" role="tab" aria-selected={status === s} className={tab(status === s)} onClick={() => setStatus(s)}>
            {t(`status.${s || 'all'}`)}
          </button>
        ))}
      </div>
      <FormError message={error} />
      {reports.length === 0 && !loading && <p className="py-12 text-center text-sm text-muted-foreground">{t('empty')}</p>}
      <ul className="flex flex-col gap-3">
        {reports.map((r) => (
          <li key={r.id} className="flex flex-col gap-3 rounded-lg border p-4 sm:flex-row">
            <div className="flex h-24 w-20 shrink-0 items-center justify-center rounded-md bg-muted/40">
              {r.texture ? (
                <TexturePreview src={`/textures/${r.texture.hash}`} type={r.texture.type} height={88} />
              ) : (
                <span className="px-1 text-center text-xs text-muted-foreground">{t('textureGone')}</span>
              )}
            </div>
            <div className="flex min-w-0 flex-1 flex-col gap-1 text-sm">
              <div className="flex flex-wrap items-center gap-2">
                {r.texture ? (
                  <Link href={`/skinlib/show/${r.texture.id}`} className="font-medium hover:underline">
                    {r.texture.name}
                  </Link>
                ) : (
                  <span className="font-medium text-muted-foreground">{t('textureGone')}</span>
                )}
                {r.texture && !r.texture.public && <Badge variant="outline">{t('private')}</Badge>}
                <Badge variant={r.status === 'pending' ? 'default' : 'secondary'}>{t(`status.${r.status}`)}</Badge>
                {r.action && <span className="text-xs text-muted-foreground">{t(`action.${r.action}`)}</span>}
              </div>
              <p className="whitespace-pre-line break-words">{r.reason}</p>
              <p className="text-xs text-muted-foreground">
                {t('meta', {
                  reporter: r.reporter ? `${r.reporter.nickname} <${r.reporter.email}>` : t('deletedUser'),
                  uploader: r.uploader ? `${r.uploader.nickname} <${r.uploader.email}>` : t('deletedUser'),
                  time: date(r.createdAt),
                })}
              </p>
              {r.handledAt && (
                <p className="text-xs text-muted-foreground">{t('handled', { handler: r.handler?.nickname ?? t('token'), time: date(r.handledAt) })}</p>
              )}
            </div>
            {r.status === 'pending' && (
              <div className="flex shrink-0 flex-row gap-2 sm:flex-col">
                <Button size="sm" variant={r.texture ? 'destructive' : 'outline'} disabled={busy === r.id} onClick={() => void review(r, 'delete')}>
                  {r.texture ? t('do.delete') : t('do.resolve')}
                </Button>
                <Button size="sm" variant="outline" disabled={busy === r.id || !r.uploader} onClick={() => void review(r, 'ban')}>
                  {t('do.ban')}
                </Button>
                <Button size="sm" variant="ghost" disabled={busy === r.id} onClick={() => void review(r, 'reject')}>
                  {t('do.reject')}
                </Button>
              </div>
            )}
          </li>
        ))}
      </ul>
      {next > 0 && (
        <Button variant="outline" className="self-center" disabled={loading} onClick={() => void load(next)}>
          {t('more')}
        </Button>
      )}
    </div>
  )
}
