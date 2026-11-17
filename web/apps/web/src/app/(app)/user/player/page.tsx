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
  const rules = await getPlayerRules(await asVisitor()).then((r) => r.data as PlayerRules)
  return <PlayersManager rules={rules} initialScore={viewer.score} />
}
