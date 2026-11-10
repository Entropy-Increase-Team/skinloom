import { redirect } from 'next/navigation'

import { AppShell } from '@/components/shell/app-shell'
import { getViewer } from '@/lib/viewer'

/** The user center: signed-in users only, in the sidebar shell. */
export default async function UserLayout({ children }: { children: React.ReactNode }) {
  const viewer = await getViewer()
  if (!viewer) redirect('/auth/login?next=/user')
  return (
    <AppShell area="user" viewer={viewer}>
      {children}
    </AppShell>
  )
}
