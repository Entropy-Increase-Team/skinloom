import { Markdown } from '@skinloom/ui'
import Link from 'next/link'
import { getTranslations } from 'next-intl/server'

import { Slot } from '@/components/slot'
import { ThemeOverride } from '@/components/theme-override'
import { getBootstrap } from '@/lib/bootstrap'

/**
 * One quiet line under public pages (design.md §6.1): the site's copyright (setting
 * site.copyright, server-rendered Markdown), SkinLoom and the thanks to Blessing Skin,
 * and the site.footer slot for plugins. Stacks on small screens.
 */
export async function SiteFooter() {
  const [boot, t] = await Promise.all([getBootstrap(), getTranslations('footer')])
  const footerElement = (
    <footer className="sl-footer border-t">
      <div className="mx-auto flex max-w-[75rem] flex-col gap-2 px-4 py-6 text-caption text-faint-foreground sm:flex-row sm:flex-wrap sm:items-center sm:gap-x-3 sm:px-6 lg:px-8">
        {boot.site.copyrightHtml ? (
          <Markdown html={boot.site.copyrightHtml} className="sl-footer-text text-caption [&_p]:inline" />
        ) : (
          <span>
            © {new Date().getFullYear()} {boot.site.name}
          </span>
        )}
        <span aria-hidden className="max-sm:hidden">
          ·
        </span>
        <a href="https://skinloom.cn" className="transition-colors hover:text-foreground" rel="noreferrer" target="_blank">
          {t('powered')}
        </a>
        <span aria-hidden className="max-sm:hidden">
          ·
        </span>
        <Link href="/about" className="transition-colors hover:text-foreground">
          {t('thanks')}
        </Link>
        <Slot name="site.footer" />
      </div>
    </footer>
  )
  return <ThemeOverride name="site.footer" props={{ siteName: boot.site.name }} fallback={footerElement} />
}
