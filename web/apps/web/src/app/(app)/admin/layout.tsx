import { notFound, redirect } from 'next/navigation'

import { AppShell } from '@/components/shell/app-shell'
import { getViewer } from '@/lib/viewer'

/** Admin area: administrators only (the API checks again on every request), in the sidebar shell. */
export default async function AdminLayout({ children }: { children: React.ReactNode }) {
  const viewer = await getViewer()
  if (!viewer) redirect('/auth/login?next=/admin')
  if (viewer.role === 'user') notFound()
  return (
    <AppShell area="admin" viewer={viewer}>
      {children}
    </AppShell>
  )
}
