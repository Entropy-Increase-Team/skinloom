import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@skinloom/ui'
import type { Metadata } from 'next'
import { getTranslations } from 'next-intl/server'

import { ResetForm } from '@/components/auth/reset-form'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('reset')
  // The URL carries a one-time secret: keep it out of search engines and Referer headers.
  return { title: t('title'), robots: { index: false }, referrer: 'no-referrer' }
}

export default async function ResetPage({ params }: { params: Promise<{ token: string }> }) {
  const { token } = await params
  const t = await getTranslations('reset')
  return (
    <div className="mx-auto flex max-w-md flex-col gap-6 px-4 py-12">
      <Card>
        <CardHeader>
          <CardTitle className="text-2xl">{t('title')}</CardTitle>
          <CardDescription>{t('intro')}</CardDescription>
        </CardHeader>
        <CardContent>
          <ResetForm token={token} />
        </CardContent>
      </Card>
    </div>
  )
}
