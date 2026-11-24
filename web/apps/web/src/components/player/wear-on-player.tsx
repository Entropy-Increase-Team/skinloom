'use client'

import { type PlayerView, wearTextures } from '@skinloom/api-client'
import { useListPlayers } from '@skinloom/api-client/query'
import { Alert, AlertAction, AlertDescription, Button, ButtonLink, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, toast } from '@skinloom/ui'
import { CircleAlert, Shirt } from 'lucide-react'
import Link from 'next/link'
import { useTranslations } from 'next-intl'
import { useState } from 'react'

import { problemOf } from '@/lib/problem'

/** Puts a texture from the closet on one of the viewer's players (texture page). */
export function WearOnPlayer({ textureId, kind }: { textureId: number; kind: 'skin' | 'cape' }) {
  const t = useTranslations('player')
  const tc = useTranslations('common')
  const { data: players, isError, refetch } = useListPlayers({ query: { select: (r) => r.data as PlayerView[] } })
  const [chosen, setChosen] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  if (isError) {
    return (
      <Alert variant="destructive" role="alert">
        <CircleAlert />
        <AlertDescription>{t('loadFailed')}</AlertDescription>
        <AlertAction>
          <Button type="button" variant="outline" size="sm" onClick={() => void refetch()}>
            {tc('retry')}
          </Button>
        </AlertAction>
      </Alert>
    )
  }
  if (!players) return null
  if (players.length === 0) {
    return (
      <ButtonLink variant="outline" size="sm" className="w-fit" render={<Link href="/user/player" />}>
        {t('createFirst')}
      </ButtonLink>
    )
  }
  const value = chosen ?? String(players[0]!.id)
  const items = players.map((p) => ({ value: String(p.id), label: p.name }))

  async function wear() {
    setPending(true)
    try {
      await wearTextures(Number(value), kind === 'skin' ? { skin: textureId } : { cape: textureId })
      toast.success(t('wornOn', { name: items.find((i) => i.value === value)?.label ?? '' }))
    } catch (err) {
      const p = problemOf(err)
      toast.error(p.unverified ? tc('verifyFirst') : p.status === 422 && p.detail ? p.detail : t('errors.failed'))
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="flex gap-2">
      <Select items={items} value={value} onValueChange={(v) => v && setChosen(String(v))}>
        <SelectTrigger className="min-w-0 flex-1" aria-label={t('choosePlayer')}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {items.map((i) => (
            <SelectItem key={i.value} value={i.value}>
              {i.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Button variant="outline" loading={pending} onClick={() => void wear()}>
        <Shirt />
        {kind === 'skin' ? t('wearSkin') : t('wearCape')}
      </Button>
    </div>
  )
}
