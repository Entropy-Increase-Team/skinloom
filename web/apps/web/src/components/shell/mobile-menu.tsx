'use client'

import {
  Button,
  ButtonLink,
  cn,
  Drawer,
  DrawerClose,
  DrawerContent,
  DrawerFooter,
  DrawerHeader,
  DrawerTitle,
  DrawerTrigger,
  MenuIcon,
  ScrollArea,
  Separator,
} from '@skinloom/ui'
import { LogIn, Menu, UserPlus, X } from 'lucide-react'
import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { useEffect, useState } from 'react'

import { Logo } from '@/components/logo'

import { isCurrent, type NavLink } from './header-nav'
import { SettingsToggles } from './settings'

/**
 * Small screens (design.md §9.2, §9.4): swipeable right-side drawer with brand identity,
 * icon-aligned navigation rows, segmented preference toggles, and safe-area auth actions.
 */
export function MobileMenu({
  links,
  signIn,
  register,
  siteName,
  siteLogo,
}: {
  links: NavLink[]
  signIn?: string
  register?: string
  siteName?: string
  siteLogo?: string
}) {
  const t = useTranslations('shell')
  const nav = useTranslations('nav')
  const ui = useTranslations('ui')
  const pathname = usePathname()
  const [open, setOpen] = useState(false)
  useEffect(() => setOpen(false), [pathname])

  const navItems: (NavLink & { exact?: boolean })[] = [
    { href: '/', label: t('home'), icon: 'house', exact: true },
    ...links,
  ]
  const hasAuthActions = Boolean(signIn || register)

  return (
    <Drawer open={open} onOpenChange={setOpen} swipeDirection="right" showSwipeHandle>
      <DrawerTrigger render={<Button variant="ghost" size="icon" aria-label={t('menu')} className="md:hidden" />}>
        <Menu className="size-5" />
      </DrawerTrigger>
      <DrawerContent className="h-dvh data-[swipe-axis=x]:[--drawer-content-width:min(20.5rem,88vw)] sm:data-[swipe-axis=x]:[--drawer-content-width:20.5rem]">
        <DrawerHeader className="h-14 flex-row items-center justify-between gap-2 border-b px-4 py-0 text-left">
          <Link href="/" className="flex min-w-0 items-center gap-2.5 rounded-md outline-none focus-visible:ring-3 focus-visible:ring-ring/50">
            {siteLogo ? (
              <img src={siteLogo} alt="" className="h-6 w-auto max-w-24 shrink-0 object-contain" />
            ) : (
              <Logo className="size-6 shrink-0" />
            )}
            <DrawerTitle className="truncate text-body font-medium">{siteName || t('menu')}</DrawerTitle>
          </Link>
          <DrawerClose render={<Button variant="ghost" size="icon-sm" aria-label={ui('close')} />}>
            <X />
          </DrawerClose>
        </DrawerHeader>

        <ScrollArea className="min-h-0 flex-1">
          <div className="flex flex-col">
            <div className="px-3 pt-4 pb-3">
              <p className="px-2 pb-1.5 text-xs font-medium text-faint-foreground">{t('mainNav')}</p>
              <nav aria-label={t('mainNav')} className="flex flex-col gap-0.5">
                {navItems.map((l) => {
                  const current = isCurrent(pathname, l.href, l.exact)
                  return (
                    <Link
                      key={l.href}
                      href={l.href}
                      aria-current={current ? 'page' : undefined}
                      className={cn(
                        'flex min-h-11 items-center gap-3 rounded-lg px-2.5 text-body transition-colors outline-none focus-visible:ring-3 focus-visible:ring-ring/50',
                        current
                          ? 'bg-primary/10 font-medium text-primary'
                          : 'text-foreground hover:bg-muted',
                      )}
                    >
                      <MenuIcon
                        name={l.icon}
                        className={cn('size-4 shrink-0', current ? 'text-primary' : 'text-muted-foreground')}
                      />
                      <span className="truncate">{l.label}</span>
                      {current && <span className="ml-auto size-1.5 shrink-0 rounded-full bg-primary" aria-hidden />}
                    </Link>
                  )
                })}
              </nav>
            </div>

            <Separator />

            <div className={cn('p-4', !hasAuthActions && 'pb-[max(1rem,env(safe-area-inset-bottom))]')}>
              <SettingsToggles />
            </div>
          </div>
        </ScrollArea>

        {hasAuthActions && (
          <DrawerFooter className="border-t bg-muted/30 p-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
            {register && (
              <ButtonLink size="lg" className="w-full" render={<Link href={register} />}>
                <UserPlus aria-hidden />
                {nav('register')}
              </ButtonLink>
            )}
            {signIn && (
              <ButtonLink size="lg" variant="outline" className="w-full" render={<Link href={signIn} />}>
                <LogIn aria-hidden />
                {nav('login')}
              </ButtonLink>
            )}
          </DrawerFooter>
        )}
      </DrawerContent>
    </Drawer>
  )
}
