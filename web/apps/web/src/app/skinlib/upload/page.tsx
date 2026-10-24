import { getTextureRules, type TextureRules } from '@skinloom/api-client'
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
    <div className="mx-auto flex max-w-3xl flex-col gap-6 px-4 py-8">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">{t('title')}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t('intro', { score: viewer.score })}</p>
      </div>
      <UploadForm rules={rules} score={viewer.score} />
    </div>
  )
}
