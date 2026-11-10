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
    <div className="mx-auto flex max-w-3xl flex-col gap-6 px-4 py-8">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">{t('title')}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t('intro')}</p>
      </div>
      <MyReports />
    </div>
  )
}
