import type { Metadata } from 'next'
import { getTranslations } from 'next-intl/server'

import { PluginsManager } from '@/components/admin/plugins-manager'
import type { MarketCategory } from '@/components/admin/plugins-market'
import { getViewer } from '@/lib/viewer'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('admin')
  return { title: t('plugins') }
}

const VALID_CATEGORIES = new Set<MarketCategory>([
  'all',
  'recommended',
  'sandbox',
  'native',
  'theme',
  'security',
  'score',
  'migration',
])

export default async function AdminPluginsPage({
  searchParams,
}: {
  searchParams?: Promise<{ tab?: string; category?: string; kind?: string }>
}) {
  const viewer = await getViewer()
  const params = searchParams ? await searchParams : undefined
  const initialTab = params?.tab === 'market' ? 'market' : 'installed'
  let initialCategory: MarketCategory = 'all'
  if (params?.category && VALID_CATEGORIES.has(params.category as MarketCategory)) {
    initialCategory = params.category as MarketCategory
  } else if (params?.kind === 'theme') {
    initialCategory = 'theme'
  }
  return (
    <PluginsManager
      superadmin={viewer?.role === 'superadmin'}
      initialTab={initialTab}
      initialMarketCategory={initialCategory}
    />
  )
}
