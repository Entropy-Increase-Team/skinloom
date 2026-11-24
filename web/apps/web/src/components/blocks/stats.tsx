import { getSiteStats, type SiteStats } from '@skinloom/api-client'
import { cn } from '@skinloom/ui'
import { getLocale, getTranslations } from 'next-intl/server'

import { CONTAINER, type Props, str } from './props'

/**
 * props: title (optional). The site's totals (accounts, players, public textures) as one
 * quiet row of figures between hairlines; nothing when they cannot be loaded.
 */
export async function StatsBlock({ props }: { props: Props }) {
  let stats: SiteStats
  try {
    stats = (await getSiteStats()).data as SiteStats
  } catch {
    return null
  }
  const [t, locale] = await Promise.all([getTranslations('blocks.stats'), getLocale()])
  const format = new Intl.NumberFormat(locale)
  const items = [
    ['textures', stats.textures],
    ['users', stats.users],
    ['players', stats.players],
  ] as const
  const title = str(props.title)
  return (
    <section className={cn(CONTAINER, 'sl-block-stats py-8')}>
      {title && <h2 className="mb-4 text-lg font-medium">{title}</h2>}
      <dl className="grid grid-cols-3 border-y">
        {items.map(([key, n], i) => (
          <div key={key} className={cn('flex min-w-0 flex-col-reverse justify-end gap-1 py-5', i > 0 && 'border-l pl-4 sm:pl-6')}>
            <dt className="text-xs text-muted-foreground sm:text-caption">{t(key)}</dt>
            <dd className="text-xl font-medium tabular-nums sm:text-[1.75rem] sm:leading-tight">{format.format(n)}</dd>
          </div>
        ))}
      </dl>
    </section>
  )
}
