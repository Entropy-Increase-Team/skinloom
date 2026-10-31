'use client'

import { addToCloset, deleteTexture, removeFromCloset, type ScoreRates, type TextureDetail, updateTexture } from '@skinloom/api-client'
import { Button, Input } from '@skinloom/ui'
import { Heart, HeartOff, Trash2 } from 'lucide-react'
import { useRouter } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { type FormEvent, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { problemOf } from '@/lib/problem'
import { uploadCost } from '@/lib/skin'

/** Add to / remove from the viewer's closet. */
export function FavoriteButton({ texture, rates }: { texture: TextureDetail; rates: ScoreRates }) {
  const t = useTranslations('texture')
  const router = useRouter()
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function toggle() {
    setPending(true)
    setError(null)
    try {
      if (texture.inCloset) await removeFromCloset(texture.id)
      else await addToCloset({ textureId: texture.id })
      router.refresh()
    } catch (err) {
      const status = problemOf(err).status
      if (status === 409 || status === 404) router.refresh() // already added or removed elsewhere
      else setError(status === 402 ? t('errors.score') : t('errors.failed'))
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="flex flex-col gap-1">
      <Button variant={texture.inCloset ? 'outline' : 'default'} disabled={pending} onClick={() => void toggle()}>
        {texture.inCloset ? <HeartOff /> : <Heart />}
        {texture.inCloset ? t('unfavorite') : t('favorite')}
      </Button>
      {!texture.inCloset && rates.perClosetItem > 0 && <p className="text-xs text-muted-foreground">{t('favoriteCost', { cost: rates.perClosetItem })}</p>}
      <FormError message={error} />
    </div>
  )
}

/** The uploader's (or an administrator's) controls: rename, type, visibility, delete. */
export function OwnerControls({ texture, rates, charged }: { texture: TextureDetail; rates: ScoreRates; charged: boolean }) {
  const t = useTranslations('texture')
  const tc = useTranslations('common')
  const router = useRouter()
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // What switching visibility costs the uploader (negative: gives back).
  const switchCost = charged ? uploadCost(texture.size, !texture.public, rates) - uploadCost(texture.size, texture.public, rates) : 0

  async function run(action: () => Promise<unknown>, after?: () => void) {
    setPending(true)
    setError(null)
    try {
      await action()
      if (after) after()
      else router.refresh()
    } catch (err) {
      const p = problemOf(err)
      setError(
        p.unverified
          ? tc('verifyFirst')
          : p.status === 402
            ? t('errors.score')
            : p.status === 409
              ? t('errors.duplicate')
              : p.fields.type
                ? t('errors.shape')
                : p.fields.name
                  ? t('errors.name')
                  : t('errors.failed'),
      )
    } finally {
      setPending(false)
    }
  }

  function onRename(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const name = String(new FormData(e.currentTarget).get('name') ?? '').trim()
    if (name && name !== texture.name) void run(() => updateTexture(texture.id, { name }))
  }

  return (
    <div className="flex flex-col gap-3 border-t pt-4">
      <p className="text-sm font-medium">{t('manage')}</p>
      <form onSubmit={onRename} className="flex gap-2">
        <Input name="name" defaultValue={texture.name} maxLength={50} aria-label={t('rename')} />
        <Button type="submit" variant="outline" disabled={pending}>
          {t('rename')}
        </Button>
      </form>
      <select
        value={texture.type}
        disabled={pending}
        aria-label={t('changeType')}
        onChange={(e) => void run(() => updateTexture(texture.id, { type: e.target.value as TextureDetail['type'] }))}
        className="h-9 rounded-md border border-input bg-transparent px-2 text-sm"
      >
        {(['steve', 'alex', 'cape'] as const).map((v) => (
          <option key={v} value={v}>
            {t(`types.${v}`)}
          </option>
        ))}
      </select>
      <Button
        variant="outline"
        disabled={pending}
        onClick={() => {
          if (texture.public && !window.confirm(t('privateConfirm'))) return
          void run(() => updateTexture(texture.id, { public: !texture.public }))
        }}
      >
        {texture.public ? t('makePrivate') : t('makePublic')}
      </Button>
      {switchCost !== 0 && (
        <p className="text-xs text-muted-foreground">{switchCost > 0 ? t('switchCost', { cost: switchCost }) : t('switchRefund', { cost: -switchCost })}</p>
      )}
      <Button
        variant="destructive"
        disabled={pending}
        onClick={() => {
          if (!window.confirm(t('deleteConfirm', { name: texture.name }))) return
          void run(
            () => deleteTexture(texture.id),
            () => {
              router.replace('/skinlib')
              router.refresh()
            },
          )
        }}
      >
        <Trash2 />
        {t('delete')}
      </Button>
      <FormError message={error} />
    </div>
  )
}
