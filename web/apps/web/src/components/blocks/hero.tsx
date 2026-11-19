import { listTextures, type TexturePage } from '@skinloom/api-client'
import { ButtonLink, cn } from '@skinloom/ui'
import Link from 'next/link'
import { getTranslations } from 'next-intl/server'

import { ThemeOverride } from '@/components/theme-override'
import { getBootstrap } from '@/lib/bootstrap'
import { safeHref, safeImageUrl } from '@/lib/safe-url'
import { getViewer } from '@/lib/viewer'

import { HomeShowcase } from './home-showcase'
import { CONTAINER, type Props, str } from './props'

type Layout = 'split' | 'centered' | 'banner'

/**
 * props: title, subtitle, background (image URL), primaryAction, secondaryAction (paths),
 * layout (split | centered | banner), showcase (latest | popular | none).
 *
 * split (default, design.md §9.5): the text beside the showcase where the site's latest
 * public skins take turns walking. Without any public skin, it falls back to centered.
 * banner: the text over the background image, or the theme's brand gradient (.sl-hero).
 */
export async function HeroBlock({ props }: { props: Props }) {
  const [boot, viewer, t] = await Promise.all([getBootstrap(), getViewer(), getTranslations()])
  // The second button usually leads to registration, which is only useful to signed-out
  // visitors of a site that accepts registrations. Other targets are always shown.
  const join = safeHref(props.secondaryAction, '/auth/register')
  const toRegister = join === '/auth/register' || join.startsWith('/auth/register?')
  const showJoin = !toRegister || (boot.auth.registration === 'open' && !boot.needsSetup && !viewer)
  const title = str(props.title) ?? boot.site.name
  const subtitle = str(props.subtitle) ?? boot.site.description
  let layout: Layout = props.layout === 'centered' || props.layout === 'banner' ? props.layout : 'split'
  const showcase = props.showcase === 'none' ? null : props.showcase === 'popular' ? 'likes' : 'time'

  // The skins of the showcase: public ones, the same for every visitor (no session forwarded).
  let skins: TexturePage['textures'] = []
  if (layout === 'split' && showcase) {
    skins = await listTextures({ sort: showcase, category: 'skin', limit: 3 }).then(
      (r) => (r.data as TexturePage).textures,
      () => [],
    )
  }
  if (layout === 'split' && skins.length === 0) layout = 'centered'

  const banner = layout === 'banner'
  const background = banner ? safeImageUrl(props.background) : undefined
  const actions = (
    <div className={cn('flex flex-wrap gap-3', layout === 'centered' && 'justify-center', 'max-sm:flex-col max-sm:items-stretch')}>
      <ButtonLink size="lg" className={cn(banner && 'bg-primary-foreground text-primary hover:bg-primary-foreground/90')} render={<Link href={safeHref(props.primaryAction, '/skinlib')} />}>
        {t('hero.browse')}
      </ButtonLink>
      {showJoin && (
        <ButtonLink
          size="lg"
          variant="outline"
          className={cn(banner && 'border-primary-foreground/50 bg-transparent text-primary-foreground hover:bg-primary-foreground/10 hover:text-primary-foreground')}
          render={<Link href={join} />}
        >
          {t('hero.join')}
        </ButtonLink>
      )}
    </div>
  )
  const text = (
    <div className={cn('flex flex-col gap-5', layout === 'centered' && 'items-center text-center')}>
      <h1 className="font-display text-[2rem] leading-tight font-semibold sm:text-5xl sm:leading-[1.15]">{title}</h1>
      {subtitle && <p className={cn('max-w-md text-lg leading-relaxed', banner ? 'text-primary-foreground/85' : 'text-muted-foreground')}>{subtitle}</p>}
      <div className="pt-2">{actions}</div>
    </div>
  )

  const heroElement =
    layout === 'split' ? (
      <section className="sl-block-hero">
        <div className={cn(CONTAINER, 'grid items-center gap-8 py-10 sm:py-14 lg:grid-cols-[minmax(0,1fr)_minmax(0,40rem)] lg:gap-12 lg:py-20')}>
          {text}
          <HomeShowcase
            label={t(showcase === 'likes' ? 'hero.showcasePopular' : 'hero.showcase')}
            className="h-56 sm:h-[30rem] lg:h-[34rem]"
            items={skins.map((x) => ({
              id: x.id,
              name: x.name,
              skin: `/textures/${x.hash}`,
              model: x.type === 'alex' ? 'slim' : 'default',
              href: `/skinlib/show/${x.id}`,
              uploader: x.uploader ? { name: x.uploader.nickname, userId: x.uploader.id } : undefined,
              likes: x.likes,
              modelLabel: t(`texture.types.${x.type === 'alex' ? 'alex' : 'steve'}`),
            }))}
          />
        </div>
      </section>
    ) : (
      <section
        className={cn('sl-block-hero', banner && 'sl-hero relative isolate overflow-hidden bg-cover bg-center text-primary-foreground')}
        style={background ? { backgroundImage: `url(${JSON.stringify(background)})` } : undefined}
      >
        <div className={cn(CONTAINER, 'relative z-10 py-16 sm:py-24', layout === 'centered' && 'max-w-3xl')}>{text}</div>
      </section>
    )

  return <ThemeOverride name="home.hero" props={{ title, subtitle, layout, background }} fallback={heroElement} />
}
