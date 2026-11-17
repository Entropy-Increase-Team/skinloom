'use client'

import { Button, ButtonLink, cn, Separator, Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger } from '@skinloom/ui'
import { Menu } from 'lucide-react'
import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { useEffect, useState } from 'react'

import { isCurrent, type NavLink } from './header-nav'
import { SettingsToggles } from './settings'

/**
 * Small screens (design.md §9.2): the header's links, sign-in and settings behind one
 * menu button, in a sheet from the right. Closes itself when a link is followed.
 */
export function MobileMenu({ links, signIn, register }: { links: NavLink[]; signIn?: string; register?: string }) {
  const t = useTranslations('shell')
  const nav = useTranslations('nav')
  const pathname = usePathname()
  const [open, setOpen] = useState(false)
  useEffect(() => setOpen(false), [pathname])
  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger render={<Button variant="ghost" size="icon" aria-label={t('menu')} className="md:hidden" />}>
        <Menu className="size-5" />
      </SheetTrigger>
      <SheetContent side="right" className="w-[min(20rem,85vw)] gap-0 p-0">
        <SheetHeader className="h-14 justify-center border-b px-4">
          <SheetTitle className="text-base font-medium">{t('menu')}</SheetTitle>
        </SheetHeader>
        <nav aria-label={t('mainNav')} className="flex flex-col gap-0.5 p-2">
          {links.map((l) => {
            const current = isCurrent(pathname, l.href)
            return (
              <Link
                key={l.href}
                href={l.href}
                aria-current={current ? 'page' : undefined}
                className={cn('flex h-11 items-center rounded-md px-3 text-[0.9375rem] transition-colors hover:bg-muted', current && 'bg-primary/10 font-medium text-primary')}
              >
                {l.label}
              </Link>
            )
          })}
        </nav>
        {(signIn || register) && (
          <>
            <Separator />
            <div className="flex flex-col gap-2 p-4">
              {register && (
                <ButtonLink size="lg" render={<Link href={register} />}>
                  {nav('register')}
                </ButtonLink>
              )}
              {signIn && (
                <ButtonLink size="lg" variant="outline" render={<Link href={signIn} />}>
                  {nav('login')}
                </ButtonLink>
              )}
            </div>
          </>
        )}
        <Separator />
        <div className="p-4">
          <SettingsToggles />
        </div>
      </SheetContent>
    </Sheet>
  )
}
