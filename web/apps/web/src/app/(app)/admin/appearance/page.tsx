import { ButtonLink } from '@skinloom/ui'
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
  const [viewer, t] = await Promise.all([getViewer(), getTranslations('componentPreview')])
  return (
    <div className="flex flex-col gap-4">
      <div className="flex justify-end">
        <ButtonLink variant="outline" size="sm" render={<Link href="/admin/appearance/preview" />}>
          <LayoutGrid />
          {t('open')}
        </ButtonLink>
      </div>
      <Appearance superadmin={viewer?.role === 'superadmin'} />
    </div>
  )
}
