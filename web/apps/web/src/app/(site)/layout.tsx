import { getTranslations } from 'next-intl/server'

import { SiteFooter } from '@/components/shell/site-footer'
import { SiteHeader } from '@/components/shell/site-header'
import { SkipLink } from '@/components/shell/skip-link'

/** Public pages: the header in the page column, the content, the footer (design.md §9.2). */
export default async function SiteLayout({ children }: { children: React.ReactNode }) {
  const t = await getTranslations('shell')
  return (
    <div data-sl-area="public" className="flex min-h-svh flex-col">
      <SkipLink label={t('skipToContent')} />
      <SiteHeader />
      <main id="main" tabIndex={-1} className="flex-1 scroll-mt-14 outline-none">
        {children}
      </main>
      <SiteFooter />
    </div>
  )
}
