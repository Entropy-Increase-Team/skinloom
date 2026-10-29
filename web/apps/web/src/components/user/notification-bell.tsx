'use client'

import { countUnreadNotifications, type UnreadCount } from '@skinloom/api-client'
import { Bell } from 'lucide-react'
import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { useEffect, useState } from 'react'

/** Fired on window when notifications are read, so the bell counts again. */
export const NOTIFICATIONS_CHANGED = 'skinloom:notifications'

/** A bell with the number of unread notifications; checked again on every page. */
export function NotificationBell() {
  const t = useTranslations('notifications')
  const pathname = usePathname()
  const [unread, setUnread] = useState(0)
  useEffect(() => {
    let live = true
    const count = () =>
      countUnreadNotifications().then(
        (r) => live && setUnread((r.data as UnreadCount).unread),
        () => undefined, // a bell is not worth an error message
      )
    void count()
    window.addEventListener(NOTIFICATIONS_CHANGED, count)
    return () => {
      live = false
      window.removeEventListener(NOTIFICATIONS_CHANGED, count)
    }
  }, [pathname])
  const label = unread > 0 ? t('bellUnread', { count: unread }) : t('title')
  return (
    <Link href="/user/notifications" title={label} aria-label={label} className="relative inline-flex size-9 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground">
      <Bell className="size-4" />
      {unread > 0 && (
        <span className="absolute right-1 top-1 min-w-4 rounded-full bg-primary px-1 text-center text-[10px] font-semibold leading-4 text-primary-foreground">
          {unread > 99 ? '99+' : unread}
        </span>
      )}
    </Link>
  )
}
