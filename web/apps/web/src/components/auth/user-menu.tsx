'use client'

import { logout } from '@skinloom/api-client'
import { Badge, Button } from '@skinloom/ui'
import { LogOut } from 'lucide-react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { useState } from 'react'

import type { Viewer } from '@/lib/viewer'

export function UserMenu({ viewer }: { viewer: Viewer }) {
  const t = useTranslations('auth')
  const router = useRouter()
  const [pending, setPending] = useState(false)
  return (
    <div className="flex items-center gap-2 text-sm">
      <Link href="/user/profile" className="max-w-32 truncate font-medium hover:underline" title={viewer.email}>
        {viewer.nickname}
      </Link>
      {viewer.role !== 'user' && (
        <Link href="/admin/users" title={t('admin')}>
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
