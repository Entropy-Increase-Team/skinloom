import type { Metadata } from 'next'
import { getTranslations } from 'next-intl/server'

import { ReportsQueue } from '@/components/admin/reports-queue'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('admin')
  return { title: t('reports') }
}

export default function AdminReportsPage() {
  return <ReportsQueue />
}
