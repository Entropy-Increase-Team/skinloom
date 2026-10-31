'use client'

import { listPlayers, type PlayerView, wearTextures } from '@skinloom/api-client'
import { Button } from '@skinloom/ui'
import Link from 'next/link'
import { useTranslations } from 'next-intl'
import { useEffect, useState } from 'react'

import { problemOf } from '@/lib/problem'

/** Puts a texture from the closet on one of the viewer's players (texture page). */
export function WearOnPlayer({ textureId, kind }: { textureId: number; kind: 'skin' | 'cape' }) {
  const t = useTranslations('player')
  const tc = useTranslations('common')
  const [players, setPlayers] = useState<PlayerView[] | null>(null)
  const [chosen, setChosen] = useState<number>(0)
  const [state, setState] = useState<'idle' | 'pending' | 'done' | 'failed' | 'unverified'>('idle')

  useEffect(() => {
    listPlayers()
      .then((r) => {
        const list = r.data as PlayerView[]
        setPlayers(list)
        if (list[0]) setChosen(list[0].id)
      })
      .catch(() => setPlayers([]))
  }, [])

  if (players === null) return null
  if (players.length === 0) {
    return (
      <Link href="/user/player" className="text-sm text-muted-foreground underline-offset-4 hover:underline">
        {t('createFirst')}
      </Link>
    )
  }
  return (
    <div className="flex flex-col gap-2">
      <div className="flex gap-2">
        <select
          value={chosen}
          onChange={(e) => {
            setChosen(Number(e.target.value))
            setState('idle')
          }}
          aria-label={t('choosePlayer')}
          className="h-9 min-w-0 flex-1 rounded-md border border-input bg-transparent px-2 text-sm"
        >
          {players.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
        <Button
          disabled={state === 'pending'}
          onClick={async () => {
            setState('pending')
            try {
              await wearTextures(chosen, kind === 'skin' ? { skin: textureId } : { cape: textureId })
              setState('done')
            } catch (err) {
              setState(problemOf(err).unverified ? 'unverified' : 'failed')
            }
          }}
        >
          {kind === 'skin' ? t('wearSkin') : t('wearCape')}
        </Button>
      </div>
      {state === 'done' && <p className="text-xs text-muted-foreground">{t('worn')}</p>}
      {state === 'failed' && <p className="text-xs text-destructive">{t('errors.failed')}</p>}
      {state === 'unverified' && <p className="text-xs text-destructive">{tc('verifyFirst')}</p>}
    </div>
  )
}
