'use client'

import {
  Button,
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
