'use client'

import { listMyReports, type ReportPage, type ReportView } from '@skinloom/api-client'
import { Badge, Button } from '@skinloom/ui'
import Link from 'next/link'
import { useLocale, useTranslations } from 'next-intl'
import { useCallback, useEffect, useState } from 'react'

import { TexturePreview } from '@/components/texture/texture-preview'

/** The signed-in user's reports and what became of them. */
export function MyReports() {
  const t = useTranslations('myReports')
  const locale = useLocale()
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

  const date = (iso: string) => new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(iso))

  return (
    <div className="flex flex-col gap-4">
      {failed && <p className="text-center text-sm text-destructive">{t('loadFailed')}</p>}
      {reports?.length === 0 && <p className="py-12 text-center text-sm text-muted-foreground">{t('empty')}</p>}
      <ul className="flex flex-col gap-3">
        {reports?.map((r) => (
          <li key={r.id} className="flex gap-4 rounded-lg border p-4">
            <div className="flex h-20 w-16 shrink-0 items-center justify-center rounded-md bg-muted/40">
              {r.texture ? (
                <TexturePreview src={`/textures/${r.texture.hash}`} type={r.texture.type} height={72} />
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
                <Badge variant={r.status === 'pending' ? 'default' : r.status === 'resolved' ? 'secondary' : 'outline'}>{t(`status.${r.status}`)}</Badge>
              </div>
              <p className="whitespace-pre-line break-words">{r.reason}</p>
              <p className="text-xs text-muted-foreground">
                {t('submitted', { time: date(r.createdAt) })}
                {r.handledAt && ` · ${t(`outcome.${r.action ?? 'reject'}`)} · ${date(r.handledAt)}`}
              </p>
            </div>
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
