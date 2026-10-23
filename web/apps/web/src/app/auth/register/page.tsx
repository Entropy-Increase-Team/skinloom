import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@skinloom/ui'
import type { Metadata } from 'next'
import Link from 'next/link'
import { redirect } from 'next/navigation'
import { getTranslations } from 'next-intl/server'

import { RegisterForm } from '@/components/auth/register-form'
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
    <div className="mx-auto flex max-w-md flex-col gap-6 px-4 py-12">
      <Card>
        <CardHeader>
          <CardTitle className="text-2xl">{t('title')}</CardTitle>
          <CardDescription>{open ? t('intro', { site: boot.site.name }) : t('closed')}</CardDescription>
        </CardHeader>
        {open && (
          <CardContent>
            <RegisterForm captcha={boot.auth.captcha} />
          </CardContent>
        )}
      </Card>
      <p className="text-center text-sm text-muted-foreground">
        {t('haveAccount')}{' '}
        <Link href="/auth/login" className="font-medium text-foreground underline-offset-4 hover:underline">
          {t('signIn')}
        </Link>
      </p>
    </div>
  )
}
