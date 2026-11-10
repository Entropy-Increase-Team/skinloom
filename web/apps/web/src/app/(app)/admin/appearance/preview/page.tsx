import { listTextures, type TexturePage } from '@skinloom/api-client'
import type { Metadata } from 'next'
import { getTranslations } from 'next-intl/server'

import { ComponentPreview } from '@/components/admin/component-preview'
import { asVisitor } from '@/lib/visitor'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('componentPreview')
  return { title: t('title') }
}

/**
 * Every component of @skinloom/ui in the active theme (design doc 07 §6.3): theme
 * authors check their tokens and CSS here, plugin authors see what they can use. The
 * skin components show the library's latest public textures.
 */
export default async function ComponentPreviewPage() {
  const textures = await listTextures({ sort: 'time' }, await asVisitor()).then(
    (r) => (r.data as TexturePage).textures.filter((x) => x.public).slice(0, 6),
    () => [],
  )
  return <ComponentPreview textures={textures} />
}
