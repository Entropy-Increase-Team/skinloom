import { PageHeader } from '@skinloom/ui'
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
    <div className="mx-auto flex w-full min-w-0 max-w-4xl flex-col gap-6">
      <PageHeader title={t('manageTitle')} description={t('manageHint')} />
      <Developer admin={viewer.role === 'admin' || viewer.role === 'superadmin'} origin={origin} />
    </div>
  )
}
