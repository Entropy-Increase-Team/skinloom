import type { Metadata } from 'next'
import { getTranslations } from 'next-intl/server'

import { SettingsCenter } from '@/components/admin/settings-center'
import { getViewer } from '@/lib/viewer'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('admin')
  return { title: t('settings') }
}

export default async function AdminSettingsPage() {
  const viewer = await getViewer()
  return <SettingsCenter superadmin={viewer?.role === 'superadmin'} />
}
