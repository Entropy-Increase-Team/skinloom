'use client'

import { cn } from '@skinloom/ui'
import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { useTranslations } from 'next-intl'

export interface NavLink {
  href: string
  label: string
}

/** Whether `pathname` is in the section `href` leads to. */
export function isCurrent(pathname: string, href: string, exact = false): boolean {
  if (href === '/') return pathname === '/'
  return pathname === href || (!exact && pathname.startsWith(`${href}/`))
}

/** The header's site links, the current section in the foreground. */
export function HeaderNav({ links, className }: { links: NavLink[]; className?: string }) {
  const pathname = usePathname()
  const t = useTranslations('shell')
  return (
    <nav aria-label={t('mainNav')} className={cn('flex items-center gap-1', className)}>
      {links.map((l) => {
        const current = isCurrent(pathname, l.href)
        return (
          <Link
            key={l.href}
            href={l.href}
            aria-current={current ? 'page' : undefined}
            className={cn(
              'rounded-md px-2.5 py-1.5 text-sm transition-colors hover:text-foreground',
              current ? 'font-medium text-foreground' : 'text-muted-foreground',
            )}
          >
            {l.label}
          </Link>
        )
      })}
    </nav>
  )
}
