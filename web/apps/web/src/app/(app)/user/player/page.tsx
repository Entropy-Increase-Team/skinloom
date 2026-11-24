import { getPlayerRules, type PlayerRules } from '@skinloom/api-client'
import { Alert, AlertAction, AlertDescription, ButtonLink, PageHeader } from '@skinloom/ui'
import { CircleAlert } from 'lucide-react'
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
  const [t, tc, rules] = await Promise.all([
    getTranslations('player'),
    getTranslations('common'),
    getPlayerRules(await asVisitor()).then(
      (r) => r.data as PlayerRules,
      () => null,
    ),
  ])
  if (!rules) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title={t('title')} description={t('intro')} />
        <Alert variant="destructive" role="alert">
          <CircleAlert />
          <AlertDescription>{t('loadFailed')}</AlertDescription>
          <AlertAction>
            <ButtonLink variant="outline" size="sm" render={<a href="/user/player" />}>
              {tc('retry')}
            </ButtonLink>
          </AlertAction>
        </Alert>
      </div>
    )
  }
  return <PlayersManager rules={rules} initialScore={viewer.score} />
}
