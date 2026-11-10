import type { Metadata } from 'next'
import { getTranslations } from 'next-intl/server'

import { UsersTable } from '@/components/admin/users-table'
import { getViewer } from '@/lib/viewer'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('admin')
  return { title: t('users') }
}

export default async function AdminUsersPage() {
  const viewer = await getViewer()
  return <UsersTable viewerId={viewer?.id ?? 0} superadmin={viewer?.role === 'superadmin'} />
}
