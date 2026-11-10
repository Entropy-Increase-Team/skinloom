import { SidebarInset } from '@skinloom/ui'
import { cookies } from 'next/headers'
import { getTranslations } from 'next-intl/server'
import type * as React from 'react'

import { getBootstrap } from '@/lib/bootstrap'
import type { Viewer } from '@/lib/viewer'

import { AppSidebar, AppSidebarProvider, type SidebarLink, type SidebarSection } from './app-sidebar'
import { SiteHeader } from './site-header'

/**
 * The frame of the user center and the admin area (design.md §9.2): the full-width
 * header, the sidebar below it, and the page in a fluid column up to 1440px.
 * Plugins' menu entries (bootstrap menus.user / menus.admin) follow in their own section.
 */
export async function AppShell({ area, viewer, children }: { area: 'user' | 'admin'; viewer: Viewer; children: React.ReactNode }) {
  const [boot, t, store] = await Promise.all([getBootstrap(), getTranslations(), cookies()])
  const saved = store.get('sidebar_state')?.value
  // Like the header: "nav.*" is a message key, anything else literal text (MenuItem.title).
  const title = (key: string) => (key.startsWith('nav.') ? t(key as 'nav.skinlib') : key)
  const plugin = (boot.menus[area] ?? []).slice().sort((a, b) => a.order - b.order).map((m) => ({ href: m.href, label: title(m.title), icon: m.icon }))

  let sections: SidebarSection[]
  let footer: SidebarLink | undefined
  if (area === 'user') {
    sections = [
      { links: [{ href: '/user', label: t('userNav.overview'), icon: 'layout-dashboard', exact: true }] },
      {
        label: t('userNav.mine'),
        links: [
          { href: '/user/player', label: t('userNav.players'), icon: 'person-standing' },
          { href: '/user/closet', label: t('userNav.closet'), icon: 'shirt' },
          { href: '/skinlib/upload', label: t('userNav.upload'), icon: 'upload' },
        ],
      },
      {
        label: t('userNav.account'),
        links: [
          { href: '/user/profile', label: t('userNav.profile'), icon: 'user-cog' },
          { href: '/user/notifications', label: t('userNav.notifications'), icon: 'bell' },
          { href: '/user/reports', label: t('userNav.reports'), icon: 'flag' },
        ],
      },
      { label: t('userNav.developer'), links: [{ href: '/user/oauth/manage', label: t('userNav.apps'), icon: 'code-xml' }] },
    ]
    if (plugin.length) sections.push({ label: t('userNav.plugins'), links: plugin })
    if (viewer.role !== 'user') footer = { href: '/admin', label: t('shell.admin'), icon: 'shield' }
  } else {
    sections = [
      { links: [{ href: '/admin', label: t('admin.overview'), icon: 'layout-dashboard', exact: true }] },
      {
        label: t('adminNav.content'),
        links: [
          { href: '/admin/users', label: t('admin.users'), icon: 'users-round' },
          { href: '/admin/players', label: t('admin.players'), icon: 'person-standing' },
          { href: '/admin/reports', label: t('admin.reports'), icon: 'flag' },
          { href: '/admin/audit', label: t('admin.audit'), icon: 'scroll-text' },
        ],
      },
      {
        label: t('adminNav.site'),
        links: [
          { href: '/admin/appearance', label: t('admin.appearance'), icon: 'palette' },
          { href: '/admin/translations', label: t('admin.translations'), icon: 'languages' },
          { href: '/admin/settings', label: t('admin.settings'), icon: 'settings' },
        ],
      },
      { label: t('adminNav.system'), links: [{ href: '/admin/status', label: t('admin.systemStatus'), icon: 'activity' }] },
    ]
    if (plugin.length) sections.push({ label: t('adminNav.plugins'), links: plugin })
    footer = { href: '/user', label: t('shell.backToUser'), icon: 'arrow-left', exact: true }
  }

  return (
    <div data-sl-area={area} className="contents">
      <AppSidebarProvider initialOpen={saved === undefined ? undefined : saved === 'true'}>
        <SiteHeader variant="app" />
        <div className="flex flex-1">
          <AppSidebar sections={sections} footer={footer} />
          <SidebarInset className="min-w-0 bg-background">
            <main className="mx-auto flex w-full max-w-[90rem] flex-1 flex-col px-4 py-6 sm:px-6 lg:px-8 lg:py-7">{children}</main>
          </SidebarInset>
        </div>
      </AppSidebarProvider>
    </div>
  )
}
