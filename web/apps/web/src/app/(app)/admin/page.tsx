import { type AdminOverview, getOverview } from '@skinloom/api-client'
import { cn, PageHeader, StatCard } from '@skinloom/ui'
import { ArrowRight, Flag } from 'lucide-react'
import type { Metadata } from 'next'
import Link from 'next/link'
import { getFormatter, getTranslations } from 'next-intl/server'

import { DailyChart } from '@/components/admin/daily-chart'
import { SendNotification } from '@/components/admin/send-notification'
import { asVisitor } from '@/lib/visitor'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('admin')
  return { title: t('overview') }
}

/** The admin console's first page: the site in figures, the last month, and what waits for review. */
export default async function AdminOverviewPage() {
  const [t, ta, format, o] = await Promise.all([
    getTranslations('adminOverview'),
    getTranslations('admin'),
    getFormatter(),
    getOverview(await asVisitor()).then((r) => r.data as AdminOverview, () => null),
  ])
  if (!o) return <p className="py-12 text-center text-sm text-destructive">{t('loadFailed')}</p>

  const mb = o.storageBytes / (1024 * 1024)
  const storage = mb >= 1 ? `${format.number(mb, { maximumFractionDigits: 1 })} MB` : `${format.number(o.storageBytes / 1024, { maximumFractionDigits: 1 })} KB`
  const pending = o.pendingReports > 0

  return (
    <div className="flex flex-col gap-8">
      <PageHeader title={ta('overview')} description={t('intro')} actions={<SendNotification />} />

      <div className="grid grid-cols-2 gap-3 sm:gap-4 md:grid-cols-3 xl:grid-cols-6">
        <StatCard label={t('users')} value={format.number(o.users)} />
        <StatCard label={t('players')} value={format.number(o.players)} />
        <StatCard label={t('textures')} value={format.number(o.textures)} note={t('publicTextures', { count: o.publicTextures })} />
        <StatCard label={t('closetItems')} value={format.number(o.closetItems)} />
        <StatCard label={t('storage')} value={storage} note={t('files', { count: o.files })} />
        {/* The one figure that asks for something: it links to the queue. */}
        <Link
          href="/admin/reports"
          className={cn(
            'group flex flex-col gap-2 rounded-xl border bg-card p-5 transition-colors hover:border-input hover:bg-muted/40',
            pending && 'border-highlight/60 bg-highlight/10 hover:border-highlight hover:bg-highlight/15',
          )}
        >
          <span className="flex items-center gap-1.5 text-caption text-muted-foreground">
            <Flag className={cn('size-3.5', pending && 'text-highlight-foreground')} aria-hidden />
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
    </div>
  )
}
