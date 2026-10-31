import type { Metadata } from 'next'
import { getTranslations } from 'next-intl/server'

import { Appearance } from '@/components/admin/appearance'
import { getViewer } from '@/lib/viewer'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('admin')
  return { title: t('appearance') }
}

export default async function AdminAppearancePage() {
  const viewer = await getViewer()
  return <Appearance superadmin={viewer?.role === 'superadmin'} />
}
