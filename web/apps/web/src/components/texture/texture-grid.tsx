'use client'

import { listTextures, type TexturePage, type TextureView } from '@skinloom/api-client'
import { Button, ButtonLink, Empty, EmptyContent, EmptyHeader, EmptyMedia, EmptyTitle, TextureCard } from '@skinloom/ui'
import { SearchX, Shirt } from 'lucide-react'
import Link from 'next/link'
import { useTranslations } from 'next-intl'
import { useState } from 'react'

import { filterHref, type LibraryFilter } from '@/lib/library'
import { textureGridClass } from '@/lib/theme'

/**
 * The skin library grid: the first page comes from the server, later pages on demand.
 * density is the one the theme picked (layout `skinlib`).
 */
export function TextureGrid({ initial, filter, density }: { initial: TexturePage; filter: LibraryFilter; density: 'comfortable' | 'compact' }) {
  const t = useTranslations('skinlib')
  const tt = useTranslations('texture')
  const [items, setItems] = useState<TextureView[]>(initial.textures)
  const [next, setNext] = useState(initial.next)
  const [loading, setLoading] = useState(false)
  const [failed, setFailed] = useState(false)

  async function more() {
    setLoading(true)
    setFailed(false)
    try {
      const res = await listTextures({
        ...(filter.category ? { category: filter.category } : {}),
        ...(filter.q ? { q: filter.q } : {}),
        sort: filter.sort,
        cursor: next,
      })
      const page = res.data as TexturePage
      setItems((list) => [...list, ...page.textures])
      setNext(page.next)
    } catch {
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }

  if (items.length === 0) {
    const filtered = Boolean(filter.q || filter.category)
    return (
      <Empty className="border border-dashed py-16">
        <EmptyHeader>
          <EmptyMedia variant="icon">{filtered ? <SearchX /> : <Shirt />}</EmptyMedia>
          <EmptyTitle>{filtered ? t('noMatch') : t('empty')}</EmptyTitle>
        </EmptyHeader>
        {filtered && (
          <EmptyContent>
            <ButtonLink variant="outline" size="sm" render={<Link href={filterHref({ category: '', q: '', sort: filter.sort })} />}>
              {t('clearFilter')}
            </ButtonLink>
          </EmptyContent>
        )}
      </Empty>
    )
  }
  return (
    <div className="flex flex-col gap-6">
      <div className={textureGridClass(density)}>
        {items.map((x) => (
          <TextureCard
            key={x.id}
            linkAs={Link}
            href={`/skinlib/show/${x.id}`}
            density={density}
            texture={{ ...x, uploader: x.uploader ? { name: x.uploader.nickname, userId: x.uploader.id } : { name: tt('deletedUser') } }}
            privateLabel={tt('private')}
            likesLabel={tt('likes')}
          />
        ))}
      </div>
      {failed && <p className="text-center text-sm text-destructive">{t('loadFailed')}</p>}
      {next && (
        <Button variant="outline" className="self-center" loading={loading} onClick={() => void more()}>
          {t('more')}
        </Button>
      )}
    </div>
  )
}
