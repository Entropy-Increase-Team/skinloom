import { PageHeader } from '@skinloom/ui'
import type { Metadata } from 'next'
import { redirect } from 'next/navigation'
import { getTranslations } from 'next-intl/server'

import { MyReports } from '@/components/user/my-reports'
import { getViewer } from '@/lib/viewer'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('myReports')
  return { title: t('title') }
}

export default async function MyReportsPage() {
  if (!(await getViewer())) redirect('/auth/login?next=/user/reports')
  const t = await getTranslations('myReports')
  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-6">
      <PageHeader title={t('title')} description={t('intro')} />
      <MyReports />
    </div>
  )
}
