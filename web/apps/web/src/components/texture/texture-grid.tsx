'use client'

import { listTextures, type TexturePage, type TextureView } from '@skinloom/api-client'
import { Alert, AlertAction, AlertDescription, Button, ButtonLink, Empty, EmptyContent, EmptyHeader, EmptyMedia, EmptyTitle, TextureCard } from '@skinloom/ui'
import { CircleAlert, SearchX, Shirt, Upload } from 'lucide-react'
import Link from 'next/link'
import { useTranslations } from 'next-intl'
import { useCallback, useEffect, useRef, useState } from 'react'

import { Slot } from '@/components/slot'
import { ThemeOverride } from '@/components/theme-override'
import { filterHref, type LibraryFilter } from '@/lib/library'
import { textureGridClass } from '@/lib/theme'

/**
 * The skin library grid: the first page comes from the server, later pages load on scroll
 * with a button fallback (design.md §10.3). density is the one the theme picked (layout `skinlib`).
 */
export function TextureGrid({
  initial,
  filter,
  density,
  uploadHref,
}: {
  initial: TexturePage
  filter: LibraryFilter
  density: 'comfortable' | 'compact'
  uploadHref?: string
}) {
  const t = useTranslations('skinlib')
  const tt = useTranslations('texture')
  const tc = useTranslations('common')
  const [items, setItems] = useState<TextureView[]>(initial.textures)
  const [next, setNext] = useState(initial.next)
  const [loading, setLoading] = useState(false)
  const [failed, setFailed] = useState(false)
  const sentinelRef = useRef<HTMLDivElement>(null)
  const busyRef = useRef(false)

  const more = useCallback(async () => {
    if (!next || busyRef.current) return
    busyRef.current = true
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
      busyRef.current = false
      setLoading(false)
    }
  }, [filter.category, filter.q, filter.sort, next])

  useEffect(() => {
    if (!next || loading || failed) return
    const el = sentinelRef.current
    if (!el || typeof IntersectionObserver === 'undefined') return
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) void more()
      },
      { rootMargin: '300px' },
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [next, loading, failed, more])

  if (items.length === 0) {
    const filtered = Boolean(filter.q || filter.category)
    return (
      <Empty className="border border-dashed py-16">
        <EmptyHeader>
          <EmptyMedia variant="icon">{filtered ? <SearchX /> : <Shirt />}</EmptyMedia>
          <EmptyTitle>{filtered ? t('noMatch') : t('empty')}</EmptyTitle>
        </EmptyHeader>
        {(filtered || uploadHref) && (
          <EmptyContent>
            {filtered ? (
              <ButtonLink variant="outline" size="sm" render={<Link href={filterHref({ category: '', q: '', sort: filter.sort })} />}>
                {t('clearFilter')}
              </ButtonLink>
            ) : (
              uploadHref && (
                <ButtonLink variant="outline" size="sm" render={<Link href={uploadHref} />}>
                  <Upload />
                  {t('upload')}
                </ButtonLink>
              )
            )}
          </EmptyContent>
        )}
      </Empty>
    )
  }
  return (
    <div className="flex flex-col gap-6">
      <div className={textureGridClass(density)}>
        {items.map((x) => {
          const card = (
            <TextureCard
              linkAs={Link}
              href={`/skinlib/show/${x.id}`}
              density={density}
              texture={{ ...x, uploader: x.uploader ? { name: x.uploader.nickname, userId: x.uploader.id } : { name: tt('deletedUser') } }}
              badges={<Slot name="skinlib.card.badges" props={{ textureId: x.id, name: x.name, type: x.type, public: x.public, likes: x.likes }} />}
              privateLabel={tt('private')}
              likesLabel={tt('likes')}
            />
          )
          return (
            <ThemeOverride
              key={x.id}
              name="texture.card"
              props={{ texture: x, density, href: `/skinlib/show/${x.id}` }}
              fallback={card}
            />
          )
        })}
      </div>
      {failed && (
        <Alert variant="destructive" role="alert">
          <CircleAlert />
          <AlertDescription>{t('loadFailed')}</AlertDescription>
          {next && (
            <AlertAction>
              <Button type="button" variant="outline" size="sm" onClick={() => void more()}>
                {tc('retry')}
              </Button>
            </AlertAction>
          )}
        </Alert>
      )}
      {next && (
        <>
          <div ref={sentinelRef} className="h-px" aria-hidden />
          <Button type="button" variant="outline" className="self-center" loading={loading} onClick={() => void more()}>
            {t('more')}
          </Button>
        </>
      )}
    </div>
  )
}
