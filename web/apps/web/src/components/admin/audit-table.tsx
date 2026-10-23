'use client'

import { type AuditPage, type AuditView, listAudit } from '@skinloom/api-client'
import { Button, Input } from '@skinloom/ui'
import { useLocale, useTranslations } from 'next-intl'
import { type FormEvent, useCallback, useEffect, useState } from 'react'

import { FormError } from '@/components/auth/field'

export function AuditTable() {
  const t = useTranslations('admin')
  const locale = useLocale()
  const [action, setAction] = useState('')
  const [records, setRecords] = useState<AuditView[]>([])
  const [next, setNext] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(
    async (before?: number) => {
      setLoading(true)
      setError(null)
      try {
        const page = (await listAudit({ limit: 50, ...(action ? { action } : {}), ...(before ? { before } : {}) })).data as AuditPage
        setRecords((prev) => (before ? [...prev, ...page.records] : page.records))
        setNext(page.next)
      } catch {
        setError(t('errors.load'))
      } finally {
        setLoading(false)
      }
    },
    [action, t],
  )
  useEffect(() => {
    void load()
  }, [load])

  const when = (iso: string) => new Intl.DateTimeFormat(locale, { dateStyle: 'short', timeStyle: 'medium' }).format(new Date(iso))
  function onFilter(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    setAction(String(new FormData(e.currentTarget).get('action') ?? '').trim())
  }

  return (
    <div className="flex flex-col gap-4">
      <form onSubmit={onFilter} className="flex items-end gap-2">
        <Input name="action" placeholder={t('actionHint')} className="w-64" />
        <Button type="submit" variant="outline">
          {t('filter')}
        </Button>
      </form>
      <FormError message={error} />
      <div className="overflow-x-auto rounded-md border">
        <table className="w-full text-sm">
          <thead className="bg-muted/50 text-left text-xs text-muted-foreground">
            <tr>
              <th className="px-3 py-2">{t('time')}</th>
              <th className="px-3 py-2">{t('action')}</th>
              <th className="px-3 py-2">{t('actor')}</th>
              <th className="px-3 py-2">{t('target')}</th>
              <th className="px-3 py-2">IP</th>
              <th className="px-3 py-2">{t('details')}</th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {records.map((r) => (
              <tr key={r.id}>
                <td className="whitespace-nowrap px-3 py-2 text-xs">{when(r.createdAt)}</td>
                <td className="px-3 py-2 font-mono text-xs">{r.action}</td>
                <td className="px-3 py-2 text-xs">{r.actorId ?? t('system')}</td>
                <td className="px-3 py-2 text-xs">
                  {r.targetType} {r.targetId}
                </td>
                <td className="px-3 py-2 text-xs">{r.ip}</td>
                <td className="max-w-md truncate px-3 py-2 font-mono text-xs text-muted-foreground" title={r.meta ? JSON.stringify(r.meta) : ''}>
                  {r.meta ? JSON.stringify(r.meta) : ''}
                </td>
              </tr>
            ))}
            {records.length === 0 && !loading && (
              <tr>
                <td colSpan={6} className="px-3 py-6 text-center text-muted-foreground">
                  {t('empty')}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {next > 0 && (
        <Button variant="outline" className="self-center" disabled={loading} onClick={() => void load(next)}>
          {t('more')}
        </Button>
      )}
    </div>
  )
}
