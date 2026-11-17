import { ButtonLink } from '@skinloom/ui'
import Link from 'next/link'
import { getTranslations } from 'next-intl/server'

import { SiteFooter } from '@/components/shell/site-footer'
import { SiteHeader } from '@/components/shell/site-header'
import { SkipLink } from '@/components/shell/skip-link'

/** Any unknown address, and pages a visitor may not see. Outside the route groups, so it brings its own frame. */
export default async function NotFound() {
  const t = await getTranslations()
  return (
    <div data-sl-area="public" className="flex min-h-svh flex-col">
      <SkipLink label={t('shell.skipToContent')} />
      <SiteHeader />
      <main id="main" tabIndex={-1} className="mx-auto flex w-full max-w-xl flex-1 scroll-mt-14 flex-col items-start gap-4 px-4 py-24 outline-none">
        <p className="font-mono text-sm text-faint-foreground">404</p>
        <h1 className="text-3xl font-medium">{t('notFound.title')}</h1>
        <p className="text-muted-foreground">{t('notFound.body')}</p>
        <ButtonLink render={<Link href="/" />}>{t('common.backHome')}</ButtonLink>
      </main>
      <SiteFooter />
    </div>
  )
}
