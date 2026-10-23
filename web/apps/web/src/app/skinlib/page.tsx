import type { Metadata } from 'next'
import { getTranslations } from 'next-intl/server'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('skinlib')
  return { title: t('title') }
}

// Placeholder until the texture module lands in M2; the route exists so menus and
// old Blessing Skin bookmarks (/skinlib) already resolve.
export default async function SkinLibraryPage() {
  const t = await getTranslations('skinlib')
  return (
    <div className="mx-auto max-w-6xl px-4 py-16">
      <h1 className="text-3xl font-bold tracking-tight">{t('title')}</h1>
      <p className="mt-4 text-muted-foreground">{t('body')}</p>
    </div>
  )
}
