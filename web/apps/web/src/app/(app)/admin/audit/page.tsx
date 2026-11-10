import type { Metadata } from 'next'
import { getTranslations } from 'next-intl/server'

import { AuditTable } from '@/components/admin/audit-table'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('admin')
  return { title: t('audit') }
}

export default function AdminAuditPage() {
  return <AuditTable />
}
