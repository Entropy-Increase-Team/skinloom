'use client'

import {
  Button,
  cn,
  MenuIcon,
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  useSidebar,
} from '@skinloom/ui'
import { PanelLeft } from 'lucide-react'
import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { useEffect, useState } from 'react'
import type * as React from 'react'

import { isCurrent } from './header-nav'

export interface SidebarLink {
  href: string
  label: string
  /** A name from the menu icon list (design.md §7.1). */
  icon?: string
  /** Current only on exactly this path (the area's overview), not below it. */
  exact?: boolean
}

export interface SidebarSection {
  label?: string
  links: SidebarLink[]
}

/**
 * Holds the sidebar's open state for the user center and the admin area. The user's
 * choice is kept in the sidebar_state cookie (read by the server for the first render);
 * without one, the sidebar starts collapsed to icons below 1024px (design.md §9.1).
 */
export function AppSidebarProvider({ initialOpen, children }: { initialOpen?: boolean; children: React.ReactNode }) {
  const [open, setOpen] = useState(initialOpen ?? true)
  useEffect(() => {
    if (initialOpen === undefined && window.innerWidth < 1024) setOpen(false)
  }, [initialOpen])
  return (
    <SidebarProvider open={open} onOpenChange={setOpen} className="flex-col">
      {children}
    </SidebarProvider>
  )
}

/** The header button that opens the sidebar (a sheet below 768px) or collapses it. */
export function AppSidebarTrigger() {
  const t = useTranslations('shell')
  const { toggleSidebar } = useSidebar()
  return (
    <Button variant="ghost" size="icon" onClick={toggleSidebar} aria-label={t('toggleSidebar')} title={t('toggleSidebar')}>
      <PanelLeft />
    </Button>
  )
}

/**
 * The user center's or the admin area's navigation (design.md §9.2): sections with
 * small labels, one icon column, the current page tinted with the primary colour.
 * Below the 56px header; icons only when collapsed, a sheet on phones.
 */
export function AppSidebar({ sections, footer }: { sections: SidebarSection[]; footer?: SidebarLink }) {
  const pathname = usePathname()
  const { isMobile, setOpenMobile } = useSidebar()
  useEffect(() => {
    if (isMobile) setOpenMobile(false) // a link was followed: put the sheet away
  }, [pathname, isMobile, setOpenMobile])

  const item = (l: SidebarLink) => (
    <SidebarMenuItem key={l.href}>
      <SidebarMenuButton isActive={isCurrent(pathname, l.href, l.exact)} tooltip={l.label} render={<Link href={l.href} />}>
        <MenuIcon name={l.icon} />
        <span>{l.label}</span>
      </SidebarMenuButton>
    </SidebarMenuItem>
  )

  return (
    <Sidebar collapsible="icon" className="sl-sidebar top-14! h-[calc(100svh-3.5rem)]!">
      <SidebarContent className="gap-0 py-2">
        {sections.map((s, i) => (
          <SidebarGroup key={s.label ?? i} className="py-1">
            {s.label && <SidebarGroupLabel className="text-faint-foreground">{s.label}</SidebarGroupLabel>}
            <SidebarGroupContent>
              <SidebarMenu className="gap-0.5">{s.links.map(item)}</SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        ))}
      </SidebarContent>
      {footer && (
        <SidebarFooter className="border-t">
          <SidebarMenu>{item(footer)}</SidebarMenu>
        </SidebarFooter>
      )}
    </Sidebar>
  )
}

/**
 * Horizontal navigation bar for the user center when the active theme sets
 * `layout.user = "tabs"` (ADR 0043). Keeps the `.sl-sidebar` hook class.
 */
export function AppTabNav({ sections, footer }: { sections: SidebarSection[]; footer?: SidebarLink }) {
  const pathname = usePathname()
  const links = sections.flatMap((s) => s.links)
  return (
    <nav className="sl-sidebar border-b bg-background">
      <div className="no-scrollbar mx-auto flex w-full max-w-[90rem] items-center gap-1 overflow-x-auto px-4 py-2 sm:px-6 lg:px-8">
        {links.map((l) => {
          const active = isCurrent(pathname, l.href, l.exact)
          return (
            <Link
              key={l.href}
              href={l.href}
              aria-current={active ? 'page' : undefined}
              className={cn(
                'inline-flex h-8 shrink-0 items-center gap-1.5 rounded-md px-3 text-sm transition-colors',
                active ? 'bg-primary/10 font-medium text-primary' : 'text-muted-foreground hover:bg-accent hover:text-foreground',
              )}
            >
              <MenuIcon name={l.icon} className="size-4" />
              <span>{l.label}</span>
            </Link>
          )
        })}
        {footer && (
          <Link
            href={footer.href}
            className="ml-auto inline-flex h-8 shrink-0 items-center gap-1.5 rounded-md px-3 text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          >
            <MenuIcon name={footer.icon} className="size-4" />
            <span>{footer.label}</span>
          </Link>
        )}
      </div>
    </nav>
  )
}
