import type { Metadata } from 'next'
import { headers } from 'next/headers'
import { redirect } from 'next/navigation'
import { getTranslations } from 'next-intl/server'

import { Developer } from '@/components/oauth/developer'
import { getBootstrap } from '@/lib/bootstrap'
import { getViewer } from '@/lib/viewer'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('oauth')
  return { title: t('manageTitle') }
}

/** Applications you registered, applications you allowed, personal access tokens (Blessing Skin's /user/oauth/manage). */
export default async function OAuthManagePage() {
  const viewer = await getViewer()
  if (!viewer) redirect('/auth/login?next=/user/oauth/manage')
  const t = await getTranslations('oauth')
  let origin = (await getBootstrap()).site.url.replace(/\/+$/, '')
  if (!origin) {
    const h = await headers()
    origin = `${h.get('x-forwarded-proto') ?? 'http'}://${h.get('x-forwarded-host') ?? h.get('host') ?? 'localhost'}`
  }
  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-6 px-4 py-8">
      <div className="flex flex-col gap-2">
        <h1 className="text-3xl font-bold tracking-tight">{t('manageTitle')}</h1>
        <p className="text-muted-foreground">{t('manageHint')}</p>
      </div>
      <Developer admin={viewer.role === 'admin' || viewer.role === 'superadmin'} origin={origin} />
    </div>
  )
}
