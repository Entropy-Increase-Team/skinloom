'use client'

import type { UnreadCount } from '@skinloom/api-client'
import { useCountUnreadNotifications } from '@skinloom/api-client/query'
import { Bell } from 'lucide-react'
import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { useEffect } from 'react'

/** Fired on window when notifications are read, so the bell counts again. */
export const NOTIFICATIONS_CHANGED = 'skinloom:notifications'

/**
 * A bell with the number of unread notifications: counted again on every page, every
 * minute while the tab is visible, and when notifications are read elsewhere.
 */
export function NotificationBell() {
  const t = useTranslations('notifications')
  const pathname = usePathname()
  // A bell is not worth an error message: a failed count just shows no number.
  const { data: unread = 0, refetch } = useCountUnreadNotifications({
    query: { refetchInterval: 60_000, select: (r) => (r.data as UnreadCount).unread },
  })
  useEffect(() => {
    void refetch()
  }, [pathname, refetch])
  useEffect(() => {
    const count = () => void refetch()
    window.addEventListener(NOTIFICATIONS_CHANGED, count)
    return () => window.removeEventListener(NOTIFICATIONS_CHANGED, count)
  }, [refetch])
  const label = unread > 0 ? t('bellUnread', { count: unread }) : t('title')
  return (
    <Link href="/user/notifications" title={label} aria-label={label} className="relative inline-flex size-9 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground">
      <Bell className="size-4" />
      {unread > 0 && (
        <span className="absolute right-1 top-1 min-w-4 rounded-full bg-primary px-1 text-center text-[10px] font-medium leading-4 text-primary-foreground">
          {unread > 99 ? '99+' : unread}
        </span>
      )}
    </Link>
  )
}
