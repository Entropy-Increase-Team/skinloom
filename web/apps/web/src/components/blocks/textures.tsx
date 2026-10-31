import { listTextures, type TexturePage } from '@skinloom/api-client'
import Link from 'next/link'
import { getTranslations } from 'next-intl/server'

import { TextureCard } from '@/components/texture/texture-card'
import { getBootstrap } from '@/lib/bootstrap'
import { filterHref, type Sort } from '@/lib/library'
import { layoutOf, textureGridClass } from '@/lib/theme'

import { num, type Props, str } from './props'

/**
 * props: title, limit (4–24), category (skin or cape). Public textures only: the list
 * is loaded without the visitor's session, so a home page shows the same to everyone.
 * Nothing is shown when the library is empty or cannot be loaded.
 */
async function TexturesBlock({ props, sort }: { props: Props; sort: Sort }) {
  const limit = num(props.limit, 4, 24, 8)
  const category = props.category === 'skin' || props.category === 'cape' ? props.category : ''
  let page: TexturePage
  try {
    page = (await listTextures({ sort, limit, ...(category ? { category } : {}) })).data as TexturePage
  } catch {
    return null
  }
  if (page.textures.length === 0) return null
  const [boot, t] = await Promise.all([getBootstrap(), getTranslations('blocks')])
  return (
    <section className="mx-auto max-w-6xl px-4 py-12">
      <div className="mb-6 flex items-end justify-between gap-4">
        <h2 className="text-2xl font-semibold tracking-tight">{str(props.title) ?? t(sort === 'likes' ? 'popular' : 'latest')}</h2>
        <Link href={filterHref({ category, q: '', sort })} className="text-sm text-muted-foreground hover:text-foreground">
          {t('more')}
        </Link>
      </div>
      <div className={textureGridClass(layoutOf(boot.theme.layout, 'skinlib'))}>
        {page.textures.map((x) => (
          <TextureCard key={x.id} texture={x} />
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
