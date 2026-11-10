'use client'

import type { UnreadCount } from '@skinloom/api-client'
import { useCountUnreadNotifications } from '@skinloom/api-client/query'
import { ButtonLink } from '@skinloom/ui'
import { Bell } from 'lucide-react'
import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { useEffect } from 'react'

/** Fired on window when notifications are read, so the bell counts again. */
export const NOTIFICATIONS_CHANGED = 'skinloom:notifications'

/**
 * The header's bell: a primary dot when something is unread (the number is in its
 * label). Counted again on every page, every minute while the tab is visible, and when
 * notifications are read elsewhere.
 */
export function NotificationBell() {
  const t = useTranslations('notifications')
  const pathname = usePathname()
  // A bell is not worth an error message: a failed count just shows no dot.
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
    <ButtonLink variant="ghost" size="icon" title={label} aria-label={label} className="relative" render={<Link href="/user/notifications" />}>
      <Bell />
      {unread > 0 && <span aria-hidden className="absolute top-2 right-2 size-2 rounded-full bg-primary ring-2 ring-background" />}
    </ButtonLink>
  )
}
