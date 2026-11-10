import { type AdminOverview, getOverview } from '@skinloom/api-client'
import { Card, CardContent } from '@skinloom/ui'
import { Flag, HardDrive, Images, Shirt, Users, UserSquare } from 'lucide-react'
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

export default async function AdminOverviewPage() {
  const [t, format, o] = await Promise.all([
    getTranslations('adminOverview'),
    getFormatter(),
    getOverview(await asVisitor()).then((r) => r.data as AdminOverview, () => null),
  ])
  if (!o) return <p className="py-12 text-center text-sm text-destructive">{t('loadFailed')}</p>

  const mb = o.storageBytes / (1024 * 1024)
  const storage = mb >= 1 ? `${format.number(mb, { maximumFractionDigits: 1 })} MB` : `${format.number(o.storageBytes / 1024, { maximumFractionDigits: 1 })} KB`
  const tiles = [
    { icon: Users, label: t('users'), value: format.number(o.users) },
    { icon: UserSquare, label: t('players'), value: format.number(o.players) },
    { icon: Images, label: t('textures'), value: format.number(o.textures), note: t('publicTextures', { count: o.publicTextures }) },
    { icon: Shirt, label: t('closetItems'), value: format.number(o.closetItems) },
    { icon: HardDrive, label: t('storage'), value: storage, note: t('files', { count: o.files }) },
  ]

  return (
    <div className="flex flex-col gap-6">
      <dl className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-6">
        {tiles.map(({ icon: Icon, label, value, note }) => (
          <Card key={label}>
            <CardContent className="flex flex-col gap-1 pt-6">
              <dt className="flex items-center gap-1.5 text-sm text-muted-foreground">
                <Icon className="size-4" aria-hidden />
                {label}
              </dt>
              <dd className="text-2xl font-semibold">{value}</dd>
              {note && <dd className="text-xs text-muted-foreground">{note}</dd>}
            </CardContent>
          </Card>
        ))}
        <Card className={o.pendingReports > 0 ? 'border-primary' : undefined}>
          <CardContent className="flex flex-col gap-1 pt-6">
            <dt className="flex items-center gap-1.5 text-sm text-muted-foreground">
              <Flag className="size-4" aria-hidden />
              {t('pendingReports')}
            </dt>
            <dd className="text-2xl font-semibold">{format.number(o.pendingReports)}</dd>
            <dd className="text-xs">
              <Link href="/admin/reports" className="text-primary hover:underline">
                {t('review')}
              </Link>
            </dd>
          </CardContent>
        </Card>
      </dl>
      <div className="grid gap-6 md:grid-cols-2">
        <Card>
          <CardContent className="pt-6">
            <DailyChart title={t('registrations')} days={o.days.map((d) => ({ date: d.date, value: d.registrations }))} />
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <DailyChart title={t('uploads')} days={o.days.map((d) => ({ date: d.date, value: d.uploads }))} />
          </CardContent>
        </Card>
      </div>
      <SendNotification />
    </div>
  )
}
