import type { Metadata } from 'next'
import { redirect } from 'next/navigation'
import { getTranslations } from 'next-intl/server'

import {
  DeleteSection,
  EmailSection,
  PasswordSection,
  ProfileSection,
  SessionsSection,
} from '@/components/user/account-settings'
import { getBootstrap } from '@/lib/bootstrap'
import { getViewer } from '@/lib/viewer'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('profile')
  return { title: t('title') }
}

export default async function ProfilePage() {
  const viewer = await getViewer()
  if (!viewer) redirect('/auth/login?next=/user/profile')
  const boot = await getBootstrap()
  const t = await getTranslations('profile')
  const admin = viewer.role !== 'user'

  return (
    <div className="mx-auto flex max-w-2xl flex-col gap-6 px-4 py-10">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t('title')}</h1>
        <p className="text-sm text-muted-foreground">{t('score', { score: viewer.score })}</p>
      </div>
      <ProfileSection viewer={viewer} locales={boot.locales} />
      <PasswordSection />
      <EmailSection viewer={viewer} mail={boot.auth.mail} />
      <SessionsSection />
      {admin ? <p className="text-sm text-muted-foreground">{t('adminNoDelete')}</p> : <DeleteSection />}
    </div>
  )
}
