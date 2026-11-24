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

/** What the site offers: a short introduction beside elevated feature cards. */
export async function FeaturesBlock() {
  const t = await getTranslations('features')
  return (
    <section className="sl-block-features bg-background">
      <div className={cn(CONTAINER, 'grid gap-10 py-14 lg:grid-cols-[20rem_minmax(0,1fr)] lg:gap-14 lg:py-20')}>
        <div className="flex flex-col gap-3">
          <h2 className="text-xl font-semibold tracking-tight sm:text-2xl">{t('title')}</h2>
          <p className="text-sm leading-relaxed text-muted-foreground sm:text-base">{t('intro')}</p>
        </div>
        <ul className="grid gap-4 sm:grid-cols-2">
          {items.map(({ key, Icon }, index) => (
            <li
              key={key}
              className="group flex flex-col gap-3.5 rounded-xl border bg-card p-5 shadow-xs transition-[border-color,box-shadow,transform] duration-200 hover:-translate-y-0.5 hover:border-primary/30 hover:shadow-sm"
            >
              <div className="flex items-center justify-between gap-2">
                <span className="flex size-10 shrink-0 items-center justify-center rounded-lg border bg-primary/8 text-primary transition-colors duration-200 group-hover:border-primary/20 group-hover:bg-primary group-hover:text-primary-foreground">
                  <Icon className="size-4.5" aria-hidden />
                </span>
                <span className="font-mono text-xs tabular-nums text-faint-foreground">
                  {String(index + 1).padStart(2, '0')}
                </span>
              </div>
              <div className="flex flex-col gap-1.5">
                <h3 className="text-base font-medium tracking-tight">{t(`${key}.title`)}</h3>
                <p className="text-sm leading-relaxed text-muted-foreground">{t(`${key}.body`)}</p>
              </div>
            </li>
          ))}
        </ul>
      </div>
    </section>
  )
}
