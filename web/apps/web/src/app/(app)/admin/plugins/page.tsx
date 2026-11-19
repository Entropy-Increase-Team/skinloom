import type { Metadata } from 'next'
import { getTranslations } from 'next-intl/server'

import { PluginsManager } from '@/components/admin/plugins-manager'
import { getViewer } from '@/lib/viewer'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('admin')
  return { title: t('plugins') }
}

export default async function AdminPluginsPage() {
  const viewer = await getViewer()
  return <PluginsManager superadmin={viewer?.role === 'superadmin'} />
}
