import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@skinloom/ui'
import type { Metadata } from 'next'
import Link from 'next/link'
import { getTranslations } from 'next-intl/server'

import { AuthShell } from '@/components/auth/auth-shell'
import { ForgotForm } from '@/components/auth/forgot-form'
import { getBootstrap } from '@/lib/bootstrap'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('forgot')
  return { title: t('title') }
}

export default async function ForgotPage() {
  const boot = await getBootstrap()
  const t = await getTranslations('forgot')
  return (
    <AuthShell>
      <Card>
        <CardHeader>
          <CardTitle className="text-2xl">{t('title')}</CardTitle>
          <CardDescription>{boot.auth.mail ? t('intro') : t('noMail')}</CardDescription>
        </CardHeader>
        {boot.auth.mail && (
          <CardContent>
            <ForgotForm captcha={boot.auth.captcha} />
          </CardContent>
        )}
      </Card>
      <p className="text-center text-sm">
        <Link href="/auth/login" className="font-medium underline-offset-4 hover:underline">
          {t('back')}
        </Link>
      </p>
    </AuthShell>
  )
}
