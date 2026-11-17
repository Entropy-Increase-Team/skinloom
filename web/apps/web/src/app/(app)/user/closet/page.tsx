import { PageHeader } from '@skinloom/ui'
import type { Metadata } from 'next'
import { redirect } from 'next/navigation'
import { getTranslations } from 'next-intl/server'

import { ClosetGrid } from '@/components/texture/closet-grid'
import { getViewer } from '@/lib/viewer'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('closet')
  return { title: t('title') }
}

export default async function ClosetPage() {
  if (!(await getViewer())) redirect('/auth/login?next=/user/closet')
  const t = await getTranslations('closet')
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={t('title')} description={t('intro')} />
      <ClosetGrid />
    </div>
  )
}
