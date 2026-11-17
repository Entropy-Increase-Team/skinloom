'use client'

import { listNotifications, type NotificationPage, type NotificationView, readAllNotifications, readNotification } from '@skinloom/api-client'
import { Badge, Button, cn, Empty, EmptyHeader, EmptyMedia, EmptyTitle, Markdown, PageHeader, RelativeTime, Skeleton } from '@skinloom/ui'
import { BellOff, CheckCheck, ChevronDown } from 'lucide-react'
import { useTranslations } from 'next-intl'
import { useCallback, useEffect, useState } from 'react'

import { NOTIFICATIONS_CHANGED } from '@/components/user/notification-bell'

/** The inbox: newest first; opening a notification marks it read. */
export function NotificationsList() {
  const t = useTranslations('notifications')
  const [items, setItems] = useState<NotificationView[] | null>(null)
  const [next, setNext] = useState(0)
  const [unread, setUnread] = useState(0)
  const [open, setOpen] = useState<number | null>(null)
  const [loading, setLoading] = useState(false)
  const [failed, setFailed] = useState(false)

  const load = useCallback(async (before?: number) => {
    setLoading(true)
    setFailed(false)
    try {
      const page = (await listNotifications({ limit: 20, ...(before ? { before } : {}) })).data as NotificationPage
      setItems((prev) => (before && prev ? [...prev, ...page.notifications] : page.notifications))
      setNext(page.next)
      setUnread(page.unread)
    } catch {
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [])
  useEffect(() => {
    void load()
  }, [load])

  async function toggle(n: NotificationView) {
    setOpen(open === n.id ? null : n.id)
    if (n.read) return
    setItems((list) => list && list.map((x) => (x.id === n.id ? { ...x, read: true } : x)))
    setUnread((u) => Math.max(0, u - 1))
    await readNotification(n.id).catch(() => undefined)
    window.dispatchEvent(new Event(NOTIFICATIONS_CHANGED))
  }

  async function readAll() {
    await readAllNotifications().catch(() => undefined)
    setItems((list) => list && list.map((x) => ({ ...x, read: true })))
    setUnread(0)
    window.dispatchEvent(new Event(NOTIFICATIONS_CHANGED))
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={t('title')}
        description={items ? t('unread', { count: unread }) : undefined}
        actions={
          <Button variant="outline" disabled={unread === 0} onClick={() => void readAll()}>
            <CheckCheck />
            {t('readAll')}
          </Button>
        }
      />
      {failed && <p className="text-center text-sm text-destructive">{t('loadFailed')}</p>}
      {items === null && !failed && <Skeleton className="h-48 rounded-xl" />}
      {items?.length === 0 && (
        <Empty className="border border-dashed py-16">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <BellOff />
            </EmptyMedia>
            <EmptyTitle>{t('empty')}</EmptyTitle>
          </EmptyHeader>
        </Empty>
      )}
      {items && items.length > 0 && (
        <ul className="flex flex-col divide-y overflow-hidden rounded-xl border bg-card">
          {items.map((n) => (
            <li key={n.id}>
              <button
                type="button"
                aria-expanded={open === n.id}
                onClick={() => void toggle(n)}
                className="flex w-full items-center gap-3 px-4 py-3.5 text-left transition-colors hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none"
              >
                <span className={cn('size-1.5 shrink-0 rounded-full', n.read ? 'bg-transparent' : 'bg-primary')} aria-hidden />
                <span className={cn('min-w-0 flex-1 truncate text-sm', n.read ? 'text-muted-foreground' : 'font-medium')}>{n.title}</span>
                {n.announcement && (
                  <Badge variant="secondary" className="max-sm:hidden">
                    {t('announcement')}
                  </Badge>
                )}
                <RelativeTime date={new Date(n.createdAt)} className="shrink-0 text-caption text-faint-foreground" />
                <ChevronDown className={cn('size-4 shrink-0 text-faint-foreground transition-transform duration-150', open === n.id && 'rotate-180')} aria-hidden />
                <span className="sr-only">{n.read ? t('read') : t('new')}</span>
              </button>
              {open === n.id && (
                <div className="px-4 pb-4 pl-[1.875rem]">
                  {n.contentHtml.trim() ? <Markdown html={n.contentHtml} className="text-sm" /> : <p className="text-sm text-muted-foreground">{t('noContent')}</p>}
                </div>
              )}
            </li>
          ))}
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
