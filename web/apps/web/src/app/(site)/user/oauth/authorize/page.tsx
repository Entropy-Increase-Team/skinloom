import type { GetAuthorizationParams } from '@skinloom/api-client'
import type { Metadata } from 'next'
import { redirect } from 'next/navigation'
import { getTranslations } from 'next-intl/server'

import { Consent } from '@/components/oauth/consent'
import { getViewer } from '@/lib/viewer'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('oauth')
  return { title: t('pageTitle'), robots: { index: false } }
}

const keys = ['response_type', 'client_id', 'redirect_uri', 'scope', 'state', 'code_challenge', 'code_challenge_method'] as const

/** The consent screen; /oauth/authorize sends applications' users here with the same query. */
export default async function AuthorizePage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const raw = await searchParams
  const params: GetAuthorizationParams = {}
  const query = new URLSearchParams()
  for (const k of keys) {
    const v = raw[k]
    if (typeof v === 'string') {
      params[k] = v
      query.set(k, v)
    }
  }
  if (!(await getViewer())) redirect(`/auth/login?next=${encodeURIComponent(`/user/oauth/authorize?${query}`)}`)
  return (
    <div className="mx-auto flex max-w-lg flex-col gap-6 px-4 py-12">
      <Consent params={params} />
    </div>
  )
}
