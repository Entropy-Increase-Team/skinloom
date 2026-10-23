import { Button } from '@skinloom/ui'
import Link from 'next/link'
import { getTranslations } from 'next-intl/server'

import { getBootstrap } from '@/lib/bootstrap'
import { safeHref, safeImageUrl } from '@/lib/safe-url'

function str(v: unknown): string | undefined {
  return typeof v === 'string' && v !== '' ? v : undefined
}

/** props: title, subtitle, background (image URL), primaryAction, secondaryAction (paths). */
export async function HeroBlock({ props }: { props: Record<string, unknown> }) {
  const boot = await getBootstrap()
  const t = await getTranslations('hero')
  const background = safeImageUrl(props.background)
  return (
    <section
      className="sl-hero relative isolate overflow-hidden text-white"
      style={background ? { backgroundImage: `url(${JSON.stringify(background)})`, backgroundSize: 'cover' } : undefined}
    >
      <div className="relative z-10 mx-auto flex max-w-6xl flex-col items-start gap-6 px-4 py-24 sm:py-32">
        <h1 className="text-4xl font-bold tracking-tight sm:text-5xl">{str(props.title) ?? boot.site.name}</h1>
        <p className="max-w-2xl text-lg text-white/85">{str(props.subtitle) ?? boot.site.description}</p>
        <div className="flex flex-wrap gap-3">
          <Button asChild size="lg" className="bg-white text-slate-900 hover:bg-white/90">
            <Link href={safeHref(props.primaryAction, '/skinlib')}>{t('browse')}</Link>
          </Button>
          <Button asChild size="lg" variant="outline" className="border-white/60 bg-transparent text-white hover:bg-white/10 hover:text-white">
            <Link href={safeHref(props.secondaryAction, '/auth/register')}>{t('join')}</Link>
          </Button>
        </div>
      </div>
    </section>
  )
}
