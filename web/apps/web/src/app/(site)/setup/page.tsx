import type { Metadata } from 'next'
import { redirect } from 'next/navigation'
import { getTranslations } from 'next-intl/server'

import { SetupWizard } from '@/components/setup/setup-wizard'
import { getBootstrap } from '@/lib/bootstrap'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('setup')
  // The URL carries a one-time secret: keep it out of search engines and Referer headers.
  return { title: t('title'), robots: { index: false }, referrer: 'no-referrer' }
}

export default async function SetupPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const boot = await getBootstrap()
  if (!boot.offline && !boot.needsSetup) redirect('/auth/login')
  const { token } = await searchParams

  return (
    <div data-sl-area="auth" className="mx-auto flex max-w-3xl flex-col gap-6 px-4 py-8 sm:py-12">
      <SetupWizard initialToken={typeof token === 'string' ? token : ''} />
    </div>
  )
}
