import { type Dashboard, getDashboard, listPlayers, type PlayerView } from '@skinloom/api-client'
import { Alert, AlertAction, AlertDescription, ButtonLink, Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle, Markdown, PageHeader, PlayerCard, StatCard } from '@skinloom/ui'
import { ArrowRight, CircleAlert, Coins, PersonStanding, Plus, Upload } from 'lucide-react'
import type { Metadata } from 'next'
import Link from 'next/link'
import { redirect } from 'next/navigation'
import { getFormatter, getTranslations } from 'next-intl/server'

import { Slot } from '@/components/slot'
import { ThemeOverride } from '@/components/theme-override'
import { SignInCard } from '@/components/user/sign-in-card'
import { UserBadges } from '@/components/user/user-badges'
import { kb } from '@/lib/skin'
import { dashedUUID } from '@/lib/uuid'
import { getViewer } from '@/lib/viewer'
import { asVisitor } from '@/lib/visitor'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('dashboard')
  return { title: t('title') }
}

/** How many players the overview shows; the rest are on the players page. */
const SHOWN = 3

/** The user center's first page: the figures, the players, check-in and the site's announcement. */
export default async function DashboardPage() {
  const viewer = await getViewer()
  if (!viewer) redirect('/auth/login?next=/user')
  const visitor = await asVisitor()
  const [t, tp, tc, format, dash, players] = await Promise.all([
    getTranslations('dashboard'),
    getTranslations('player'),
    getTranslations('common'),
    getFormatter(),
    getDashboard(visitor).then((r) => r.data as Dashboard, () => null),
    listPlayers(visitor).then((r) => r.data as PlayerView[], () => null),
  ])
  if (!dash) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title={t('title')} description={t('welcome', { name: viewer.nickname })} />
        <Alert variant="destructive" role="alert">
          <CircleAlert />
          <AlertDescription>{t('loadFailed')}</AlertDescription>
          <AlertAction>
            <ButtonLink variant="outline" size="sm" render={<a href="/user" />}>
              {tc('retry')}
            </ButtonLink>
          </AlertAction>
        </Alert>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-8">
      <PageHeader
        title={t('title')}
        description={t('welcome', { name: viewer.nickname })}
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <UserBadges badges={viewer.badges} />
            {/* Keep one primary button above the fold: daily check-in leads while available. */}
            <ButtonLink variant={dash.sign.nextAt ? 'default' : 'outline'} render={<Link href="/skinlib/upload" />}>
              <Upload />
              {t('links.upload')}
            </ButtonLink>
          </div>
        }
      />

      <div className="grid grid-cols-2 gap-3 sm:gap-4 lg:grid-cols-4">
        <StatCard label={t('score')} value={format.number(dash.score)} icon={<Coins className="size-3.5 text-highlight" aria-hidden />} note={t('scoreNote')} />
        <StatCard label={t('players')} value={format.number(dash.players)} />
        <StatCard label={t('closetItems')} value={format.number(dash.closetItems)} />
        <StatCard label={t('uploads')} value={format.number(dash.uploads)} note={t('storageNote', { kb: format.number(kb(dash.storageBytes)) })} />
      </div>

      <div className="grid grid-cols-1 gap-8 lg:grid-cols-[minmax(0,1fr)_20rem]">
        <section className="flex flex-col gap-4">
          <div className="flex items-center gap-4">
            <h2 className="flex-1 text-lg font-medium">{t('myPlayers')}</h2>
            {players && players.length > 0 && (
              <ButtonLink variant="ghost" size="sm" className="pointer-coarse:h-10" render={<Link href="/user/player" />}>
                {t('links.players')}
                <ArrowRight aria-hidden />
              </ButtonLink>
            )}
          </div>
          {players === null ? (
            <Alert variant="destructive" role="alert">
              <CircleAlert />
              <AlertDescription>{tp('loadFailed')}</AlertDescription>
              <AlertAction>
                <ButtonLink variant="outline" size="sm" render={<a href="/user" />}>
                  {tc('retry')}
                </ButtonLink>
              </AlertAction>
            </Alert>
          ) : players.length === 0 ? (
            <Empty className="border border-dashed">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <PersonStanding />
                </EmptyMedia>
                <EmptyTitle>{t('noPlayers')}</EmptyTitle>
                <EmptyDescription>{t('noPlayersBody')}</EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <ButtonLink variant="outline" render={<Link href="/user/player" />}>
                  <Plus />
                  {t('createPlayer')}
                </ButtonLink>
              </EmptyContent>
            </Empty>
          ) : (
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 sm:gap-4">
              {players.slice(0, SHOWN).map((p) => (
                <ThemeOverride
                  key={p.id}
                  name="player.card"
                  props={{ id: p.id, name: p.name, uuid: dashedUUID(p.uuid), skin: p.skin, cape: p.cape, href: '/user/player', size: 'sm' }}
                  fallback={
                    <PlayerCard
                      href="/user/player"
                      linkAs={Link}
                      size="sm"
                      name={p.name}
                      uuid={dashedUUID(p.uuid)}
                      skin={p.skin && p.skin.type !== 'cape' ? { id: p.skin.id, type: p.skin.type } : null}
                      details={[
                        { label: tp('skin'), value: p.skin?.name ?? '—' },
                        { label: tp('cape'), value: p.cape?.name ?? '—' },
                      ]}
                    />
                  }
                />
              ))}
            </div>
          )}
        </section>

        <div className="flex flex-col gap-4">
          <SignInCard sign={dash.sign} />
          <Slot name="user.dashboard.widgets" />
          {dash.announcement?.trim() && (
            <section className="flex flex-col gap-2 rounded-xl border bg-card p-5" aria-label={t('announcement')}>
              <h2 className="text-sm font-medium">{t('announcement')}</h2>
              <Markdown html={dash.announcementHtml ?? ''} className="text-sm" />
            </section>
          )}
        </div>
      </div>
    </div>
  )
}
