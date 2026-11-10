import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@skinloom/ui'
import type { Metadata } from 'next'
import { redirect } from 'next/navigation'
import { getTranslations } from 'next-intl/server'

import { SetupForm } from '@/components/auth/setup-form'
import { getBootstrap } from '@/lib/bootstrap'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('setup')
  // The URL carries a one-time secret: keep it out of search engines and Referer headers.
  return { title: t('title'), robots: { index: false }, referrer: 'no-referrer' }
}

export default async function SetupPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const boot = await getBootstrap()
  if (!boot.offline && !boot.needsSetup) redirect('/auth/login')
  const t = await getTranslations('setup')
  const { token } = await searchParams

  return (
    <div className="mx-auto flex max-w-xl flex-col gap-6 px-4 py-12">
      <Card>
        <CardHeader>
          <CardTitle className="text-2xl">{t('title')}</CardTitle>
          <CardDescription>{t('intro')}</CardDescription>
        </CardHeader>
        <CardContent>
          <SetupForm token={typeof token === 'string' ? token : ''} siteName={boot.site.name} />
        </CardContent>
      </Card>
    </div>
  )
}
