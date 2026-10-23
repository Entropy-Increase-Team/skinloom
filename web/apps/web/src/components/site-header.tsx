import { Button } from '@skinloom/ui'
import Link from 'next/link'
import { getTranslations } from 'next-intl/server'

import { getBootstrap } from '@/lib/bootstrap'
import { getViewer } from '@/lib/viewer'

import { UserMenu } from './auth/user-menu'
import { LocaleSwitch } from './locale-switch'
import { Logo } from './logo'
import { ThemeToggle } from './theme-toggle'

export async function SiteHeader() {
  const [boot, viewer] = await Promise.all([getBootstrap(), getViewer()])
  const t = await getTranslations()
  const explore = [...(boot.menus.explore ?? [])].sort((a, b) => a.order - b.order)

  return (
    <header className="sticky top-0 z-40 border-b bg-background/80 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <div className="mx-auto flex h-14 max-w-6xl items-center gap-6 px-4">
        <Link href="/" className="flex items-center gap-2 font-semibold">
          <Logo className="size-6" />
          <span>{boot.site.name}</span>
        </Link>
        <nav className="flex items-center gap-4 text-sm text-muted-foreground">
          {explore.map((item) => (
            <Link key={item.id} href={item.href} className="transition-colors hover:text-foreground">
              {item.title.startsWith('nav.') ? t(item.title as 'nav.skinlib') : item.title}
            </Link>
          ))}
          <Link href="/about" className="transition-colors hover:text-foreground">
            {t('nav.about')}
          </Link>
        </nav>
        <div className="ml-auto flex items-center gap-2">
          <LocaleSwitch label={t('common.language')} />
          <ThemeToggle label={t('common.theme')} />
          {viewer ? (
            <UserMenu viewer={viewer} />
          ) : (
            <Button variant="ghost" size="sm" asChild>
              <Link href="/auth/login">{t('nav.login')}</Link>
            </Button>
          )}
        </div>
      </div>
      {boot.offline && (
        <div className="bg-destructive/10 px-4 py-2 text-center text-sm text-destructive">{t('offline')}</div>
      )}
      {boot.needsSetup && (
        <div className="bg-primary/10 px-4 py-2 text-center text-sm">
          {t.rich('setupPending', { code: (chunks) => <code className="font-mono">{chunks}</code> })}
        </div>
      )}
    </header>
  )
}
