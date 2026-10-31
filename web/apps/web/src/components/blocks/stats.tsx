import { getSiteStats, type SiteStats } from '@skinloom/api-client'
import { getLocale, getTranslations } from 'next-intl/server'

import { type Props, str } from './props'

/** props: title. The site's totals (accounts, players, public textures); nothing when they cannot be loaded. */
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
    ['users', stats.users],
    ['players', stats.players],
    ['textures', stats.textures],
  ] as const
  return (
    <section className="mx-auto max-w-6xl px-4 py-12">
      <h2 className="mb-6 text-2xl font-semibold tracking-tight">{str(props.title) ?? t('title')}</h2>
      <dl className="grid gap-4 sm:grid-cols-3">
        {items.map(([key, n]) => (
          <div key={key} className="rounded-xl border bg-card p-6">
            <dt className="text-sm text-muted-foreground">{t(key)}</dt>
            <dd className="mt-1 text-3xl font-semibold tabular-nums">{format.format(n)}</dd>
          </div>
        ))}
      </dl>
    </section>
  )
}
