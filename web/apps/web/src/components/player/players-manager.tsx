'use client'

import {
  type ClosetPage,
  createPlayer,
  deletePlayer,
  listPlayers,
  type PlayerCreated,
  type PlayerDeleted,
  type PlayerRules,
  type PlayerView,
  renamePlayer,
  wearTextures,
} from '@skinloom/api-client'
import { getListPlayersQueryKey, useListCloset, useListPlayers } from '@skinloom/api-client/query'
import {
  Button,
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
  Input,
  PageHeader,
  PlayerCard,
  Skeleton,
  TexturePicker,
  toast,
  useConfirm,
} from '@skinloom/ui'
import { useQueryClient } from '@tanstack/react-query'
import { Copy, FileJson, MoreHorizontal, Pencil, Plus, Shirt, Trash2, Users, X } from 'lucide-react'
import { useTranslations } from 'next-intl'
import { type FormEvent, useState } from 'react'

import { problemOf } from '@/lib/problem'
import { dashedUUID } from '@/lib/uuid'

type Editing = { kind: 'create' } | { kind: 'rename'; player: PlayerView } | null
type Picking = { player: PlayerView; kind: 'skin' | 'cape' } | null

/** The signed-in user's players: create, rename, dress, delete (design.md §9.4). */
export function PlayersManager({ rules, initialScore }: { rules: PlayerRules; initialScore: number }) {
  const t = useTranslations('player')
  const tc = useTranslations('common')
  const confirm = useConfirm()
  const qc = useQueryClient()
  const players = useListPlayers({ query: { select: (r) => r.data as PlayerView[] } })
  const [score, setScore] = useState(initialScore)
  const [editing, setEditing] = useState<Editing>(null)
  const [picking, setPicking] = useState<Picking>(null)
  // The closet, for the picker: loaded when it is first opened.
  const closet = useListCloset({ limit: 100 }, { query: { enabled: picking !== null, select: (r) => (r.data as ClosetPage).items } })

  const list = players.data
  const set = (next: (l: PlayerView[]) => PlayerView[]) =>
    qc.setQueryData<Awaited<ReturnType<typeof listPlayers>>>(getListPlayersQueryKey(), (old) => (old ? ({ ...old, data: next(old.data as PlayerView[]) } as typeof old) : old))
  const replace = (p: PlayerView) => set((l) => l.map((x) => (x.id === p.id ? p : x)))

  async function wear(player: PlayerView, body: { skin?: number; cape?: number }) {
    try {
      replace((await wearTextures(player.id, body)).data as PlayerView)
      return true
    } catch (err) {
      const p = problemOf(err)
      toast.error(p.unverified ? tc('verifyFirst') : p.status === 422 && p.detail ? p.detail : t('errors.failed'))
      return false
    }
  }

  async function remove(player: PlayerView) {
    const ok = await confirm({
      title: t('deleteTitle', { name: player.name }),
      description: rules.refund && rules.cost > 0 ? `${t('deleteBody')} ${t('deleteRefund')}` : t('deleteBody'),
      confirmLabel: t('delete'),
    })
    if (!ok) return
    try {
      const refunded = ((await deletePlayer(player.id)).data as PlayerDeleted).refunded
      set((l) => l.filter((x) => x.id !== player.id))
      setScore((s) => s + refunded)
      toast.success(t('deleted', { name: player.name }))
    } catch (err) {
      const p = problemOf(err)
      toast.error(p.unverified ? tc('verifyFirst') : p.status === 422 && p.detail ? p.detail : t('errors.failed'))
    }
  }

  async function copyUUID(player: PlayerView) {
    try {
      await navigator.clipboard.writeText(dashedUUID(player.uuid))
      toast.success(t('copied'))
    } catch {
      toast.error(t('errors.failed'))
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={t('title')}
        description={t('intro')}
        actions={
          <Button onClick={() => setEditing({ kind: 'create' })}>
            <Plus />
            {t('create')}
          </Button>
        }
      />

      {players.isError && <p className="text-sm text-destructive">{t('loadFailed')}</p>}
      {players.isPending && (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {[0, 1].map((i) => (
            <Skeleton key={i} className="h-80 rounded-xl" />
          ))}
        </div>
      )}
      {list?.length === 0 && (
        <Empty className="border border-dashed py-16">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Users />
            </EmptyMedia>
            <EmptyTitle>{t('none')}</EmptyTitle>
            <EmptyDescription>{t('noneBody')}</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button onClick={() => setEditing({ kind: 'create' })}>
              <Plus />
              {t('create')}
            </Button>
          </EmptyContent>
        </Empty>
      )}
      {list && list.length > 0 && (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {list.map((p) => (
            <PlayerCard
              key={p.id}
              name={p.name}
              uuid={dashedUUID(p.uuid)}
              skin={p.skin && p.skin.type !== 'cape' ? { id: p.skin.id, type: p.skin.type } : null}
              details={[
                { label: t('skin'), value: p.skin?.name ?? '—' },
                { label: t('cape'), value: p.cape?.name ?? '—' },
              ]}
              actions={
                <>
                  <Button variant="outline" size="sm" onClick={() => setPicking({ player: p, kind: 'skin' })}>
                    <Shirt />
                    {t('chooseSkin')}
                  </Button>
                  <span className="flex-1" />
                  <DropdownMenu>
                    <DropdownMenuTrigger render={<Button variant="ghost" size="icon-sm" aria-label={t('more', { name: p.name })} title={t('more', { name: p.name })} />}>
                      <MoreHorizontal />
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="w-48">
                      <DropdownMenuItem onClick={() => setPicking({ player: p, kind: 'cape' })}>
                        <Shirt />
                        {t('chooseCape')}
                      </DropdownMenuItem>
                      {p.skin && (
                        <DropdownMenuItem onClick={() => void wear(p, { skin: 0 })}>
                          <X />
                          {t('removeSkin')}
                        </DropdownMenuItem>
                      )}
                      {p.cape && (
                        <DropdownMenuItem onClick={() => void wear(p, { cape: 0 })}>
                          <X />
                          {t('removeCape')}
                        </DropdownMenuItem>
                      )}
                      <DropdownMenuSeparator />
                      <DropdownMenuItem onClick={() => setEditing({ kind: 'rename', player: p })}>
                        <Pencil />
                        {t('rename')}
                      </DropdownMenuItem>
                      <DropdownMenuItem onClick={() => void copyUUID(p)}>
                        <Copy />
                        {t('copyUUID')}
                      </DropdownMenuItem>
                      <DropdownMenuItem render={<a href={`/${encodeURIComponent(p.name)}.json`} target="_blank" rel="noreferrer" />}>
                        <FileJson />
                        {t('profileLink')}
                      </DropdownMenuItem>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem variant="destructive" onClick={() => void remove(p)}>
                        <Trash2 />
                        {t('delete')}
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </>
              }
            />
          ))}
        </div>
      )}

      <NameDialog
        editing={editing}
        rules={rules}
        score={score}
        onClose={() => setEditing(null)}
        onCreated={(c) => {
          set((l) => [...l, c.player])
          setScore(c.score)
          toast.success(t('created', { name: c.player.name }))
        }}
        onRenamed={replace}
      />

      <TexturePicker
        open={picking !== null}
        onOpenChange={(open) => !open && setPicking(null)}
        title={picking ? t(picking.kind === 'skin' ? 'pickSkinTitle' : 'pickCapeTitle', { name: picking.player.name }) : ''}
        description={closet.data?.length === 0 ? t('closetEmpty') : closet.isPending ? tc('loading') : undefined}
        kind={picking?.kind}
        items={(closet.data ?? []).map((it) => ({ id: it.texture.id, name: it.name, type: it.texture.type }))}
        selectedId={picking ? (picking.kind === 'skin' ? picking.player.skin?.id : picking.player.cape?.id) : null}
        onPick={async (x) => {
          if (!picking) return
          if (await wear(picking.player, picking.kind === 'skin' ? { skin: x.id } : { cape: x.id })) setPicking(null)
        }}
      />
    </div>
  )
}

/** Create a player, or rename one: a name with the site's rule. */
function NameDialog({
  editing,
  rules,
  score,
  onClose,
  onCreated,
  onRenamed,
}: {
  editing: Editing
  rules: PlayerRules
  score: number
  onClose: () => void
  onCreated: (c: PlayerCreated) => void
  onRenamed: (p: PlayerView) => void
}) {
  const t = useTranslations('player')
  const tc = useTranslations('common')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const hint = t(`rules.${rules.nameRule}`, { min: rules.minLength, max: rules.maxLength })
  const renaming = editing?.kind === 'rename' ? editing.player : null

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const name = String(new FormData(e.currentTarget).get('name') ?? '').trim()
    if (renaming && name === renaming.name) return onClose()
    setPending(true)
    setError(null)
    try {
      if (renaming) onRenamed((await renamePlayer(renaming.id, { name })).data as PlayerView)
      else onCreated((await createPlayer({ name })).data as PlayerCreated)
      onClose()
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
                : p.status === 422 && p.detail
                  ? p.detail
                  : t('errors.failed'),
      )
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog
      open={editing !== null}
      onOpenChange={(open) => {
        if (!open) onClose()
        setError(null)
      }}
    >
      <DialogContent className="sm:max-w-sm">
        <form onSubmit={(e) => void submit(e)} className="flex flex-col gap-5">
          <DialogHeader>
            <DialogTitle>{renaming ? t('renameTitle', { name: renaming.name }) : t('createTitle')}</DialogTitle>
            <DialogDescription>{renaming ? t('renameBody') : t('createBody')}</DialogDescription>
          </DialogHeader>
          <Field data-invalid={error ? true : undefined}>
            <FieldLabel htmlFor="player-name">{t('name')}</FieldLabel>
            <Input
              key={renaming?.id ?? 'new'}
              id="player-name"
              name="name"
              defaultValue={renaming?.name}
              required
              maxLength={rules.maxLength}
              autoFocus
              autoComplete="off"
              aria-invalid={error ? true : undefined}
            />
            {error ? <FieldError>{error}</FieldError> : <FieldDescription>{hint}</FieldDescription>}
          </Field>
          {!renaming && rules.cost > 0 && (
            <p className="text-caption text-muted-foreground">
              {t('cost', { cost: rules.cost, score })}
              {rules.refund ? ` ${t('refundHint')}` : ''}
            </p>
          )}
          <DialogFooter>
            <DialogClose render={<Button type="button" variant="ghost" />}>{tc('cancel')}</DialogClose>
            <Button type="submit" loading={pending}>
              {renaming ? tc('save') : t('create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
