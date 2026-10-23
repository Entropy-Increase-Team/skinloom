import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@skinloom/ui'
import type { Metadata } from 'next'
import { getTranslations } from 'next-intl/server'

import { VerifyEmail } from '@/components/auth/verify-email'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('verify')
  return { title: t('title'), robots: { index: false }, referrer: 'no-referrer' }
}

export default async function VerifyPage({ params }: { params: Promise<{ token: string }> }) {
  const { token } = await params
  const t = await getTranslations('verify')
  return (
    <div className="mx-auto flex max-w-md flex-col gap-6 px-4 py-12">
      <Card>
        <CardHeader>
          <CardTitle className="text-2xl">{t('title')}</CardTitle>
          <CardDescription>{t('intro')}</CardDescription>
        </CardHeader>
        <CardContent>
          <VerifyEmail token={token} />
        </CardContent>
      </Card>
    </div>
  )
}
