import { ButtonLink, cn } from '@skinloom/ui'
import Link from 'next/link'
import { getTranslations } from 'next-intl/server'

import { VerifyBanner } from '@/components/auth/verify-email'
import { Logo } from '@/components/logo'
import { NotificationBell } from '@/components/user/notification-bell'
import { getBootstrap } from '@/lib/bootstrap'
import { safeImageUrl } from '@/lib/safe-url'
import { getViewer } from '@/lib/viewer'

import { AccountMenu } from './account-menu'
import { HeaderNav, type NavLink } from './header-nav'
import { MobileMenu } from './mobile-menu'
import { SettingsMenu } from './settings'
import { AppSidebarTrigger } from './app-sidebar'

/**
 * The site's top bar (design.md §9.2). `public` sits in the page's 1200px column with a
 * menu sheet on small screens; `app` spans the width above the user center's and the
 * admin area's sidebar, whose trigger it shows on small screens.
 *
 * Visitors: language and appearance, 登录 as a text link, 注册 as the one button.
 * Signed in: notifications and the account menu (which holds language and appearance).
 */
export async function SiteHeader({ variant = 'public' }: { variant?: 'public' | 'app' }) {
  const [boot, viewer] = await Promise.all([getBootstrap(), getViewer()])
  const t = await getTranslations()
  const logo = safeImageUrl(boot.site.logo)
  const title = (key: string) => (key.startsWith('nav.') ? t(key as 'nav.skinlib') : key)
  const links: NavLink[] = [
    ...[...(boot.menus.explore ?? [])].sort((a, b) => a.order - b.order).map((m) => ({ href: m.href, label: title(m.title) })),
    { href: '/about', label: t('nav.about') },
  ]
  const canRegister = boot.auth.registration === 'open' && !boot.needsSetup
  const app = variant === 'app'

  return (
    <header className="sl-header sticky top-0 z-40 border-b bg-background/90 backdrop-blur supports-[backdrop-filter]:bg-background/75">
      <div className={cn('flex h-14 items-center gap-2 md:gap-6', app ? 'px-3 md:px-5' : 'mx-auto max-w-[75rem] px-4 sm:px-6 lg:px-8')}>
        {app && <AppSidebarTrigger />}
        <Link href="/" className="flex min-w-0 items-center gap-2.5 rounded-md">
          {logo ? (
            // An administrator's URL of any size and host: next/image would need each host configured.
            <img src={logo} alt="" className="h-7 w-auto max-w-32 shrink-0 object-contain" />
          ) : (
            <Logo className="size-7 shrink-0" />
          )}
          <span className="truncate text-[0.9375rem] font-medium">{boot.site.name}</span>
        </Link>
        <HeaderNav links={links} className="max-md:hidden" />
        <div className="ml-auto flex shrink-0 items-center gap-1">
          {viewer ? (
            <>
              <NotificationBell />
              <AccountMenu viewer={viewer} />
            </>
          ) : (
            <div className="flex items-center gap-1 max-md:hidden">
              <SettingsMenu />
              <ButtonLink variant="ghost" render={<Link href="/auth/login" />}>
                {t('nav.login')}
              </ButtonLink>
              {canRegister && <ButtonLink render={<Link href="/auth/register" />}>{t('nav.register')}</ButtonLink>}
            </div>
          )}
          {!app && <MobileMenu links={links} signIn={viewer ? undefined : '/auth/login'} register={!viewer && canRegister ? '/auth/register' : undefined} />}
        </div>
      </div>
      {boot.offline && <div className="bg-destructive/10 px-4 py-2 text-center text-sm text-destructive">{t('offline')}</div>}
      {boot.needsSetup && (
        <div className="bg-primary/10 px-4 py-2 text-center text-sm">
          {t.rich('setupPending', { code: (chunks) => <code className="font-mono">{chunks}</code> })}
        </div>
      )}
      {viewer && !viewer.emailVerified && (boot.auth.mail || boot.auth.requireVerification) && (
        <VerifyBanner email={viewer.email} required={boot.auth.requireVerification} canResend={boot.auth.mail} />
      )}
    </header>
  )
}
