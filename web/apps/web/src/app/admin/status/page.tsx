import type { Metadata } from 'next'
import { getTranslations } from 'next-intl/server'

import { SystemStatusView } from '@/components/admin/system-status'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('admin')
  return { title: t('systemStatus') }
}

export default function AdminStatusPage() {
  return <SystemStatusView />
}
