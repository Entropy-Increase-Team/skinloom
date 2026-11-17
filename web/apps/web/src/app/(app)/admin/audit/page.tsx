import { PageHeader } from '@skinloom/ui'
import type { Metadata } from 'next'
import { getTranslations } from 'next-intl/server'

import { AuditTable } from '@/components/admin/audit-table'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('admin')
  return { title: t('audit') }
}

export default async function AdminAuditPage() {
  const t = await getTranslations('admin')
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={t('audit')} description={t('auditIntro')} />
      <AuditTable />
    </div>
  )
}
