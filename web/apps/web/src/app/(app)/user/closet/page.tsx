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
    <div className="mx-auto flex max-w-6xl flex-col gap-6 px-4 py-8">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">{t('title')}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t('intro')}</p>
      </div>
      <ClosetGrid />
    </div>
  )
}
