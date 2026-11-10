import type { Metadata } from 'next'
import { getTranslations } from 'next-intl/server'

import { PlayersTable } from '@/components/admin/players-table'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('admin')
  return { title: t('players') }
}

export default async function AdminPlayersPage({ searchParams }: { searchParams: Promise<{ user?: string }> }) {
  const user = Number((await searchParams).user)
  return <PlayersTable key={user || 0} userId={Number.isSafeInteger(user) && user > 0 ? user : undefined} />
}
