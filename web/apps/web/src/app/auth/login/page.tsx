import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@skinloom/ui'
import type { Metadata } from 'next'
import Link from 'next/link'
import { redirect } from 'next/navigation'
import { getTranslations } from 'next-intl/server'

import { LoginForm } from '@/components/auth/login-form'
import { SocialError } from '@/components/auth/social-buttons'
import { getBootstrap } from '@/lib/bootstrap'
import { safeNext } from '@/lib/safe-url'
import { getViewer } from '@/lib/viewer'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('auth')
  return { title: t('signIn') }
}

export default async function LoginPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const params = await searchParams
  const next = safeNext(params.next)
  // From /auth/social: sign in to link the provider account just used.
  const linkPending = params.social === 'link'
  if (await getViewer()) redirect(linkPending ? '/auth/social' : next)
  const socialError = typeof params.social_error === 'string' ? params.social_error : undefined
  const boot = await getBootstrap()
  const t = await getTranslations('auth')

  return (
    <div className="mx-auto flex max-w-md flex-col gap-6 px-4 py-12">
      <Card>
        <CardHeader>
          <CardTitle className="text-2xl">{t('signIn')}</CardTitle>
          <CardDescription>{linkPending ? t('signInToLink') : t('signInTo', { site: boot.site.name })}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <SocialError code={socialError} />
          <LoginForm next={next} captcha={boot.auth.captcha} providers={boot.auth.social} linkPending={linkPending} />
        </CardContent>
      </Card>
      <div className="flex flex-col items-center gap-2 text-sm text-muted-foreground">
        {boot.auth.mail && (
          <Link href="/auth/forgot" className="underline-offset-4 hover:text-foreground hover:underline">
            {t('forgot')}
          </Link>
        )}
        {boot.auth.registration === 'open' && (
          <p>
            {t('noAccount')}{' '}
            <Link href="/auth/register" className="font-medium text-foreground underline-offset-4 hover:underline">
              {t('register')}
            </Link>
          </p>
        )}
      </div>
    </div>
  )
}
