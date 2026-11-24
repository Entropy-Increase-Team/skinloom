'use client'

import { listNotifications, type NotificationPage, type NotificationView, readAllNotifications, readNotification } from '@skinloom/api-client'
import { Alert, AlertAction, AlertDescription, Badge, Button, cn, Empty, EmptyHeader, EmptyMedia, EmptyTitle, Markdown, PageHeader, RelativeTime, Skeleton, toast } from '@skinloom/ui'
import { BellOff, CheckCheck, ChevronDown, CircleAlert } from 'lucide-react'
import { useTranslations } from 'next-intl'
import { useCallback, useEffect, useRef, useState } from 'react'

import { NOTIFICATIONS_CHANGED } from '@/components/user/notification-bell'

/** The inbox: newest first; opening a notification marks it read. */
export function NotificationsList() {
  const t = useTranslations('notifications')
  const tc = useTranslations('common')
  const [items, setItems] = useState<NotificationView[] | null>(null)
  const [next, setNext] = useState(0)
  const [unread, setUnread] = useState(0)
  const [open, setOpen] = useState<number | null>(null)
  const [loading, setLoading] = useState(false)
  const [markingAll, setMarkingAll] = useState(false)
  const [failed, setFailed] = useState(false)
  const syncVersion = useRef(0)

  const load = useCallback(async (before?: number) => {
    setLoading(true)
    setFailed(false)
    try {
      const page = (await listNotifications({ limit: 20, ...(before ? { before } : {}) })).data as NotificationPage
      syncVersion.current += 1
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
    setOpen((cur) => (cur === n.id ? null : n.id))
    if (n.read) return
    const version = syncVersion.current
    setItems((list) => list && list.map((x) => (x.id === n.id ? { ...x, read: true } : x)))
    setUnread((u) => Math.max(0, u - 1))
    try {
      await readNotification(n.id)
      window.dispatchEvent(new Event(NOTIFICATIONS_CHANGED))
    } catch {
      if (syncVersion.current === version) {
        setItems((list) => list && list.map((x) => (x.id === n.id ? { ...x, read: false } : x)))
        setUnread((u) => u + 1)
      }
      toast.error(t('actionFailed'))
    }
  }

  async function readAll() {
    setMarkingAll(true)
    try {
      await readAllNotifications()
      syncVersion.current += 1
      setItems((list) => list && list.map((x) => ({ ...x, read: true })))
      setUnread(0)
      window.dispatchEvent(new Event(NOTIFICATIONS_CHANGED))
    } catch {
      toast.error(t('actionFailed'))
    } finally {
      setMarkingAll(false)
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={t('title')}
        description={items ? t('unread', { count: unread }) : undefined}
        actions={
          <Button variant="outline" disabled={unread === 0} loading={markingAll} onClick={() => void readAll()}>
            <CheckCheck />
            {t('readAll')}
          </Button>
        }
      />
      {failed && (
        <Alert variant="destructive" role="alert">
          <CircleAlert />
          <AlertDescription>{t('loadFailed')}</AlertDescription>
          <AlertAction>
            <Button type="button" variant="outline" size="sm" onClick={() => void load(items && items.length > 0 ? next || undefined : undefined)}>
              {tc('retry')}
            </Button>
          </AlertAction>
        </Alert>
      )}
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
                className="flex w-full items-center gap-3 px-4 py-3.5 text-left transition-colors hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:ring-inset focus-visible:outline-none"
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
                <div className="px-4 pb-4 pl-8">
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
