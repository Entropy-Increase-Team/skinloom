import type { Metadata } from 'next'
import { redirect } from 'next/navigation'
import { getTranslations } from 'next-intl/server'

import { NotificationsList } from '@/components/user/notifications-list'
import { getViewer } from '@/lib/viewer'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('notifications')
  return { title: t('title') }
}

export default async function NotificationsPage() {
  if (!(await getViewer())) redirect('/auth/login?next=/user/notifications')
  return (
    <div className="mx-auto w-full max-w-3xl">
      <NotificationsList />
    </div>
  )
}
