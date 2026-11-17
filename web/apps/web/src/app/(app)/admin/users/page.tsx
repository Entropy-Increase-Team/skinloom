import { PageHeader } from '@skinloom/ui'
import type { Metadata } from 'next'
import { getTranslations } from 'next-intl/server'

import { UsersTable } from '@/components/admin/users-table'
import { getViewer } from '@/lib/viewer'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('admin')
  return { title: t('users') }
}

export default async function AdminUsersPage() {
  const [viewer, t] = await Promise.all([getViewer(), getTranslations('admin')])
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={t('users')} description={t('usersIntro')} />
      <UsersTable viewerId={viewer?.id ?? 0} superadmin={viewer?.role === 'superadmin'} />
    </div>
  )
}
