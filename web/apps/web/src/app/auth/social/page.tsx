import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@skinloom/ui'
import type { Metadata } from 'next'
import { getTranslations } from 'next-intl/server'

import { AuthShell } from '@/components/auth/auth-shell'
import { SocialSignup } from '@/components/auth/social-signup'
import { getBootstrap } from '@/lib/bootstrap'
import { getViewer } from '@/lib/viewer'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('social')
  return { title: t('pending.title') }
}

export default async function SocialPage() {
  const [viewer, boot, t] = await Promise.all([getViewer(), getBootstrap(), getTranslations('social')])
  return (
    <AuthShell>
      <Card>
        <CardHeader>
          <CardTitle className="text-2xl">{t('pending.title')}</CardTitle>
          <CardDescription>{t('pending.intro', { site: boot.site.name })}</CardDescription>
        </CardHeader>
        <CardContent>
          <SocialSignup signedInAs={viewer?.nickname} mail={boot.auth.mail} />
        </CardContent>
      </Card>
    </AuthShell>
  )
}
