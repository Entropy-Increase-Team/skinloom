'use client'

import { type TextureRules, uploadTextureFile } from '@skinloom/api-client'
import { Button, Label } from '@skinloom/ui'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { type ChangeEvent, type FormEvent, useEffect, useState } from 'react'
import { inferModelType, loadSkinToCanvas } from 'skinview-utils'

import { Field, FormError } from '@/components/auth/field'
import { problemOf } from '@/lib/problem'
import { looksLikeCape, uploadCost } from '@/lib/skin'

import { TexturePreview } from './texture-preview'

type TextureType = 'steve' | 'alex' | 'cape'

/** Reads a picked image: its size and a guess of what it is. */
async function inspect(file: File): Promise<{ url: string; width: number; height: number; guess: TextureType }> {
  const url = URL.createObjectURL(file)
  const img = new Image()
  img.src = url
  try {
    await img.decode()
  } catch (err) {
    URL.revokeObjectURL(url)
    throw err
  }
  const { naturalWidth: w, naturalHeight: h } = img
  const canvas = document.createElement('canvas')
  canvas.width = w
  canvas.height = h
  const ctx = canvas.getContext('2d', { willReadFrequently: true })
  ctx?.drawImage(img, 0, 0)
  const data = ctx?.getImageData(0, 0, w, h).data
  let guess: TextureType = 'steve'
  if (data && looksLikeCape(w, h, (x, y) => data[(y * w + x) * 4 + 3])) {
    guess = 'cape'
  } else if (w === h) {
    const skin = document.createElement('canvas')
    loadSkinToCanvas(skin, img)
    if (inferModelType(skin) === 'slim') guess = 'alex'
  }
  return { url, width: w, height: h, guess }
}

export function UploadForm({ rules, score }: { rules: TextureRules; score: number }) {
  const t = useTranslations('upload')
  const tt = useTranslations('texture')
  const router = useRouter()
  const [file, setFile] = useState<File | null>(null)
  const [picked, setPicked] = useState<{ url: string; width: number; height: number } | null>(null)
  const [name, setName] = useState('')
  const [type, setType] = useState<TextureType>('steve')
  const [isPublic, setPublic] = useState(true)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [fields, setFields] = useState<Record<string, string>>({})
  const [duplicate, setDuplicate] = useState<number | null>(null)

  useEffect(() => {
    if (!picked) return
    return () => URL.revokeObjectURL(picked.url)
  }, [picked])

  async function onPick(e: ChangeEvent<HTMLInputElement>) {
    const f = e.target.files?.[0] ?? null
    setFields({})
    setError(null)
    setDuplicate(null)
    setFile(null)
    setPicked(null)
    if (!f) return
    // Some systems give no type for .png files; the server checks the content anyway.
    if (f.type !== 'image/png' && !(f.type === '' && /\.png$/i.test(f.name))) {
      setFields({ file: t('errors.notPng') })
      return
    }
    if (f.size > rules.maxSizeKb * 1024) {
      setFields({ file: t('errors.tooLarge', { kb: rules.maxSizeKb }) })
      return
    }
    try {
      const info = await inspect(f)
      setFile(f)
      setPicked(info)
      setType(info.guess)
      if (!name) setName(f.name.replace(/\.png$/i, '').slice(0, 50))
    } catch {
      setFields({ file: t('errors.unreadable') })
    }
  }

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!file) {
      setFields({ file: t('errors.noFile') })
      return
    }
    setPending(true)
    setError(null)
    setFields({})
    try {
      const res = await uploadTextureFile({ file, name, type, public: isPublic })
      if (res.duplicate) {
        setDuplicate(res.texture.id)
        setPending(false)
        return
      }
      router.push(`/skinlib/show/${res.texture.id}`)
      router.refresh()
    } catch (err) {
      const p = problemOf(err)
      setFields(p.fields)
      if (p.status === 402) setError(t('errors.score'))
      else if (Object.keys(p.fields).length === 0) setError(t('errors.failed'))
      setPending(false)
    }
  }

  const cost = file ? uploadCost(file.size, isPublic, rules.rates) : 0
  const types: TextureType[] = ['steve', 'alex', 'cape']

  return (
    <form onSubmit={onSubmit} className="grid gap-6 md:grid-cols-[12rem_1fr]" noValidate>
      <div className="flex h-64 items-center justify-center rounded-lg border bg-muted/40">
        {picked ? (
          <TexturePreview key={`${picked.url}-${type}`} src={picked.url} type={type} height={224} />
        ) : (
          <span className="px-4 text-center text-sm text-muted-foreground">{t('noPreview')}</span>
        )}
      </div>
      <div className="flex flex-col gap-4">
        <FormError message={error} />
        {duplicate !== null && (
          <p role="status" className="rounded-md border bg-muted/40 px-3 py-2 text-sm">
            {t('duplicate')}{' '}
            <Link href={`/skinlib/show/${duplicate}`} className="font-medium underline underline-offset-4">
              {t('viewExisting')}
            </Link>
          </p>
        )}
        <div className="flex flex-col gap-2">
          <Label htmlFor="file">{t('file')}</Label>
          <input
            id="file"
            type="file"
            accept="image/png"
            onChange={(e) => void onPick(e)}
            aria-invalid={fields.file ? true : undefined}
            className="text-sm file:mr-3 file:rounded-md file:border file:bg-transparent file:px-3 file:py-1.5 file:text-sm"
          />
          {fields.file ? (
            <p className="text-xs text-destructive">{fields.file}</p>
          ) : (
            <p className="text-xs text-muted-foreground">
              {picked ? t('picked', { w: picked.width, h: picked.height }) : t('fileHint', { kb: rules.maxSizeKb, width: rules.maxWidth })}
            </p>
          )}
        </div>
        <Field id="name" label={t('name')} value={name} onChange={(e) => setName(e.target.value)} maxLength={50} required error={fields.name} />
        <fieldset className="flex flex-col gap-2">
          <legend className="mb-2 text-sm font-medium">{t('type')}</legend>
          <div className="flex flex-wrap gap-4">
            {types.map((v) => (
              <label key={v} className="flex items-center gap-2 text-sm">
                <input type="radio" name="type" value={v} checked={type === v} onChange={() => setType(v)} className="size-4" />
                {tt(`types.${v}`)}
              </label>
            ))}
          </div>
          {fields.type && <p className="text-xs text-destructive">{fields.type}</p>}
          <p className="text-xs text-muted-foreground">{t('typeHint')}</p>
        </fieldset>
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={isPublic} onChange={(e) => setPublic(e.target.checked)} className="size-4" />
          {t('public')}
        </label>
        <p className="text-sm text-muted-foreground">
          {file ? t('cost', { cost, score }) : t('rates', { pub: rules.rates.perStorage, priv: rules.rates.privatePerStorage })}
        </p>
        {rules.contentPolicy && (
          <div className="whitespace-pre-line rounded-md border bg-muted/40 px-3 py-2 text-xs text-muted-foreground">{rules.contentPolicy}</div>
        )}
        <Button type="submit" className="self-start" disabled={pending || !file}>
          {pending ? t('uploading') : t('submit')}
        </Button>
      </div>
    </form>
  )
}
