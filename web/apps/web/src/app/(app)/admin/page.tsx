import { type AdminOverview, getOverview } from '@skinloom/api-client'
import { Alert, AlertAction, AlertDescription, ButtonLink, cn, PageHeader, StatCard } from '@skinloom/ui'
import { ArrowRight, CircleAlert, Flag } from 'lucide-react'
import type { Metadata } from 'next'
import Link from 'next/link'
import { getFormatter, getLocale, getTranslations } from 'next-intl/server'

import { DailyChart } from '@/components/admin/daily-chart'
import { SendNotification } from '@/components/admin/send-notification'
import { Slot } from '@/components/slot'
import { formatBytes } from '@/lib/bytes'
import { asVisitor } from '@/lib/visitor'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('admin')
  return { title: t('overview') }
}

/** The admin console's first page: the site in figures, the last month, and what waits for review. */
export default async function AdminOverviewPage() {
  const [t, ta, tc, format, locale, o] = await Promise.all([
    getTranslations('adminOverview'),
    getTranslations('admin'),
    getTranslations('common'),
    getFormatter(),
    getLocale(),
    getOverview(await asVisitor()).then((r) => r.data as AdminOverview, () => null),
  ])
  if (!o) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title={ta('overview')} description={t('intro')} actions={<SendNotification />} />
        <Alert variant="destructive" role="alert">
          <CircleAlert />
          <AlertDescription>{t('loadFailed')}</AlertDescription>
          <AlertAction>
            <ButtonLink variant="outline" size="sm" render={<a href="/admin" />}>
              {tc('retry')}
            </ButtonLink>
          </AlertAction>
        </Alert>
      </div>
    )
  }

  const storage = formatBytes(o.storageBytes, locale)
  const pending = o.pendingReports > 0
  const recentUsers = o.days.reduce((sum, d) => sum + d.registrations, 0)
  const recentUploads = o.days.reduce((sum, d) => sum + d.uploads, 0)

  return (
    <div className="flex flex-col gap-8">
      <PageHeader title={ta('overview')} description={t('intro')} actions={<SendNotification />} />

      <div className="grid grid-cols-2 gap-3 sm:gap-4 md:grid-cols-3 xl:grid-cols-6">
        <StatCard
          label={t('users')}
          value={format.number(o.users)}
          note={recentUsers > 0 ? t('last30dDelta', { count: format.number(recentUsers) }) : undefined}
        />
        <StatCard label={t('players')} value={format.number(o.players)} />
        <StatCard
          label={t('textures')}
          value={format.number(o.textures)}
          note={
            recentUploads > 0
              ? `${t('publicTextures', { count: format.number(o.publicTextures) })} · ${t('last30dDelta', { count: format.number(recentUploads) })}`
              : t('publicTextures', { count: format.number(o.publicTextures) })
          }
        />
        <StatCard label={t('closetItems')} value={format.number(o.closetItems)} />
        <StatCard label={t('storage')} value={storage} note={t('files', { count: format.number(o.files) })} />
        {/* The one figure that asks for something: it links to the queue. */}
        <Link
          href="/admin/reports"
          className={cn(
            'group flex flex-col gap-2 rounded-xl border bg-card p-5 transition-colors hover:border-input hover:bg-muted/40',
            pending && 'border-highlight/60 bg-highlight/10 hover:border-highlight hover:bg-highlight/15',
          )}
        >
          <span className="flex items-center gap-1.5 text-caption text-muted-foreground">
            <Flag className={cn('size-3.5', pending && 'text-highlight-foreground dark:text-highlight')} aria-hidden />
            {t('pendingReports')}
          </span>
          <span className="text-3xl leading-none font-medium tabular-nums">{format.number(o.pendingReports)}</span>
          <span className="inline-flex items-center gap-1 text-xs font-medium text-primary">
            {t('review')}
            <ArrowRight className="size-3 transition-transform duration-150 group-hover:translate-x-0.5" aria-hidden />
          </span>
        </Link>
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <section className="rounded-xl border bg-card p-5">
          <DailyChart title={t('registrations')} days={o.days.map((d) => ({ date: d.date, value: d.registrations }))} />
        </section>
        <section className="rounded-xl border bg-card p-5">
          <DailyChart title={t('uploads')} days={o.days.map((d) => ({ date: d.date, value: d.uploads }))} />
        </section>
      </div>
      <Slot name="admin.dashboard.widgets" />
    </div>
  )
}
