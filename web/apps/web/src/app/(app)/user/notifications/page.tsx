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
  const t = await getTranslations('notifications')
  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-6 px-4 py-8">
      <h1 className="text-3xl font-bold tracking-tight">{t('title')}</h1>
      <NotificationsList />
    </div>
  )
}
