'use client'

import {
  type AdminListPlayersParams,
  type AdminPlayerPage,
  type AdminPlayerView,
  adminDeletePlayer,
  adminListPlayers,
  adminUpdatePlayer,
  adminWearPlayer,
} from '@skinloom/api-client'
import { Button, Input, Label } from '@skinloom/ui'
import Link from 'next/link'
import { useLocale, useTranslations } from 'next-intl'
import { type FormEvent, useCallback, useEffect, useRef, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { problemOf } from '@/lib/problem'
import { dashedUUID } from '@/lib/uuid'

/** All players: search by name, UUID or id, or one user's; rename, give away, dress, delete. */
export function PlayersTable({ userId }: { userId?: number }) {
  const t = useTranslations('adminPlayers')
  const locale = useLocale()
  const [filter, setFilter] = useState<AdminListPlayersParams>(userId ? { user: userId } : {})
  const [players, setPlayers] = useState<AdminPlayerView[]>([])
  const [next, setNext] = useState(0)
  const [loading, setLoading] = useState(false)
  const [editing, setEditing] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)
  const latest = useRef(0)

  const load = useCallback(
    async (before?: number) => {
      const seq = ++latest.current
      setLoading(true)
      setError(null)
      try {
        const page = (await adminListPlayers({ ...filter, limit: 30, ...(before ? { before } : {}) })).data as AdminPlayerPage
        if (seq !== latest.current) return
        setPlayers((prev) => (before ? [...prev, ...page.players] : page.players))
        setNext(page.next)
      } catch {
        if (seq === latest.current) setError(t('errors.load'))
      } finally {
        if (seq === latest.current) setLoading(false)
      }
    },
    [filter, t],
  )
  useEffect(() => {
    void load()
  }, [load])

  function onSearch(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const q = String(new FormData(e.currentTarget).get('q') ?? '').trim()
    setFilter({ ...(q ? { q } : {}), ...(filter.user ? { user: filter.user } : {}) })
  }

  const date = (iso: string) => new Intl.DateTimeFormat(locale, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(iso))

  return (
    <div className="flex flex-col gap-4">
      <form onSubmit={onSearch} className="flex flex-wrap items-end gap-2">
        <div className="flex flex-col gap-1">
          <Label htmlFor="q">{t('search')}</Label>
          <Input id="q" name="q" placeholder={t('searchHint')} className="w-64" maxLength={64} />
        </div>
        <Button type="submit" variant="outline">
          {t('search')}
        </Button>
        {filter.user && (
          <Button type="button" variant="ghost" onClick={() => setFilter({ ...(filter.q ? { q: filter.q } : {}) })}>
            {t('ofUser', { id: filter.user })} ×
          </Button>
        )}
      </form>
      <FormError message={error} />
      <div className="overflow-x-auto rounded-md border">
        <table className="w-full text-sm">
          <thead className="bg-muted/50 text-left text-xs text-muted-foreground">
            <tr>
              <th className="px-3 py-2">ID</th>
              <th className="px-3 py-2">{t('name')}</th>
              <th className="px-3 py-2">{t('owner')}</th>
              <th className="px-3 py-2">{t('skin')}</th>
              <th className="px-3 py-2">{t('cape')}</th>
              <th className="px-3 py-2">{t('updated')}</th>
              <th className="px-3 py-2" />
            </tr>
          </thead>
          <tbody className="divide-y">
            {players.map((p) => (
              <PlayerRow
                key={p.id}
                p={p}
                editing={editing === p.id}
                onEdit={() => setEditing(editing === p.id ? null : p.id)}
                onUpdated={(updated) => setPlayers((list) => list.map((x) => (x.id === updated.id ? updated : x)))}
                onChanged={(updated) => {
                  setPlayers((list) => (updated ? list.map((x) => (x.id === updated.id ? updated : x)) : list.filter((x) => x.id !== p.id)))
                  setEditing(null)
                }}
                date={date}
              />
            ))}
            {players.length === 0 && !loading && (
              <tr>
                <td colSpan={7} className="px-3 py-6 text-center text-muted-foreground">
                  {t('empty')}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {next > 0 && (
        <Button variant="outline" className="self-center" disabled={loading} onClick={() => void load(next)}>
          {t('more')}
        </Button>
      )}
    </div>
  )
}

/**
 * The skin and cape fields the administrator changed (0 takes one off), compared with
 * the player as the form showed it: a transfer saved just before may have taken off
 * what the new owner's closet lacks, and an untouched field must not put it back on.
 */
export function wearChange(shown: Pick<AdminPlayerView, 'skin' | 'cape'>, skin: number, cape: number): { skin?: number; cape?: number } {
  const wear: { skin?: number; cape?: number } = {}
  if (skin !== (shown.skin?.id ?? 0)) wear.skin = skin
  if (cape !== (shown.cape?.id ?? 0)) wear.cape = cape
  return wear
}

function worn(w?: { id: number; name: string }) {
  return w ? (
    <Link href={`/skinlib/show/${w.id}`} className="hover:underline">
      {w.name} <span className="text-muted-foreground">#{w.id}</span>
    </Link>
  ) : (
    <span className="text-muted-foreground">—</span>
  )
}

function PlayerRow({
  p,
  editing,
  onEdit,
  onUpdated,
  onChanged,
  date,
}: {
  p: AdminPlayerView
  editing: boolean
  onEdit: () => void
  /** Part of an edit was saved before another part failed: show it, keep editing. */
  onUpdated: (updated: AdminPlayerView) => void
  onChanged: (updated: AdminPlayerView | null) => void
  date: (iso: string) => string
}) {
  const t = useTranslations('adminPlayers')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function save(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    const name = String(f.get('name') ?? '').trim()
    const owner = Number(f.get('owner'))
    const textureId = (k: string) => {
      const v = String(f.get(k) ?? '').trim()
      return v === '' ? 0 : Number(v)
    }
    const skin = textureId('skin')
    const cape = textureId('cape')
    setPending(true)
    setError(null)
    let updated = p
    try {
      const change: { name?: string; userId?: number } = {}
      if (name && name !== p.name) change.name = name
      if (owner > 0 && owner !== p.owner?.id) change.userId = owner
      if (change.name || change.userId) updated = (await adminUpdatePlayer(p.id, change)).data as AdminPlayerView
      const wear = wearChange(p, skin, cape)
      if (wear.skin !== undefined || wear.cape !== undefined) updated = (await adminWearPlayer(p.id, wear)).data as AdminPlayerView
      onChanged(updated)
    } catch (err) {
      if (updated !== p) onUpdated(updated)
      const prob = problemOf(err)
      setError(
        prob.status === 403
          ? t('errors.forbidden')
          : prob.fields.name === 'already taken'
            ? t('errors.taken')
            : prob.fields.name
              ? t('errors.name')
              : prob.fields.userId
                ? t('errors.owner')
                : prob.fields.skin || prob.fields.cape
                  ? t('errors.texture')
                  : t('errors.save'),
      )
    } finally {
      setPending(false)
    }
  }

  async function remove() {
    if (!window.confirm(t('deleteConfirm', { name: p.name }))) return
    setPending(true)
    try {
      await adminDeletePlayer(p.id)
      onChanged(null)
    } catch (err) {
      setError(problemOf(err).status === 403 ? t('errors.forbidden') : t('errors.save'))
      setPending(false)
    }
  }

  return (
    <>
      <tr>
        <td className="px-3 py-2 font-mono text-xs">{p.id}</td>
        <td className="px-3 py-2">
          <div className="font-medium">{p.name}</div>
          <div className="font-mono text-xs text-muted-foreground">{dashedUUID(p.uuid)}</div>
        </td>
        <td className="px-3 py-2">
          {p.owner ? (
            <>
              <div>{p.owner.nickname}</div>
              <div className="text-xs text-muted-foreground">
                #{p.owner.id} {p.owner.email}
              </div>
            </>
          ) : (
            '—'
          )}
        </td>
        <td className="px-3 py-2 text-xs">{worn(p.skin)}</td>
        <td className="px-3 py-2 text-xs">{worn(p.cape)}</td>
        <td className="px-3 py-2 text-xs">{date(p.updatedAt)}</td>
        <td className="px-3 py-2 text-right">
          <Button variant="ghost" size="sm" onClick={onEdit}>
            {editing ? t('close') : t('edit')}
          </Button>
        </td>
      </tr>
      {editing && (
        <tr>
          <td colSpan={7} className="bg-muted/30 px-3 py-4">
            <form onSubmit={save} className="flex flex-col gap-3">
              <div className="grid gap-3 sm:grid-cols-4">
                {(
                  [
                    ['name', p.name, 'text'],
                    ['owner', String(p.owner?.id ?? ''), 'number'],
                    ['skin', p.skin ? String(p.skin.id) : '', 'number'],
                    ['cape', p.cape ? String(p.cape.id) : '', 'number'],
                  ] as const
                ).map(([key, value, type]) => (
                  <div key={key} className="flex flex-col gap-1">
                    <Label htmlFor={`${key}-${p.id}`}>{t(`fields.${key}`)}</Label>
                    <Input id={`${key}-${p.id}`} name={key} type={type} min={type === 'number' ? 0 : undefined} defaultValue={value} />
                  </div>
                ))}
              </div>
              <p className="text-xs text-muted-foreground">{t('editHint')}</p>
              <FormError message={error} />
              <div className="flex gap-2">
                <Button type="submit" disabled={pending}>
                  {t('save')}
                </Button>
                <Button type="button" variant="destructive" disabled={pending} onClick={() => void remove()}>
                  {t('delete')}
                </Button>
              </div>
            </form>
          </td>
        </tr>
      )}
    </>
  )
}
