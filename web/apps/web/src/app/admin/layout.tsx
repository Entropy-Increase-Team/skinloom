import { notFound, redirect } from 'next/navigation'
import { getTranslations } from 'next-intl/server'

import { AdminNav } from '@/components/admin/admin-nav'
import { getViewer } from '@/lib/viewer'

/** Admin area: administrators only (the API checks again on every request). */
export default async function AdminLayout({ children }: { children: React.ReactNode }) {
  const viewer = await getViewer()
  if (!viewer) redirect('/auth/login?next=/admin/users')
  if (viewer.role === 'user') notFound()
  const t = await getTranslations('admin')
  const nav = [
    { href: '/admin/users', label: t('users') },
    { href: '/admin/audit', label: t('audit') },
  ]
  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6 px-4 py-8">
      <AdminNav title={t('title')} items={nav} />
      {children}
    </div>
  )
}
