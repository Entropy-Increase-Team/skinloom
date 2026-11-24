'use client'

import { logout } from '@skinloom/api-client'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
  MenuIcon,
  UserAvatar,
} from '@skinloom/ui'
import { ChevronDown, Languages, LogOut } from 'lucide-react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { useState } from 'react'

import type { Viewer } from '@/lib/viewer'

import { SettingsMenuItems } from './settings'

/** The account entries: the same list in the menu and in the user center's sidebar. */
const LINKS = [
  { href: '/user', icon: 'layout-dashboard', key: 'overview' },
  { href: '/user/player', icon: 'person-standing', key: 'players' },
  { href: '/user/closet', icon: 'shirt', key: 'closet' },
  { href: '/user/notifications', icon: 'bell', key: 'notifications' },
  { href: '/user/profile', icon: 'user-cog', key: 'profile' },
  { href: '/user/oauth/manage', icon: 'code-xml', key: 'apps' },
] as const

/**
 * The signed-in user's menu in the header (design.md §9.2): the head of their avatar and
 * their nickname; inside, the account pages, the admin area for administrators,
 * language and appearance, and signing out.
 */
export function AccountMenu({ viewer }: { viewer: Viewer }) {
  const t = useTranslations('shell')
  const nav = useTranslations('userNav')
  const router = useRouter()
  const [pending, setPending] = useState(false)
  const admin = viewer.role !== 'user'

  async function signOut() {
    setPending(true)
    try {
      await logout()
    } finally {
      setPending(false)
      router.push('/')
      router.refresh()
    }
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        aria-label={t('account')}
        className="flex h-9 pointer-coarse:h-10 max-w-48 items-center gap-2 rounded-md pr-2 pl-1 text-sm font-medium outline-none transition-colors hover:bg-muted focus-visible:ring-3 focus-visible:ring-ring/50 data-popup-open:bg-muted"
      >
        <UserAvatar of={{ user: viewer.id, version: viewer.avatarTextureId ?? undefined }} size={24} />
        <span className="truncate max-sm:hidden">{viewer.nickname}</span>
        <ChevronDown className="size-3.5 shrink-0 text-faint-foreground" aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuGroup>
          <DropdownMenuLabel className="flex flex-col gap-0.5">
            <span className="truncate text-sm font-medium text-foreground">{viewer.nickname}</span>
            <span className="truncate text-xs font-normal text-faint-foreground">{viewer.email}</span>
          </DropdownMenuLabel>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          {LINKS.map((l) => (
            <DropdownMenuItem key={l.href} render={<Link href={l.href} />}>
              <MenuIcon name={l.icon} />
              {nav(l.key)}
            </DropdownMenuItem>
          ))}
        </DropdownMenuGroup>
        {admin && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem render={<Link href="/admin" />}>
              <MenuIcon name="shield" />
              {t('admin')}
            </DropdownMenuItem>
          </>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuSub>
          <DropdownMenuSubTrigger>
            <Languages />
            {t('settings')}
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent className="w-48">
            <SettingsMenuItems />
          </DropdownMenuSubContent>
        </DropdownMenuSub>
        <DropdownMenuSeparator />
        <DropdownMenuItem disabled={pending} onClick={() => void signOut()}>
          <LogOut />
          {t('signOut')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
