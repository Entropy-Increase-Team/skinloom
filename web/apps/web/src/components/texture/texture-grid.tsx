'use client'

import { listTextures, type TexturePage, type TextureView } from '@skinloom/api-client'
import { Button } from '@skinloom/ui'
import { useTranslations } from 'next-intl'
import { useState } from 'react'

import type { LibraryFilter } from '@/lib/library'

import { TextureCard } from './texture-card'

/** The skin library grid: the first page comes from the server, later pages on demand. */
export function TextureGrid({ initial, filter }: { initial: TexturePage; filter: LibraryFilter }) {
  const t = useTranslations('skinlib')
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
    return <p className="py-16 text-center text-muted-foreground">{filter.q || filter.category ? t('noMatch') : t('empty')}</p>
  }
  return (
    <div className="flex flex-col gap-6">
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
        {items.map((x) => (
          <TextureCard key={x.id} texture={x} />
        ))}
      </div>
      {failed && <p className="text-center text-sm text-destructive">{t('loadFailed')}</p>}
      {next && (
        <Button variant="outline" className="self-center" disabled={loading} onClick={() => void more()}>
          {loading ? t('loading') : t('more')}
        </Button>
      )}
    </div>
  )
}
