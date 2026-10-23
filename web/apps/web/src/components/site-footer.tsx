import Link from 'next/link'
import { getTranslations } from 'next-intl/server'

import { getBootstrap } from '@/lib/bootstrap'

import { Slot } from './slot'

export async function SiteFooter() {
  const boot = await getBootstrap()
  const t = await getTranslations('footer')
  return (
    <footer className="border-t">
      <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-4 gap-y-2 px-4 py-6 text-sm text-muted-foreground">
        <span>
          © {new Date().getFullYear()} {boot.site.name}
        </span>
        <span aria-hidden>·</span>
        <a href="https://skinloom.cn" className="hover:text-foreground" rel="noreferrer" target="_blank">
          {t('powered')}
        </a>
        <span aria-hidden>·</span>
        <Link href="/about" className="hover:text-foreground">
          {t('thanks')}
        </Link>
        <Slot name="site.footer" />
      </div>
    </footer>
  )
}
