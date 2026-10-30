import { type Dashboard, getDashboard } from '@skinloom/api-client'
import { Button, Card, CardContent, CardHeader, CardTitle } from '@skinloom/ui'
import { Coins, Flag, HardDrive, Images, KeyRound, Megaphone, Shirt, Upload, UserCog, Users } from 'lucide-react'
import type { Metadata } from 'next'
import Link from 'next/link'
import { redirect } from 'next/navigation'
import { getFormatter, getTranslations } from 'next-intl/server'

import { Markdown } from '@/components/markdown'
import { SignInCard } from '@/components/user/sign-in-card'
import { kb } from '@/lib/skin'
import { getViewer } from '@/lib/viewer'
import { asVisitor } from '@/lib/visitor'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('dashboard')
  return { title: t('title') }
}

export default async function DashboardPage() {
  const viewer = await getViewer()
  if (!viewer) redirect('/auth/login?next=/user')
  const [t, format, dash] = await Promise.all([
    getTranslations('dashboard'),
    getFormatter(),
    getDashboard(await asVisitor()).then((r) => r.data as Dashboard, () => null),
  ])
  if (!dash) return <p className="px-4 py-24 text-center text-sm text-destructive">{t('loadFailed')}</p>

  const stats = [
    { icon: Coins, label: t('score'), value: format.number(dash.score) },
    { icon: Users, label: t('players'), value: format.number(dash.players) },
    { icon: Shirt, label: t('closetItems'), value: format.number(dash.closetItems) },
    { icon: Upload, label: t('uploads'), value: format.number(dash.uploads) },
    { icon: HardDrive, label: t('storage'), value: t('kb', { kb: format.number(kb(dash.storageBytes)) }) },
  ]
  const links = [
    { href: '/user/player', icon: Users, label: t('links.players') },
    { href: '/user/closet', icon: Shirt, label: t('links.closet') },
    { href: '/skinlib/upload', icon: Upload, label: t('links.upload') },
    { href: '/skinlib', icon: Images, label: t('links.skinlib') },
    { href: '/user/profile', icon: UserCog, label: t('links.profile') },
    { href: '/user/reports', icon: Flag, label: t('links.reports') },
    { href: '/user/oauth/manage', icon: KeyRound, label: t('links.developer') },
  ]

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-6 px-4 py-8">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">{t('title')}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t('welcome', { name: viewer.nickname })}</p>
      </div>
      <dl className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
        {stats.map(({ icon: Icon, label, value }) => (
          <Card key={label}>
            <CardContent className="flex flex-col gap-1 pt-6">
              <dt className="flex items-center gap-1.5 text-sm text-muted-foreground">
                <Icon className="size-4" aria-hidden />
                {label}
              </dt>
              <dd className="text-2xl font-semibold tabular-nums">{value}</dd>
            </CardContent>
          </Card>
        ))}
      </dl>
      <div className="grid gap-6 md:grid-cols-2">
        <SignInCard sign={dash.sign} />
        <Card>
          <CardHeader>
            <CardTitle>{t('links.title')}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-wrap gap-2">
            {links.map(({ href, icon: Icon, label }) => (
              <Button key={href} asChild variant="outline">
                <Link href={href}>
                  <Icon />
                  {label}
                </Link>
              </Button>
            ))}
          </CardContent>
        </Card>
      </div>
      {dash.announcement?.trim() && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Megaphone className="size-4 text-primary" aria-hidden />
              {t('announcement')}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <Markdown html={dash.announcementHtml ?? ''} />
          </CardContent>
        </Card>
      )}
    </div>
  )
}
