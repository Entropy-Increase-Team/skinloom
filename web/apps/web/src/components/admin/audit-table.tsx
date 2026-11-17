'use client'

import { type AuditPage, type AuditView, listAudit } from '@skinloom/api-client'
import {
  Button,
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  RelativeTime,
  Skeleton,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@skinloom/ui'
import { Search } from 'lucide-react'
import { useTranslations } from 'next-intl'
import { type FormEvent, useCallback, useEffect, useState } from 'react'

import { FormError } from '@/components/auth/field'

/** Who did what, newest first; filtered by an action prefix such as auth.login or admin. */
export function AuditTable() {
  const t = useTranslations('admin')
  const [action, setAction] = useState('')
  const [records, setRecords] = useState<AuditView[] | null>(null)
  const [next, setNext] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(
    async (before?: number) => {
      setLoading(true)
      setError(null)
      try {
        const page = (await listAudit({ limit: 50, ...(action ? { action } : {}), ...(before ? { before } : {}) })).data as AuditPage
        setRecords((prev) => (before && prev ? [...prev, ...page.records] : page.records))
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

  function onFilter(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    setAction(String(new FormData(e.currentTarget).get('action') ?? '').trim())
  }

  return (
    <div className="flex flex-col gap-4">
      <form onSubmit={onFilter} role="search" className="w-full sm:w-72">
        <InputGroup>
          <InputGroupAddon>
            <Search aria-hidden />
          </InputGroupAddon>
          <InputGroupInput type="search" name="action" placeholder={t('actionHint')} aria-label={t('filter')} className="font-mono" />
        </InputGroup>
      </form>
      <FormError message={error} />
      {records === null && !error && <Skeleton className="h-64 rounded-xl" />}
      {records && (
        // A log is read as a table even on phones: the table scrolls sideways on its own.
        <div className="overflow-x-auto rounded-xl border bg-card">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">{t('time')}</TableHead>
                <TableHead>{t('action')}</TableHead>
                <TableHead>{t('actor')}</TableHead>
                <TableHead>{t('target')}</TableHead>
                <TableHead>IP</TableHead>
                <TableHead>{t('details')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {records.map((r) => (
                <TableRow key={r.id}>
                  <TableCell className="pl-4 text-caption whitespace-nowrap text-muted-foreground">
                    <RelativeTime date={r.createdAt} />
                  </TableCell>
                  <TableCell className="font-mono text-caption">{r.action}</TableCell>
                  <TableCell className="text-caption">{r.actorId ? `#${r.actorId}` : <span className="text-muted-foreground">{t('system')}</span>}</TableCell>
                  <TableCell className="text-caption">
                    {r.targetType} <span className="font-mono">{r.targetId}</span>
                  </TableCell>
                  <TableCell className="font-mono text-caption text-muted-foreground">{r.ip}</TableCell>
                  <TableCell className="max-w-md truncate font-mono text-caption text-muted-foreground" title={r.meta ? JSON.stringify(r.meta) : ''}>
                    {r.meta ? JSON.stringify(r.meta) : ''}
                  </TableCell>
                </TableRow>
              ))}
              {records.length === 0 && (
                <TableRow>
                  <TableCell colSpan={6} className="py-10 text-center text-muted-foreground">
                    {t('emptyAudit')}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>
      )}
      {next > 0 && (
        <Button variant="outline" className="self-center" loading={loading} onClick={() => void load(next)}>
          {t('more')}
        </Button>
      )}
    </div>
  )
}
