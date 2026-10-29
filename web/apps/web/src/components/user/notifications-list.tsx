'use client'

import { listNotifications, type NotificationPage, type NotificationView, readAllNotifications, readNotification } from '@skinloom/api-client'
import { Badge, Button } from '@skinloom/ui'
import { useLocale, useTranslations } from 'next-intl'
import { useCallback, useEffect, useState } from 'react'

import { Markdown } from '@/components/markdown'
import { NOTIFICATIONS_CHANGED } from '@/components/user/notification-bell'

/** The inbox: newest first; opening a notification marks it read. */
export function NotificationsList() {
  const t = useTranslations('notifications')
  const locale = useLocale()
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

  const date = (iso: string) => new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(iso))

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">{t('unread', { count: unread })}</p>
        <Button variant="outline" size="sm" disabled={unread === 0} onClick={() => void readAll()}>
          {t('readAll')}
        </Button>
      </div>
      {failed && <p className="text-center text-sm text-destructive">{t('loadFailed')}</p>}
      {items?.length === 0 && <p className="py-12 text-center text-sm text-muted-foreground">{t('empty')}</p>}
      <ul className="flex flex-col divide-y rounded-lg border">
        {items?.map((n) => (
          <li key={n.id}>
            <button
              type="button"
              aria-expanded={open === n.id}
              onClick={() => void toggle(n)}
              className="flex w-full items-center gap-3 px-4 py-3 text-left hover:bg-muted/50"
            >
              <span className={`size-2 shrink-0 rounded-full ${n.read ? 'bg-transparent' : 'bg-primary'}`} aria-hidden />
              <span className={`min-w-0 flex-1 truncate ${n.read ? '' : 'font-semibold'}`}>{n.title}</span>
              {n.announcement && <Badge variant="secondary">{t('announcement')}</Badge>}
              <span className="shrink-0 text-xs text-muted-foreground">{date(n.createdAt)}</span>
              <span className="sr-only">{n.read ? t('read') : t('new')}</span>
            </button>
            {open === n.id && (
              <div className="px-4 pb-4 pl-9">
                {n.contentHtml.trim() ? <Markdown html={n.contentHtml} /> : <p className="text-sm text-muted-foreground">{t('noContent')}</p>}
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
