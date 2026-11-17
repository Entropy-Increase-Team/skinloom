import { listTextures, type TexturePage } from '@skinloom/api-client'
import { ButtonLink, cn, TextureCard } from '@skinloom/ui'
import { ArrowRight } from 'lucide-react'
import Link from 'next/link'
import { getTranslations } from 'next-intl/server'

import { getBootstrap } from '@/lib/bootstrap'
import { filterHref, type Sort } from '@/lib/library'
import { layoutOf } from '@/lib/theme'

import { CONTAINER, num, type Props, str } from './props'

/**
 * props: title, limit (4–24), category (skin or cape). Public textures only: the list
 * is loaded without the visitor's session, so a home page shows the same to everyone.
 * One row of dual-view cards; on phones it scrolls sideways. Nothing is shown when the
 * library is empty or cannot be loaded.
 */
async function TexturesBlock({ props, sort }: { props: Props; sort: Sort }) {
  const limit = num(props.limit, 4, 24, 5)
  const category = props.category === 'skin' || props.category === 'cape' ? props.category : ''
  let page: TexturePage
  try {
    page = (await listTextures({ sort, limit, ...(category ? { category } : {}) })).data as TexturePage
  } catch {
    return null
  }
  if (page.textures.length === 0) return null
  const [boot, t] = await Promise.all([getBootstrap(), getTranslations()])
  const density = layoutOf(boot.theme.layout, 'skinlib')
  const popular = sort === 'likes'
  return (
    <section className={cn(CONTAINER, 'sl-block-textures flex flex-col gap-5 py-10', popular ? 'sl-block-popular-textures' : 'sl-block-latest-textures')}>
      <div className="flex items-end gap-4">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <h2 className="text-xl font-medium sm:text-[1.375rem]">{str(props.title) ?? t(popular ? 'blocks.popular' : 'blocks.latest')}</h2>
          <p className="text-sm text-muted-foreground max-sm:hidden">{t(popular ? 'blocks.popularDesc' : 'blocks.latestDesc')}</p>
        </div>
        <ButtonLink variant="ghost" size="sm" className="shrink-0" render={<Link href={filterHref({ category, q: '', sort })} />}>
          {t('blocks.more')}
          <ArrowRight aria-hidden />
        </ButtonLink>
      </div>
      <div
        className={cn(
          // Phones: one row that scrolls sideways, the next card peeking in.
          'flex snap-x gap-3 overflow-x-auto pb-1 sm:grid sm:gap-4 sm:overflow-visible sm:pb-0 [&>*]:shrink-0 [&>*]:snap-start max-sm:[&>*]:w-40',
          density === 'compact' ? 'sm:grid-cols-4 md:grid-cols-6 lg:grid-cols-8' : 'sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5',
        )}
      >
        {page.textures.map((x) => (
          <TextureCard
            key={x.id}
            linkAs={Link}
            href={`/skinlib/show/${x.id}`}
            density={density}
            texture={{ ...x, uploader: x.uploader ? { name: x.uploader.nickname, userId: x.uploader.id } : null }}
            likesLabel={t('texture.likes')}
          />
        ))}
      </div>
    </section>
  )
}

export function LatestTexturesBlock({ props }: { props: Props }) {
  return <TexturesBlock props={props} sort="time" />
}

export function PopularTexturesBlock({ props }: { props: Props }) {
  return <TexturesBlock props={props} sort="likes" />
}
