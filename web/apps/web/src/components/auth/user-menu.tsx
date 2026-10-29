'use client'

import { logout } from '@skinloom/api-client'
import { Badge, Button } from '@skinloom/ui'
import { LogOut } from 'lucide-react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { useState } from 'react'

import { Avatar } from '@/components/user/avatar'
import { NotificationBell } from '@/components/user/notification-bell'
import type { Viewer } from '@/lib/viewer'

export function UserMenu({ viewer }: { viewer: Viewer }) {
  const t = useTranslations('auth')
  const router = useRouter()
  const [pending, setPending] = useState(false)
  return (
    <div className="flex items-center gap-2 text-sm">
      <Link href="/user" className="text-muted-foreground hover:text-foreground">
        {t('dashboard')}
      </Link>
      <Link href="/user/player" className="text-muted-foreground hover:text-foreground">
        {t('players')}
      </Link>
      <Link href="/user/closet" className="text-muted-foreground hover:text-foreground">
        {t('closet')}
      </Link>
      <NotificationBell />
      <Link href="/user/profile" className="flex max-w-40 items-center gap-1.5 font-medium hover:underline" title={viewer.email}>
        <Avatar userId={viewer.id} size={24} version={viewer.avatarTextureId} className="size-6 shrink-0 rounded" />
        <span className="truncate">{viewer.nickname}</span>
      </Link>
      {viewer.role !== 'user' && (
        <Link href="/admin" title={t('admin')}>
          <Badge variant="secondary">{t(`roles.${viewer.role}`)}</Badge>
        </Link>
      )}
      <Button
        variant="ghost"
        size="icon"
        title={t('signOut')}
        aria-label={t('signOut')}
        disabled={pending}
        onClick={async () => {
          setPending(true)
          try {
            await logout()
          } finally {
            router.refresh()
            setPending(false)
          }
        }}
      >
        <LogOut />
      </Button>
    </div>
  )
}
