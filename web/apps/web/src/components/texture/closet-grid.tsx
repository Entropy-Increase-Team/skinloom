'use client'

import { type ClosetItemView, type ClosetPage, listCloset, removeFromCloset, renameClosetItem, setAvatar, type Unfavorited } from '@skinloom/api-client'
import {
  Alert,
  AlertAction,
  AlertDescription,
  Button,
  ButtonLink,
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
  FieldError,
  FieldLabel,
  Input,
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  ResponsiveDialog,
  ResponsiveDialogClose,
  ResponsiveDialogContent,
  ResponsiveDialogDescription,
  ResponsiveDialogFooter,
  ResponsiveDialogHeader,
  ResponsiveDialogTitle,
  Skeleton,
  TextureCard,
  ToggleGroup,
  ToggleGroupItem,
  toast,
  useConfirm,
} from '@skinloom/ui'
import { CircleAlert, MoreHorizontal, Pencil, Search, Shirt, Trash2, UserRound } from 'lucide-react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { type FormEvent, useCallback, useEffect, useRef, useState } from 'react'

import { useBootstrap } from '@/components/providers'
import { Slot } from '@/components/slot'
import { ThemeOverride } from '@/components/theme-override'
import { problemOf } from '@/lib/problem'
import { layoutOf, textureGridClass } from '@/lib/theme'

type Category = 'skin' | 'cape'

/** The signed-in user's closet: skins or capes, searchable by item name. */
export function ClosetGrid() {
  const t = useTranslations('closet')
  const tt = useTranslations('texture')
  const tc = useTranslations('common')
  const boot = useBootstrap()
  const router = useRouter()
  const confirm = useConfirm()
  const density = layoutOf(boot.theme.layout, 'skinlib')
  const [category, setCategory] = useState<Category>('skin')
  const [searchInput, setSearchInput] = useState('')
  const [query, setQuery] = useState('')
  const [items, setItems] = useState<ClosetItemView[] | null>(null)
  const [next, setNext] = useState(0)
  const [loading, setLoading] = useState(false)
  const [failed, setFailed] = useState(false)
  const [renaming, setRenaming] = useState<ClosetItemView | null>(null)
  // Answers can arrive out of order when the tab or search changes quickly: only the
  // latest request may update the list.
  const latest = useRef(0)

  const load = useCallback(
    async (before?: number) => {
      const seq = ++latest.current
      if (!before) {
        setItems(null)
        setNext(0)
      }
      setLoading(true)
      setFailed(false)
      try {
        const page = (await listCloset({ category, ...(query ? { q: query } : {}), ...(before ? { before } : {}) })).data as ClosetPage
        if (seq !== latest.current) return
        setItems((list) => (before && list ? [...list, ...page.items] : page.items))
        setNext(page.next)
      } catch {
        if (seq === latest.current) setFailed(true)
      } finally {
        if (seq === latest.current) setLoading(false)
      }
    },
    [category, query],
  )
  useEffect(() => {
    void load()
  }, [load])

  const update = (item: ClosetItemView, updated: ClosetItemView | null) =>
    setItems((list) => list && (updated ? list.map((x) => (x.id === item.id ? updated : x)) : list.filter((x) => x.id !== item.id)))

  async function remove(item: ClosetItemView) {
    const ok = await confirm({ title: t('removeTitle', { name: item.name }), description: t('removeBody'), confirmLabel: t('remove') })
    if (!ok) return
    try {
      const { refunded } = (await removeFromCloset(item.texture.id)).data as Unfavorited
      update(item, null)
      toast.success(refunded > 0 ? t('removedRefund', { score: refunded }) : t('removed'))
    } catch (err) {
      const p = problemOf(err)
      toast.error(p.status === 422 && p.detail ? p.detail : t('changeFailed'))
    }
  }

  async function chooseAvatar(item: ClosetItemView) {
    try {
      await setAvatar({ textureId: item.texture.id })
      toast.success(t('avatarSet', { name: item.name }))
      router.refresh() // the header shows the new avatar
    } catch (err) {
      const p = problemOf(err)
      toast.error(p.status === 422 && p.detail ? p.detail : t('changeFailed'))
    }
  }

  function onSearch(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    setQuery(searchInput.trim())
  }

  function clearSearch() {
    setSearchInput('')
    setQuery('')
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center gap-3">
        <ToggleGroup
          value={[category]}
          onValueChange={(v) => {
            const nextCat = v[0] as Category | undefined
            if (nextCat) setCategory(nextCat)
          }}
          spacing={1}
          aria-label={t('category')}
          className="rounded-lg bg-muted p-0.5"
        >
          {(['skin', 'cape'] as const).map((c) => (
            <ToggleGroupItem
              key={c}
              value={c}
              size="sm"
              className="rounded-md px-3 text-sm text-muted-foreground hover:bg-transparent hover:text-foreground aria-pressed:bg-card aria-pressed:font-medium aria-pressed:text-foreground aria-pressed:shadow-xs aria-pressed:ring-1 aria-pressed:ring-border"
            >
              {t(c === 'skin' ? 'skins' : 'capes')}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        <form onSubmit={onSearch} role="search" className="min-w-44 flex-1 sm:max-w-60 sm:flex-none">
          <InputGroup>
            <InputGroupAddon>
              <Search aria-hidden />
            </InputGroupAddon>
            <InputGroupInput
              type="search"
              name="q"
              value={searchInput}
              onChange={(e) => {
                setSearchInput(e.target.value)
                if (e.target.value === '' && query !== '') setQuery('')
              }}
              placeholder={t('search')}
              aria-label={t('search')}
              maxLength={50}
            />
          </InputGroup>
        </form>
      </div>

      {failed && (
        <Alert variant="destructive" role="alert">
          <CircleAlert />
          <AlertDescription>{t('loadFailed')}</AlertDescription>
          <AlertAction>
            <Button type="button" variant="outline" size="sm" onClick={() => void load(items && items.length > 0 ? next : undefined)}>
              {tc('retry')}
            </Button>
          </AlertAction>
        </Alert>
      )}
      {items === null && !failed && (
        <div className={textureGridClass(density)}>
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-60 rounded-xl" />
          ))}
        </div>
      )}
      {items && items.length === 0 && !loading && (
        <Empty className="border border-dashed py-16">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Shirt />
            </EmptyMedia>
            <EmptyTitle>{query ? t('noMatch') : t('empty')}</EmptyTitle>
            {!query && <EmptyDescription>{t('emptyBody')}</EmptyDescription>}
          </EmptyHeader>
          <EmptyContent className="flex-row justify-center">
            {query ? (
              <Button type="button" variant="outline" size="sm" onClick={clearSearch}>
                {t('clearSearch')}
              </Button>
            ) : (
              <>
                <ButtonLink render={<Link href="/skinlib" />}>{t('browse')}</ButtonLink>
                <ButtonLink variant="outline" render={<Link href="/skinlib/upload" />}>
                  {t('upload')}
                </ButtonLink>
              </>
            )}
          </EmptyContent>
        </Empty>
      )}
      {items && items.length > 0 && (
        <div className={textureGridClass(density)}>
          {items.map((it) => (
            // The card is a link; the menu sits over it as a sibling (links cannot hold buttons).
            <div key={it.id} className="relative">
              <ThemeOverride
                name="texture.card"
                props={{ texture: it.texture, name: it.name, density, href: `/skinlib/show/${it.texture.id}` }}
                fallback={
                  <TextureCard
                    linkAs={Link}
                    href={`/skinlib/show/${it.texture.id}`}
                    density={density}
                    texture={{ ...it.texture, name: it.name, uploader: null }}
                    meta={<span className="truncate">{tt(`types.${it.texture.type}`)}</span>}
                    privateLabel={tt('private')}
                    likesLabel={tt('likes')}
                  />
                }
              />
              <div className="mt-1.5 flex items-center gap-1 empty:hidden">
                <Slot name="user.closet.item.actions" props={{ textureId: it.texture.id, name: it.name, type: it.texture.type }} />
              </div>
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button
                      variant="outline"
                      size="icon-sm"
                      className="absolute top-2.5 right-2.5 bg-card/90 shadow-xs backdrop-blur-sm pointer-coarse:size-10"
                      aria-label={t('itemMenu', { name: it.name })}
                      title={t('itemMenu', { name: it.name })}
                    />
                  }
                >
                  <MoreHorizontal />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-44">
                  {it.texture.type !== 'cape' && (
                    <DropdownMenuItem onClick={() => void chooseAvatar(it)}>
                      <UserRound />
                      {t('useAsAvatar')}
                    </DropdownMenuItem>
                  )}
                  <DropdownMenuItem onClick={() => setRenaming(it)}>
                    <Pencil />
                    {t('rename')}
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem variant="destructive" onClick={() => void remove(it)}>
                    <Trash2 />
                    {t('remove')}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          ))}
        </div>
      )}
      {next > 0 && (
        <Button variant="outline" className="self-center" loading={loading} onClick={() => void load(next)}>
          {t('more')}
        </Button>
      )}

      <ResponsiveDialog open={renaming !== null} onOpenChange={(open) => !open && setRenaming(null)}>
        <ResponsiveDialogContent className="md:max-w-sm">
          {renaming && (
            <RenameForm
              item={renaming}
              onDone={(updated) => {
                update(renaming, updated)
                setRenaming(null)
              }}
              cancel={tc('cancel')}
              save={tc('save')}
            />
          )}
        </ResponsiveDialogContent>
      </ResponsiveDialog>
    </div>
  )
}

/** The name of a closet item is the user's own: it does not rename the texture. */
function RenameForm({ item, onDone, cancel, save }: { item: ClosetItemView; onDone: (updated: ClosetItemView) => void; cancel: string; save: string }) {
  const t = useTranslations('closet')
  const tf = useTranslations('fieldErrors')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const name = String(new FormData(e.currentTarget).get('name') ?? '').trim()
    if (!name) {
      setError(tf('required'))
      return
    }
    if (name === item.name) return onDone(item)
    setPending(true)
    setError(null)
    try {
      onDone((await renameClosetItem(item.texture.id, { name })).data as ClosetItemView)
    } catch (err) {
      const p = problemOf(err)
      setError(p.fields.name ?? (p.status === 422 && p.detail ? p.detail : t('changeFailed')))
      setPending(false)
    }
  }

  return (
    <form onSubmit={(e) => void submit(e)} className="flex flex-col gap-5" noValidate>
      <ResponsiveDialogHeader>
        <ResponsiveDialogTitle>{t('renameTitle')}</ResponsiveDialogTitle>
        <ResponsiveDialogDescription>{t('renameBody')}</ResponsiveDialogDescription>
      </ResponsiveDialogHeader>
      <Field data-invalid={error ? true : undefined}>
        <FieldLabel htmlFor="closet-name">{t('name')}</FieldLabel>
        <Input id="closet-name" name="name" defaultValue={item.name} required maxLength={50} autoFocus autoComplete="off" aria-invalid={error ? true : undefined} />
        {error && <FieldError>{error}</FieldError>}
      </Field>
      <ResponsiveDialogFooter>
        <ResponsiveDialogClose render={<Button type="button" variant="ghost" />}>{cancel}</ResponsiveDialogClose>
        <Button type="submit" loading={pending}>
          {save}
        </Button>
      </ResponsiveDialogFooter>
    </form>
  )
}
