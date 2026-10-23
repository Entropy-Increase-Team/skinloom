import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@skinloom/ui'
import type { Metadata } from 'next'
import { redirect } from 'next/navigation'
import { getTranslations } from 'next-intl/server'

import { LoginForm } from '@/components/auth/login-form'
import { getBootstrap } from '@/lib/bootstrap'
import { safeNext } from '@/lib/safe-url'
import { getViewer } from '@/lib/viewer'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('auth')
  return { title: t('signIn') }
}

export default async function LoginPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const next = safeNext((await searchParams).next)
  if (await getViewer()) redirect(next)
  const boot = await getBootstrap()
  const t = await getTranslations('auth')

  return (
    <div className="mx-auto flex max-w-md flex-col gap-6 px-4 py-12">
      <Card>
        <CardHeader>
          <CardTitle className="text-2xl">{t('signIn')}</CardTitle>
          <CardDescription>{t('signInTo', { site: boot.site.name })}</CardDescription>
        </CardHeader>
        <CardContent>
          <LoginForm next={next} />
        </CardContent>
      </Card>
    </div>
  )
}
