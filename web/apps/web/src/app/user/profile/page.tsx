import type { Metadata } from 'next'
import { redirect } from 'next/navigation'
import { getTranslations } from 'next-intl/server'

import {
  AvatarSection,
  DeleteSection,
  EmailSection,
  LaunchersSection,
  PasswordSection,
  ProfileSection,
  SessionsSection,
  SocialSection,
} from '@/components/user/account-settings'
import { getBootstrap } from '@/lib/bootstrap'
import { getViewer } from '@/lib/viewer'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('profile')
  return { title: t('title') }
}

export default async function ProfilePage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const params = await searchParams
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
      <AvatarSection viewer={viewer} />
      <ProfileSection viewer={viewer} locales={boot.locales} />
      <PasswordSection viewer={viewer} />
      <EmailSection viewer={viewer} mail={boot.auth.mail} />
      <SocialSection
        viewer={viewer}
        linked={params.social_linked !== undefined}
        error={typeof params.social_error === 'string' ? params.social_error : undefined}
      />
      <SessionsSection />
      <LaunchersSection />
      {admin ? <p className="text-sm text-muted-foreground">{t('adminNoDelete')}</p> : <DeleteSection viewer={viewer} />}
    </div>
  )
}
