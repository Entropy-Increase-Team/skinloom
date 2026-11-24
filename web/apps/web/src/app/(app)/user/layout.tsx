import { headers } from 'next/headers'
import { redirect } from 'next/navigation'

import { AppShell } from '@/components/shell/app-shell'
import { safeNext } from '@/lib/safe-url'
import { getViewer } from '@/lib/viewer'

/** The user center: signed-in users only, in the sidebar shell. */
export default async function UserLayout({ children }: { children: React.ReactNode }) {
  const viewer = await getViewer()
  if (!viewer) {
    const raw = (await headers()).get('x-pathname')
    const next = raw?.startsWith('/user') ? safeNext(raw) : '/user'
    redirect(`/auth/login?next=${encodeURIComponent(next)}`)
  }
  return (
    <AppShell area="user" viewer={viewer}>
      {children}
    </AppShell>
  )
}
