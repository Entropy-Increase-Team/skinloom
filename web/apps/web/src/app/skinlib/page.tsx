import { listTextures, type TexturePage } from '@skinloom/api-client'
import { Button, Input } from '@skinloom/ui'
import { Upload } from 'lucide-react'
import type { Metadata } from 'next'
import Link from 'next/link'
import { getTranslations } from 'next-intl/server'

import { TextureGrid } from '@/components/texture/texture-grid'
import { getBootstrap } from '@/lib/bootstrap'
import { filterHref, type LibraryFilter, parseFilter, SORTS } from '@/lib/library'
import { layoutOf, textureGridClass } from '@/lib/theme'
import { getViewer } from '@/lib/viewer'
import { asVisitor } from '@/lib/visitor'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('skinlib')
  return { title: t('title'), description: t('description') }
}

const categories = [
  { value: '', key: 'all' },
  { value: 'skin', key: 'skins' },
  { value: 'steve', key: 'steve' },
  { value: 'alex', key: 'alex' },
  { value: 'cape', key: 'capes' },
] as const

async function firstPage(f: LibraryFilter): Promise<TexturePage | null> {
  try {
    const res = await listTextures(
      { ...(f.category ? { category: f.category } : {}), ...(f.q ? { q: f.q } : {}), sort: f.sort },
      await asVisitor(),
    )
    return res.data as TexturePage
  } catch {
    return null
  }
}

export default async function SkinLibraryPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const filter = parseFilter(await searchParams)
  const [page, viewer, t, boot] = await Promise.all([firstPage(filter), getViewer(), getTranslations('skinlib'), getBootstrap()])

  const pill = (active: boolean) =>
    `rounded-full px-3 py-1 text-sm transition-colors ${active ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:bg-muted hover:text-foreground'}`

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6 px-4 py-8">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">{t('title')}</h1>
          <p className="mt-1 text-sm text-muted-foreground">{t('description')}</p>
        </div>
        <Button asChild>
          <Link href={viewer ? '/skinlib/upload' : '/auth/login?next=/skinlib/upload'}>
            <Upload />
            {t('upload')}
          </Link>
        </Button>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-3 border-b pb-4">
        <nav className="flex flex-wrap gap-1" aria-label={t('category')}>
          {categories.map((c) => (
            <Link key={c.key} href={filterHref(filter, { category: c.value })} className={pill(filter.category === c.value)}>
              {t(`categories.${c.key}`)}
            </Link>
          ))}
        </nav>
        <div className="flex flex-wrap items-center gap-3">
          <nav className="flex gap-1" aria-label={t('sort')}>
            {SORTS.map((s) => (
              <Link key={s} href={filterHref(filter, { sort: s })} className={pill(filter.sort === s)}>
                {t(`sorts.${s}`)}
              </Link>
            ))}
          </nav>
          {/* A plain GET form: searching works without JavaScript and keeps the URL shareable. */}
          <form action="/skinlib" className="flex gap-2">
            {filter.category && <input type="hidden" name="category" value={filter.category} />}
            {filter.sort !== 'time' && <input type="hidden" name="sort" value={filter.sort} />}
            <Input name="q" defaultValue={filter.q} placeholder={t('search')} aria-label={t('search')} maxLength={50} className="w-48" />
          </form>
        </div>
      </div>

      {page ? (
        // Keyed by the filter, so changing it starts the list over.
        <TextureGrid key={filterHref(filter)} initial={page} filter={filter} gridClass={textureGridClass(layoutOf(boot.theme.layout, 'skinlib'))} />
      ) : (
        <p className="py-16 text-center text-muted-foreground">{t('loadFailed')}</p>
      )}
    </div>
  )
}
