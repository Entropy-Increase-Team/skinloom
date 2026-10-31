'use client'

import {
  type ClosetItemView,
  type ClosetPage,
  createPlayer,
  deletePlayer,
  listCloset,
  listPlayers,
  type PlayerCreated,
  type PlayerDeleted,
  type PlayerRules,
  type PlayerView,
  renamePlayer,
  wearTextures,
} from '@skinloom/api-client'
import { Button, Card, CardContent, Input } from '@skinloom/ui'
import { Copy, Pencil, Trash2 } from 'lucide-react'
import { useTranslations } from 'next-intl'
import { type FormEvent, useCallback, useEffect, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { TexturePreview } from '@/components/texture/texture-preview'
import { problemOf } from '@/lib/problem'
import { dashedUUID } from '@/lib/uuid'

/** The signed-in user's players: create, rename, dress, delete. */
export function PlayersManager({ rules, initialScore }: { rules: PlayerRules; initialScore: number }) {
  const t = useTranslations('player')
  const [players, setPlayers] = useState<PlayerView[] | null>(null)
  const [score, setScore] = useState(initialScore)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    listPlayers()
      .then((r) => setPlayers(r.data as PlayerView[]))
      .catch(() => setFailed(true))
  }, [])

  const replace = (p: PlayerView) => setPlayers((list) => list?.map((x) => (x.id === p.id ? p : x)) ?? null)

  return (
    <div className="flex flex-col gap-6">
      <CreatePlayer
        rules={rules}
        score={score}
        onCreated={(c) => {
          setPlayers((list) => [...(list ?? []), c.player])
          setScore(c.score)
        }}
      />
      {failed && <p className="text-sm text-destructive">{t('loadFailed')}</p>}
      {players?.length === 0 && <p className="py-8 text-center text-muted-foreground">{t('none')}</p>}
      <div className="grid gap-4 md:grid-cols-2">
        {players?.map((p) => (
          <PlayerCard
            key={p.id}
            player={p}
            rules={rules}
            onChange={replace}
            onDeleted={(refunded) => {
              setPlayers((list) => list?.filter((x) => x.id !== p.id) ?? null)
              setScore((s) => s + refunded)
            }}
          />
        ))}
      </div>
    </div>
  )
}

function useRuleHint(rules: PlayerRules) {
  const t = useTranslations('player')
  return t(`rules.${rules.nameRule}`, { min: rules.minLength, max: rules.maxLength })
}

function CreatePlayer({ rules, score, onCreated }: { rules: PlayerRules; score: number; onCreated: (c: PlayerCreated) => void }) {
  const t = useTranslations('player')
  const tc = useTranslations('common')
  const hint = useRuleHint(rules)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = e.currentTarget
    const name = String(new FormData(form).get('name') ?? '').trim()
    setPending(true)
    setError(null)
    try {
      onCreated((await createPlayer({ name })).data as PlayerCreated)
      form.reset()
    } catch (err) {
      const p = problemOf(err)
      setError(
        p.unverified
          ? tc('verifyFirst')
          : p.status === 402
            ? t('errors.score')
            : p.fields.name === 'already taken'
              ? t('errors.taken')
              : p.fields.name
                ? t('errors.name')
                : t('errors.failed'),
      )
    } finally {
      setPending(false)
    }
  }

  return (
    <Card>
      <CardContent className="flex flex-col gap-3 pt-6">
        <form onSubmit={onSubmit} className="flex flex-wrap items-start gap-2">
          <Input name="name" required maxLength={rules.maxLength} placeholder={t('newName')} aria-label={t('newName')} className="w-56" />
          <Button type="submit" disabled={pending}>
            {t('create')}
          </Button>
        </form>
        <FormError message={error} />
        <p className="text-xs text-muted-foreground">
          {hint} {t('cost', { cost: rules.cost, score })}
          {rules.refund && rules.cost > 0 ? ` ${t('refundHint')}` : ''}
        </p>
      </CardContent>
    </Card>
  )
}

function PlayerCard({
  player,
  rules,
  onChange,
  onDeleted,
}: {
  player: PlayerView
  rules: PlayerRules
  onChange: (p: PlayerView) => void
  onDeleted: (refunded: number) => void
}) {
  const t = useTranslations('player')
  const tc = useTranslations('common')
  const hint = useRuleHint(rules)
  const [renaming, setRenaming] = useState(false)
  const [picking, setPicking] = useState<'skin' | 'cape' | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  async function wear(body: { skin?: number; cape?: number }) {
    setError(null)
    try {
      onChange((await wearTextures(player.id, body)).data as PlayerView)
      setPicking(null)
    } catch (err) {
      setError(problemOf(err).unverified ? tc('verifyFirst') : t('errors.failed'))
    }
  }

  async function onRename(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const name = String(new FormData(e.currentTarget).get('name') ?? '').trim()
    setError(null)
    try {
      onChange((await renamePlayer(player.id, { name })).data as PlayerView)
      setRenaming(false)
    } catch (err) {
      const p = problemOf(err)
      setError(
        p.unverified
          ? tc('verifyFirst')
          : p.fields.name === 'already taken'
            ? t('errors.taken')
            : p.fields.name
              ? `${t('errors.name')} ${hint}`
              : t('errors.failed'),
      )
    }
  }

  async function onDelete() {
    if (!window.confirm(t('deleteConfirm', { name: player.name }))) return
    try {
      onDeleted(((await deletePlayer(player.id)).data as PlayerDeleted).refunded)
    } catch (err) {
      setError(problemOf(err).unverified ? tc('verifyFirst') : t('errors.failed'))
    }
  }

  return (
    <Card>
      <CardContent className="flex gap-4 pt-6">
        <div className="flex h-40 w-24 shrink-0 flex-col items-center justify-center gap-2 rounded-md bg-muted/40">
          {player.skin ? (
            <TexturePreview src={`/textures/${player.skin.hash}`} type={player.skin.type} height={112} />
          ) : (
            <span className="px-2 text-center text-xs text-muted-foreground">{t('noSkin')}</span>
          )}
          {player.cape && <TexturePreview src={`/textures/${player.cape.hash}`} type="cape" height={24} />}
        </div>
        <div className="flex min-w-0 flex-1 flex-col gap-2">
          {renaming ? (
            <form onSubmit={onRename} className="flex gap-2">
              <Input name="name" defaultValue={player.name} required maxLength={rules.maxLength} aria-label={t('rename')} autoFocus />
              <Button type="submit" size="sm">
                {t('save')}
              </Button>
            </form>
          ) : (
            <div className="flex items-center gap-1">
              <h3 className="truncate text-lg font-semibold">{player.name}</h3>
              <Button variant="ghost" size="icon" title={t('rename')} aria-label={t('rename')} onClick={() => setRenaming(true)}>
                <Pencil />
              </Button>
            </div>
          )}
          <button
            type="button"
            className="flex items-center gap-1 self-start font-mono text-xs text-muted-foreground hover:text-foreground"
            title={t('copyUUID')}
            onClick={() => {
              void navigator.clipboard?.writeText(dashedUUID(player.uuid)).then(() => setCopied(true))
            }}
          >
            {dashedUUID(player.uuid)}
            <Copy className="size-3" aria-hidden />
            {copied && <span className="font-sans">{t('copied')}</span>}
          </button>
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 text-sm">
            <dt className="text-muted-foreground">{t('skin')}</dt>
            <dd className="truncate">{player.skin?.name ?? '—'}</dd>
            <dt className="text-muted-foreground">{t('cape')}</dt>
            <dd className="truncate">{player.cape?.name ?? '—'}</dd>
          </dl>
          <FormError message={error} />
          <div className="flex flex-wrap gap-2">
            <Button size="sm" variant="outline" onClick={() => setPicking(picking === 'skin' ? null : 'skin')}>
              {t('chooseSkin')}
            </Button>
            <Button size="sm" variant="outline" onClick={() => setPicking(picking === 'cape' ? null : 'cape')}>
              {t('chooseCape')}
            </Button>
            {player.skin && (
              <Button size="sm" variant="ghost" onClick={() => void wear({ skin: 0 })}>
                {t('removeSkin')}
              </Button>
            )}
            {player.cape && (
              <Button size="sm" variant="ghost" onClick={() => void wear({ cape: 0 })}>
                {t('removeCape')}
              </Button>
            )}
            <Button size="sm" variant="ghost" className="text-destructive" title={t('delete')} aria-label={t('delete')} onClick={() => void onDelete()}>
              <Trash2 />
            </Button>
          </div>
          {picking && <ClosetPicker category={picking} onPick={(id) => void wear(picking === 'skin' ? { skin: id } : { cape: id })} />}
          <a href={`/${encodeURIComponent(player.name)}.json`} target="_blank" rel="noreferrer" className="self-start text-xs text-muted-foreground hover:underline">
            {t('profileLink')}
          </a>
        </div>
      </CardContent>
    </Card>
  )
}

/** Skins or capes from the closet, to put on a player. */
function ClosetPicker({ category, onPick }: { category: 'skin' | 'cape'; onPick: (textureId: number) => void }) {
  const t = useTranslations('player')
  const [items, setItems] = useState<ClosetItemView[] | null>(null)
  const load = useCallback(async () => {
    try {
      setItems(((await listCloset({ category, limit: 100 })).data as ClosetPage).items)
    } catch {
      setItems([])
    }
  }, [category])
  useEffect(() => {
    void load()
  }, [load])

  if (items === null) return <p className="text-xs text-muted-foreground">…</p>
  if (items.length === 0) return <p className="text-xs text-muted-foreground">{t('closetEmpty')}</p>
  return (
    <div className="grid max-h-64 grid-cols-3 gap-2 overflow-y-auto rounded-md border p-2 sm:grid-cols-4">
      {items.map((it) => (
        <button
          key={it.id}
          type="button"
          onClick={() => onPick(it.texture.id)}
          className="flex flex-col items-center gap-1 rounded-md p-1 text-xs hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring"
          title={it.name}
        >
          <TexturePreview src={`/textures/${it.texture.hash}`} type={it.texture.type} height={category === 'cape' ? 48 : 64} />
          <span className="w-full truncate">{it.name}</span>
        </button>
      ))}
    </div>
  )
}
