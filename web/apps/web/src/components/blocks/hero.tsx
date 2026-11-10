import { ButtonLink } from '@skinloom/ui'
import Link from 'next/link'
import { getTranslations } from 'next-intl/server'

import { getBootstrap } from '@/lib/bootstrap'
import { safeHref, safeImageUrl } from '@/lib/safe-url'
import { getViewer } from '@/lib/viewer'

import { type Props, str } from './props'

/** props: title, subtitle, background (image URL), primaryAction, secondaryAction (paths). */
export async function HeroBlock({ props }: { props: Props }) {
  const [boot, viewer] = await Promise.all([getBootstrap(), getViewer()])
  const t = await getTranslations('hero')
  const background = safeImageUrl(props.background)
  // The second button usually leads to registration, which is only useful to signed-out
  // visitors of a site that accepts registrations. Other targets are always shown.
  const join = safeHref(props.secondaryAction, '/auth/register')
  const toRegister = join === '/auth/register' || join.startsWith('/auth/register?')
  const showJoin = !toRegister || (boot.auth.registration === 'open' && !boot.needsSetup && !viewer)
  return (
    <section
      className="sl-hero relative isolate overflow-hidden text-primary-foreground"
      style={background ? { backgroundImage: `url(${JSON.stringify(background)})`, backgroundSize: 'cover' } : undefined}
    >
      <div className="relative z-10 mx-auto flex max-w-6xl flex-col items-start gap-6 px-4 py-24 sm:py-32">
        <h1 className="text-4xl font-bold tracking-tight sm:text-5xl">{str(props.title) ?? boot.site.name}</h1>
        <p className="max-w-2xl text-lg text-primary-foreground/85">{str(props.subtitle) ?? boot.site.description}</p>
        <div className="flex flex-wrap gap-3">
          <ButtonLink size="lg" className="bg-primary-foreground text-primary hover:bg-primary-foreground/90" render={<Link href={safeHref(props.primaryAction, '/skinlib')} />}>
            {t('browse')}
          </ButtonLink>
          {showJoin && (
            <ButtonLink size="lg" variant="outline" className="border-primary-foreground/60 bg-transparent text-primary-foreground hover:bg-primary-foreground/10 hover:text-primary-foreground" render={<Link href={join} />}>
              {t('join')}
            </ButtonLink>
          )}
        </div>
      </div>
    </section>
  )
}
