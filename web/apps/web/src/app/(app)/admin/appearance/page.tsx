import { ButtonLink, PageHeader } from '@skinloom/ui'
import { LayoutGrid } from 'lucide-react'
import type { Metadata } from 'next'
import Link from 'next/link'
import { getTranslations } from 'next-intl/server'

import { Appearance } from '@/components/admin/appearance'
import { getViewer } from '@/lib/viewer'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('admin')
  return { title: t('appearance') }
}

export default async function AdminAppearancePage() {
  const [viewer, t, ta, tp] = await Promise.all([getViewer(), getTranslations('componentPreview'), getTranslations('admin'), getTranslations('appearance')])
  return (
    <div className="flex flex-col gap-8">
      <PageHeader
        title={ta('appearance')}
        description={tp('intro')}
        actions={
          <ButtonLink variant="outline" render={<Link href="/admin/appearance/preview" />}>
            <LayoutGrid />
            {t('open')}
          </ButtonLink>
        }
      />
      <Appearance superadmin={viewer?.role === 'superadmin'} />
    </div>
  )
}
