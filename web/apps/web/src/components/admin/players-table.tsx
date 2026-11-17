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
import {
  Badge,
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  Field,
  FieldLabel,
  Input,
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  PixelBackdrop,
  RelativeTime,
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  Skeleton,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  TexturePreview,
  toast,
  useConfirm,
} from '@skinloom/ui'
import { MoreHorizontal, Pencil, Search, Trash2, X } from 'lucide-react'
import Link from 'next/link'
import { useTranslations } from 'next-intl'
import { type FormEvent, useCallback, useEffect, useRef, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { problemOf } from '@/lib/problem'
import { dashedUUID } from '@/lib/uuid'

/** All players: search by name, UUID or id, or one user's; rename, give away, dress, delete. */
export function PlayersTable({ userId }: { userId?: number }) {
  const t = useTranslations('adminPlayers')
  const confirm = useConfirm()
  const [filter, setFilter] = useState<AdminListPlayersParams>(userId ? { user: userId } : {})
  const [players, setPlayers] = useState<AdminPlayerView[] | null>(null)
  const [next, setNext] = useState(0)
  const [loading, setLoading] = useState(false)
  const [editing, setEditing] = useState<AdminPlayerView | null>(null)
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
        setPlayers((prev) => (before && prev ? [...prev, ...page.players] : page.players))
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

  const changed = (p: AdminPlayerView, updated: AdminPlayerView | null) =>
    setPlayers((list) => list && (updated ? list.map((x) => (x.id === updated.id ? updated : x)) : list.filter((x) => x.id !== p.id)))

  async function remove(p: AdminPlayerView) {
    const ok = await confirm({ title: t('deleteTitle', { name: p.name }), description: t('deleteBody'), confirmLabel: t('delete') })
    if (!ok) return
    try {
      await adminDeletePlayer(p.id)
      changed(p, null)
      toast.success(t('deleted', { name: p.name }))
    } catch (err) {
      toast.error(problemOf(err).status === 403 ? t('errors.forbidden') : t('errors.save'))
    }
  }

  function onSearch(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const q = String(new FormData(e.currentTarget).get('q') ?? '').trim()
    setFilter({ ...(q ? { q } : {}), ...(filter.user ? { user: filter.user } : {}) })
  }

  const menu = (p: AdminPlayerView) => (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button variant="ghost" size="icon-sm" aria-label={t('menu', { name: p.name })} title={t('menu', { name: p.name })} />}>
        <MoreHorizontal />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-40">
        <DropdownMenuItem onClick={() => setEditing(p)}>
          <Pencil />
          {t('edit')}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onClick={() => void remove(p)}>
          <Trash2 />
          {t('delete')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )

  const figure = (p: AdminPlayerView) => (
    <PixelBackdrop className="size-11 shrink-0 rounded-md">
      {p.skin && p.skin.type !== 'cape' && <TexturePreview texture={{ id: p.skin.id }} type={p.skin.type} height={36} />}
    </PixelBackdrop>
  )

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <form onSubmit={onSearch} role="search" className="w-full sm:w-72">
          <InputGroup>
            <InputGroupAddon>
              <Search aria-hidden />
            </InputGroupAddon>
            <InputGroupInput type="search" name="q" placeholder={t('searchHint')} aria-label={t('search')} maxLength={64} />
          </InputGroup>
        </form>
        {filter.user && (
          <Badge variant="secondary" className="h-8 gap-1 pr-1 pl-2.5 text-sm">
            {t('ofUser', { id: filter.user })}
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label={t('clearUser')}
              onClick={() => setFilter({ ...(filter.q ? { q: filter.q } : {}) })}
            >
              <X />
            </Button>
          </Badge>
        )}
      </div>
      <FormError message={error} />

      {players === null && !error && <Skeleton className="h-64 rounded-xl" />}
      {players && (
        <>
          <ul className="flex flex-col divide-y rounded-xl border bg-card md:hidden">
            {players.map((p) => (
              <li key={p.id} className="flex items-center gap-3 p-3">
                {figure(p)}
                <div className="flex min-w-0 flex-1 flex-col">
                  <span className="truncate font-medium">{p.name}</span>
                  <span className="truncate text-caption text-muted-foreground">{p.owner ? `${p.owner.nickname} · #${p.owner.id}` : '—'}</span>
                </div>
                {menu(p)}
              </li>
            ))}
            {players.length === 0 && <li className="p-6 text-center text-sm text-muted-foreground">{t('empty')}</li>}
          </ul>
          <div className="hidden overflow-hidden rounded-xl border bg-card md:block">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-14 pl-4">ID</TableHead>
                  <TableHead>{t('name')}</TableHead>
                  <TableHead>{t('owner')}</TableHead>
                  <TableHead>{t('skin')}</TableHead>
                  <TableHead>{t('cape')}</TableHead>
                  <TableHead>{t('updated')}</TableHead>
                  <TableHead className="w-12" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {players.map((p) => (
                  <TableRow key={p.id}>
                    <TableCell className="pl-4 font-mono text-caption text-muted-foreground">{p.id}</TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2.5">
                        {figure(p)}
                        <div className="flex min-w-0 flex-col">
                          <span className="font-medium">{p.name}</span>
                          <span className="font-mono text-[0.6875rem] text-faint-foreground">{dashedUUID(p.uuid)}</span>
                        </div>
                      </div>
                    </TableCell>
                    <TableCell>
                      {p.owner ? (
                        <div className="flex flex-col">
                          <span>{p.owner.nickname}</span>
                          <span className="text-caption text-muted-foreground">
                            #{p.owner.id} {p.owner.email}
                          </span>
                        </div>
                      ) : (
                        '—'
                      )}
                    </TableCell>
                    <TableCell className="text-caption">{worn(p.skin)}</TableCell>
                    <TableCell className="text-caption">{worn(p.cape)}</TableCell>
                    <TableCell className="text-caption">
                      <RelativeTime date={p.updatedAt} />
                    </TableCell>
                    <TableCell className="pr-3 text-right">{menu(p)}</TableCell>
                  </TableRow>
                ))}
                {players.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={7} className="py-10 text-center text-muted-foreground">
                      {t('empty')}
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </div>
        </>
      )}
      {next > 0 && (
        <Button variant="outline" className="self-center" loading={loading} onClick={() => void load(next)}>
          {t('more')}
        </Button>
      )}

      <Sheet open={editing !== null} onOpenChange={(open) => !open && setEditing(null)}>
        <SheetContent className="w-full overflow-y-auto sm:max-w-md">
          {editing && (
            <EditPlayer
              key={editing.id}
              p={editing}
              onUpdated={(updated) => changed(editing, updated)}
              onDone={(updated) => {
                changed(editing, updated)
                setEditing(null)
                toast.success(t('saved'))
              }}
            />
          )}
        </SheetContent>
      </Sheet>
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
    <Link href={`/skinlib/show/${w.id}`} className="hover:text-primary">
      {w.name} <span className="text-faint-foreground">#{w.id}</span>
    </Link>
  ) : (
    <span className="text-faint-foreground">—</span>
  )
}

function EditPlayer({
  p,
  onUpdated,
  onDone,
}: {
  p: AdminPlayerView
  /** Part of an edit was saved before another part failed: show it, keep editing. */
  onUpdated: (updated: AdminPlayerView) => void
  onDone: (updated: AdminPlayerView) => void
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
      onDone(updated)
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

  return (
    <form onSubmit={(e) => void save(e)} className="flex min-h-full flex-col">
      <SheetHeader>
        <SheetTitle>{t('editTitle', { name: p.name })}</SheetTitle>
        <SheetDescription>{t('editHint')}</SheetDescription>
      </SheetHeader>
      <div className="flex flex-1 flex-col gap-4 px-4">
        <FormError message={error} />
        {(
          [
            ['name', p.name, 'text'],
            ['owner', String(p.owner?.id ?? ''), 'number'],
            ['skin', p.skin ? String(p.skin.id) : '', 'number'],
            ['cape', p.cape ? String(p.cape.id) : '', 'number'],
          ] as const
        ).map(([key, value, type]) => (
          <Field key={key}>
            <FieldLabel htmlFor={`p-${key}`}>{t(`fields.${key}`)}</FieldLabel>
            <Input id={`p-${key}`} name={key} type={type} min={type === 'number' ? 0 : undefined} defaultValue={value} />
          </Field>
        ))}
      </div>
      <SheetFooter className="flex-row justify-end">
        <Button type="submit" loading={pending}>
          {t('save')}
        </Button>
      </SheetFooter>
    </form>
  )
}
