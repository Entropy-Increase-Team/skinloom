import { headers } from 'next/headers'
import { notFound, redirect } from 'next/navigation'

import { AppShell } from '@/components/shell/app-shell'
import { safeNext } from '@/lib/safe-url'
import { getViewer } from '@/lib/viewer'

/** Admin area: administrators only (the API checks again on every request), in the sidebar shell. */
export default async function AdminLayout({ children }: { children: React.ReactNode }) {
  const viewer = await getViewer()
  if (!viewer) {
    const raw = (await headers()).get('x-pathname')
    const next = raw?.startsWith('/admin') ? safeNext(raw) : '/admin'
    redirect(`/auth/login?next=${encodeURIComponent(next)}`)
  }
  if (viewer.role !== 'admin' && viewer.role !== 'superadmin') notFound()
  return (
    <AppShell area="admin" viewer={viewer}>
      {children}
    </AppShell>
  )
}
