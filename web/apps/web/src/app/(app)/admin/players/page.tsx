import { PageHeader } from '@skinloom/ui'
import type { Metadata } from 'next'
import { getTranslations } from 'next-intl/server'

import { PlayersTable } from '@/components/admin/players-table'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('admin')
  return { title: t('players') }
}

export default async function AdminPlayersPage({ searchParams }: { searchParams: Promise<{ user?: string }> }) {
  const user = Number((await searchParams).user)
  const t = await getTranslations('admin')
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={t('players')} description={t('playersIntro')} />
      <PlayersTable userId={Number.isSafeInteger(user) && user > 0 ? user : undefined} />
    </div>
  )
}
