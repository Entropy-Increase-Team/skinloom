import type { Metadata } from 'next'
import { redirect } from 'next/navigation'
import { getTranslations } from 'next-intl/server'

import { DeviceConsent } from '@/components/oauth/device'
import { getViewer } from '@/lib/viewer'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('oauth.device')
  return { title: t('pageTitle'), robots: { index: false } }
}

/** RFC 8628 Device Authorization confirmation screen (/user/oauth/device). */
export default async function DeviceAuthorizePage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>
}) {
  const raw = await searchParams
  const userCode = typeof raw.user_code === 'string' ? raw.user_code : ''
  const next = userCode ? `/user/oauth/device?user_code=${encodeURIComponent(userCode)}` : '/user/oauth/device'
  if (!(await getViewer())) redirect(`/auth/login?next=${encodeURIComponent(next)}`)
  return (
    <div className="mx-auto flex max-w-lg flex-col gap-6 px-4 py-12">
      <DeviceConsent initialCode={userCode} />
    </div>
  )
}
