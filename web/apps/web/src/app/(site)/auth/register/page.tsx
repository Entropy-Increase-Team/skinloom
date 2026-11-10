import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@skinloom/ui'
import type { Metadata } from 'next'
import Link from 'next/link'
import { redirect } from 'next/navigation'
import { getTranslations } from 'next-intl/server'

import { AuthShell } from '@/components/auth/auth-shell'
import { RegisterForm } from '@/components/auth/register-form'
import { SocialButtons } from '@/components/auth/social-buttons'
import { getBootstrap } from '@/lib/bootstrap'
import { getViewer } from '@/lib/viewer'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('register')
  return { title: t('title') }
}

export default async function RegisterPage() {
  if (await getViewer()) redirect('/')
  const boot = await getBootstrap()
  const t = await getTranslations('register')
  const open = boot.auth.registration === 'open' && !boot.needsSetup

  return (
    <AuthShell>
      <Card>
        <CardHeader>
          <CardTitle className="text-2xl">{t('title')}</CardTitle>
          <CardDescription>{open ? t('intro', { site: boot.site.name }) : t('closed')}</CardDescription>
        </CardHeader>
        {open && (
          <CardContent className="flex flex-col gap-4">
            <RegisterForm captcha={boot.auth.captcha} />
            <SocialButtons providers={boot.auth.social} next="/user" />
          </CardContent>
        )}
      </Card>
      <p className="text-center text-sm text-muted-foreground">
        {t('haveAccount')}{' '}
        <Link href="/auth/login" className="font-medium text-foreground underline-offset-4 hover:underline">
          {t('signIn')}
        </Link>
      </p>
    </AuthShell>
  )
}
