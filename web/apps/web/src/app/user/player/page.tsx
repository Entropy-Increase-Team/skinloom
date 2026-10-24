import { getPlayerRules, type PlayerRules } from '@skinloom/api-client'
import type { Metadata } from 'next'
import { redirect } from 'next/navigation'
import { getTranslations } from 'next-intl/server'

import { PlayersManager } from '@/components/player/players-manager'
import { getViewer } from '@/lib/viewer'
import { asVisitor } from '@/lib/visitor'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('player')
  return { title: t('title') }
}

export default async function PlayersPage() {
  const viewer = await getViewer()
  if (!viewer) redirect('/auth/login?next=/user/player')
  const [t, rules] = await Promise.all([
    getTranslations('player'),
    getPlayerRules(await asVisitor()).then((r) => r.data as PlayerRules),
  ])
  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-6 px-4 py-8">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">{t('title')}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t('intro')}</p>
      </div>
      <PlayersManager rules={rules} initialScore={viewer.score} />
    </div>
  )
}
