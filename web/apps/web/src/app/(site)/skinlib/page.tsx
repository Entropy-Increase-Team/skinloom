import { listTextures, type TexturePage } from '@skinloom/api-client'
import { ButtonLink, cn, InputGroup, InputGroupAddon, InputGroupInput, PageHeader } from '@skinloom/ui'
import { Search, Upload } from 'lucide-react'
import type { Metadata } from 'next'
import Link from 'next/link'
import { getTranslations } from 'next-intl/server'
import type * as React from 'react'

import { TextureGrid } from '@/components/texture/texture-grid'
import { getBootstrap } from '@/lib/bootstrap'
import { filterHref, type LibraryFilter, parseFilter, SORTS } from '@/lib/library'
import { layoutOf } from '@/lib/theme'
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

/** One segment of a segmented control, as a link: the filters work without JavaScript. */
function Segment({ href, active, children }: { href: string; active: boolean; children: React.ReactNode }) {
  return (
    <Link
      href={href}
      aria-current={active ? 'page' : undefined}
      className={cn(
        'inline-flex h-8 shrink-0 items-center rounded-md px-3 text-sm transition-colors pointer-coarse:h-9',
        active ? 'bg-card font-medium text-foreground shadow-xs ring-1 ring-border' : 'text-muted-foreground hover:text-foreground',
      )}
    >
      {children}
    </Link>
  )
}

export default async function SkinLibraryPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const filter = parseFilter(await searchParams)
  const [page, viewer, t, boot] = await Promise.all([firstPage(filter), getViewer(), getTranslations('skinlib'), getBootstrap()])

  return (
    <div className="mx-auto flex w-full max-w-[75rem] flex-col gap-6 px-4 py-8 sm:px-6 lg:px-8 lg:py-10">
      <PageHeader
        title={t('title')}
        description={t('description')}
        actions={
          <ButtonLink render={<Link href={viewer ? '/skinlib/upload' : '/auth/login?next=/skinlib/upload'} />}>
            <Upload />
            {t('upload')}
          </ButtonLink>
        }
      />

      <div className="flex flex-col gap-3 lg:flex-row lg:items-center">
        {/* Phones: the segments scroll sideways rather than wrap. */}
        <nav aria-label={t('category')} className="-mx-4 overflow-x-auto px-4 sm:mx-0 sm:px-0">
          <div className="flex w-max gap-0.5 rounded-lg bg-muted p-0.5">
            {categories.map((c) => (
              <Segment key={c.key} href={filterHref(filter, { category: c.value })} active={filter.category === c.value}>
                {t(`categories.${c.key}`)}
              </Segment>
            ))}
          </div>
        </nav>
        <div className="flex flex-wrap items-center gap-3 lg:ml-auto">
          <nav aria-label={t('sort')} className="flex shrink-0 gap-0.5 rounded-lg bg-muted p-0.5">
            {SORTS.map((s) => (
              <Segment key={s} href={filterHref(filter, { sort: s })} active={filter.sort === s}>
                {t(`sorts.${s}`)}
              </Segment>
            ))}
          </nav>
          {/* A plain GET form: searching works without JavaScript and keeps the URL shareable. */}
          <form action="/skinlib" role="search" className="min-w-0 flex-1 lg:w-60 lg:flex-none">
            {filter.category && <input type="hidden" name="category" value={filter.category} />}
            {filter.sort !== 'time' && <input type="hidden" name="sort" value={filter.sort} />}
            <InputGroup>
              <InputGroupAddon>
                <Search aria-hidden />
              </InputGroupAddon>
              <InputGroupInput type="search" name="q" defaultValue={filter.q} placeholder={t('search')} aria-label={t('search')} maxLength={50} />
            </InputGroup>
          </form>
        </div>
      </div>

      {page ? (
        // Keyed by the filter, so changing it starts the list over.
        <TextureGrid key={filterHref(filter)} initial={page} filter={filter} density={layoutOf(boot.theme.layout, 'skinlib')} />
      ) : (
        <p className="py-16 text-center text-muted-foreground">{t('loadFailed')}</p>
      )}
    </div>
  )
}
