import { getTextureRules, type TextureRules } from '@skinloom/api-client'
import { PageHeader } from '@skinloom/ui'
import type { Metadata } from 'next'
import { redirect } from 'next/navigation'
import { getTranslations } from 'next-intl/server'

import { UploadForm } from '@/components/texture/upload-form'
import { getViewer } from '@/lib/viewer'
import { asVisitor } from '@/lib/visitor'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('upload')
  return { title: t('title'), robots: { index: false } }
}

export default async function UploadPage() {
  const viewer = await getViewer()
  if (!viewer) redirect('/auth/login?next=/skinlib/upload')
  const [t, rules] = await Promise.all([
    getTranslations('upload'),
    getTextureRules(await asVisitor()).then((r) => r.data as TextureRules),
  ])
  return (
    <div className="mx-auto flex w-full max-w-[75rem] flex-col gap-8 px-4 py-8 sm:px-6 lg:px-8 lg:py-10">
      <PageHeader title={t('title')} description={t('intro', { score: viewer.score })} />
      <UploadForm rules={rules} score={viewer.score} />
    </div>
  )
}
