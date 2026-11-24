'use client'

import { addToCloset, deleteTexture, removeFromCloset, type ScoreRates, type TextureDetail, updateTexture } from '@skinloom/api-client'
import {
  Button,
  cn,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
  Field,
  FieldError,
  FieldLabel,
  Input,
  ResponsiveDialog,
  ResponsiveDialogClose,
  ResponsiveDialogContent,
  ResponsiveDialogDescription,
  ResponsiveDialogFooter,
  ResponsiveDialogHeader,
  ResponsiveDialogTitle,
  toast,
  useConfirm,
} from '@skinloom/ui'
import { Globe, Heart, HeartOff, Lock, MoreHorizontal, Pencil, Shapes, Trash2 } from 'lucide-react'
import { useRouter } from 'next/navigation'
import { useFormatter, useTranslations } from 'next-intl'
import { type FormEvent, useState } from 'react'

import { problemOf } from '@/lib/problem'
import { uploadCost } from '@/lib/skin'

/** Add to / remove from the viewer's closet: the page's primary action. */
export function FavoriteButton({ texture, rates, className }: { texture: TextureDetail; rates: ScoreRates; className?: string }) {
  const t = useTranslations('texture')
  const format = useFormatter()
  const router = useRouter()
  const [pending, setPending] = useState(false)

  async function toggle() {
    setPending(true)
    try {
      if (texture.inCloset) await removeFromCloset(texture.id)
      else await addToCloset({ textureId: texture.id })
      router.refresh()
    } catch (err) {
      const p = problemOf(err)
      if (p.status === 409 || p.status === 404) router.refresh() // already added or removed elsewhere
      else toast.error(p.status === 402 ? t('errors.score') : p.status === 422 && p.detail ? p.detail : t('errors.failed'))
    } finally {
      setPending(false)
    }
  }

  return (
    <div className={cn('flex flex-col gap-1.5', className)}>
      <Button size="lg" variant={texture.inCloset ? 'outline' : 'default'} loading={pending} onClick={() => void toggle()}>
        {texture.inCloset ? <HeartOff /> : <Heart />}
        {texture.inCloset ? t('unfavorite') : t('favorite')}
      </Button>
      {!texture.inCloset && rates.perClosetItem > 0 && (
        <p className="text-caption text-muted-foreground tabular-nums">{t('favoriteCost', { cost: format.number(rates.perClosetItem) })}</p>
      )}
    </div>
  )
}

const TYPES = ['steve', 'alex', 'cape'] as const

/**
 * The uploader's (or an administrator's) controls behind one "more" button: rename,
 * type, visibility, delete. Irreversible ones ask first (design.md §10.2).
 */
export function OwnerMenu({ texture, rates, charged }: { texture: TextureDetail; rates: ScoreRates; charged: boolean }) {
  const t = useTranslations('texture')
  const tc = useTranslations('common')
  const tf = useTranslations('fieldErrors')
  const router = useRouter()
  const confirm = useConfirm()
  const [pending, setPending] = useState(false)
  const [renaming, setRenaming] = useState(false)
  const [renameError, setRenameError] = useState<string | null>(null)
  // What switching visibility costs the uploader (negative: gives back).
  const switchCost = charged ? uploadCost(texture.size, !texture.public, rates) - uploadCost(texture.size, texture.public, rates) : 0
  const costNote = switchCost > 0 ? t('switchCost', { cost: switchCost }) : switchCost < 0 ? t('switchRefund', { cost: -switchCost }) : null

  function messageOf(err: unknown): string {
    const p = problemOf(err)
    if (p.unverified) return tc('verifyFirst')
    if (p.status === 402) return t('errors.score')
    if (p.status === 409) return t('errors.duplicate')
    if (p.fields.type) return t('errors.shape')
    if (p.fields.name) return t('errors.name')
    if (p.status === 422 && p.detail) return p.detail
    return t('errors.failed')
  }

  async function run(action: () => Promise<unknown>, done?: string): Promise<boolean> {
    setPending(true)
    try {
      await action()
      if (done) toast.success(done)
      router.refresh()
      return true
    } catch (err) {
      toast.error(messageOf(err))
      return false
    } finally {
      setPending(false)
    }
  }

  async function rename(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const name = String(new FormData(e.currentTarget).get('name') ?? '').trim()
    if (!name) {
      setRenameError(tf('required'))
      return
    }
    if (name === texture.name) {
      setRenaming(false)
      return
    }
    setPending(true)
    setRenameError(null)
    try {
      await updateTexture(texture.id, { name })
      setRenaming(false)
      router.refresh()
    } catch (err) {
      setRenameError(messageOf(err))
    } finally {
      setPending(false)
    }
  }

  async function toggleVisibility() {
    if (texture.public) {
      const ok = await confirm({
        title: t('privateTitle', { name: texture.name }),
        description: [t('privateConfirm'), costNote].filter(Boolean).join(' '),
        confirmLabel: t('makePrivate'),
        destructive: false,
      })
      if (!ok) return
    } else if (costNote) {
      const ok = await confirm({ title: t('publicTitle', { name: texture.name }), description: costNote, confirmLabel: t('makePublic'), destructive: false })
      if (!ok) return
    }
    await run(() => updateTexture(texture.id, { public: !texture.public }), texture.public ? t('madePrivate') : t('madePublic'))
  }

  async function remove() {
    const ok = await confirm({ title: t('deleteTitle', { name: texture.name }), description: t('deleteConfirm'), confirmLabel: t('delete') })
    if (!ok) return
    setPending(true)
    try {
      await deleteTexture(texture.id)
      toast.success(t('deleted', { name: texture.name }))
      router.replace('/skinlib')
      router.refresh()
    } catch (err) {
      toast.error(messageOf(err))
      setPending(false)
    }
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger render={<Button variant="outline" size="icon" aria-label={t('manage')} title={t('manage')} disabled={pending} />}>
          <MoreHorizontal />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-48">
          <DropdownMenuItem
            onClick={() => {
              setRenameError(null)
              setRenaming(true)
            }}
          >
            <Pencil />
            {t('rename')}
          </DropdownMenuItem>
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <Shapes />
              {t('changeType')}
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent>
              <DropdownMenuRadioGroup
                value={texture.type}
                onValueChange={(v) => {
                  if (v !== texture.type) void run(() => updateTexture(texture.id, { type: v as TextureDetail['type'] }))
                }}
              >
                {TYPES.map((v) => (
                  <DropdownMenuRadioItem key={v} value={v}>
                    {t(`types.${v}`)}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuSubContent>
          </DropdownMenuSub>
          <DropdownMenuItem onClick={() => void toggleVisibility()}>
            {texture.public ? <Lock /> : <Globe />}
            {texture.public ? t('makePrivate') : t('makePublic')}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem variant="destructive" onClick={() => void remove()}>
            <Trash2 />
            {t('delete')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <ResponsiveDialog open={renaming} onOpenChange={setRenaming}>
        <ResponsiveDialogContent className="md:max-w-sm">
          <form onSubmit={(e) => void rename(e)} className="flex flex-col gap-5" noValidate>
            <ResponsiveDialogHeader>
              <ResponsiveDialogTitle>{t('renameTitle')}</ResponsiveDialogTitle>
              <ResponsiveDialogDescription>{t('renameBody')}</ResponsiveDialogDescription>
            </ResponsiveDialogHeader>
            <Field data-invalid={renameError ? true : undefined}>
              <FieldLabel htmlFor="texture-name">{t('name')}</FieldLabel>
              <Input id="texture-name" name="name" defaultValue={texture.name} maxLength={50} required autoFocus aria-invalid={renameError ? true : undefined} />
              {renameError && <FieldError>{renameError}</FieldError>}
            </Field>
            <ResponsiveDialogFooter>
              <ResponsiveDialogClose render={<Button type="button" variant="ghost" />}>{tc('cancel')}</ResponsiveDialogClose>
              <Button type="submit" loading={pending}>
                {tc('save')}
              </Button>
            </ResponsiveDialogFooter>
          </form>
        </ResponsiveDialogContent>
      </ResponsiveDialog>
    </>
  )
}
