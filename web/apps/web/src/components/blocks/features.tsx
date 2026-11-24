import { cn } from '@skinloom/ui'
import { Gamepad2, PersonStanding, Server, Upload } from 'lucide-react'
import { getTranslations } from 'next-intl/server'

import { CONTAINER } from './props'

const items = [
  { key: 'upload', Icon: Upload },
  { key: 'players', Icon: PersonStanding },
  { key: 'launcher', Icon: Gamepad2 },
  { key: 'selfhost', Icon: Server },
] as const

/** What the site offers: a short introduction beside a two-column list, no cards (design.md §6.1). */
export async function FeaturesBlock() {
  const t = await getTranslations('features')
  return (
    <section className={cn(CONTAINER, 'sl-block-features grid gap-10 py-12 lg:grid-cols-[22rem_minmax(0,1fr)] lg:gap-20 lg:py-16')}>
      <div className="flex flex-col gap-3">
        <h2 className="text-xl font-medium sm:text-[1.375rem]">{t('title')}</h2>
        <p className="text-body text-muted-foreground">{t('intro')}</p>
      </div>
      <ul className="grid gap-x-12 gap-y-8 sm:grid-cols-2">
        {items.map(({ key, Icon }) => (
          <li key={key} className="flex gap-3.5">
            <span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-muted">
              <Icon className="size-4 text-foreground" aria-hidden />
            </span>
            <div className="flex flex-col gap-1.5">
              <h3 className="text-base font-medium">{t(`${key}.title`)}</h3>
              <p className="text-sm leading-relaxed text-muted-foreground">{t(`${key}.body`)}</p>
            </div>
          </li>
        ))}
      </ul>
    </section>
  )
}
