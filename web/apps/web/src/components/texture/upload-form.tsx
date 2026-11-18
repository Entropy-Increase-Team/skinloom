'use client'

import { type TextureRules, uploadTextureFile } from '@skinloom/api-client'
import {
  Alert,
  AlertDescription,
  Button,
  Dropzone,
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldLabel,
  FieldLegend,
  FieldSet,
  Input,
  PixelBackdrop,
  RadioGroup,
  RadioGroupItem,
  SkinViewer,
  Switch,
} from '@skinloom/ui'
import { Coins, RefreshCw } from 'lucide-react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { type FormEvent, useEffect, useState } from 'react'
import { inferModelType, loadSkinToCanvas } from 'skinview-utils'

import { FormError } from '@/components/auth/field'
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

/**
 * Pick a PNG (drop or choose), name it, check the type guessed from the image, choose
 * public or private, and see what it costs before uploading. Large screens show the
 * picked skin in 3D beside the form; the drop zone shows it flat everywhere.
 */
export function UploadForm({ rules, score }: { rules: TextureRules; score: number }) {
  const t = useTranslations('upload')
  const tt = useTranslations('texture')
  const tc = useTranslations('common')
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

  async function onPick(f: File) {
    setFields({})
    setError(null)
    setDuplicate(null)
    setFile(null)
    setPicked(null)
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
      if (p.unverified) setError(tc('verifyFirst'))
      else if (p.status === 402) setError(t('errors.score'))
      else if (Object.keys(p.fields).length === 0) setError(p.status === 422 && p.detail ? p.detail : t('errors.failed'))
      setPending(false)
    }
  }

  const cost = file ? uploadCost(file.size, isPublic, rules.rates) : 0
  const types: TextureType[] = ['steve', 'alex', 'cape']

  return (
    <form onSubmit={onSubmit} className="grid grid-cols-1 gap-8 lg:grid-cols-[minmax(0,1fr)_24rem]" noValidate>
      <PixelBackdrop className="hidden h-[32rem] rounded-xl border lg:flex">
        {picked ? (
          <SkinViewer
            key={`${picked.url}-${type}`}
            skin={type === 'cape' ? null : picked.url}
            cape={type === 'cape' ? picked.url : null}
            model={type === 'alex' ? 'slim' : 'default'}
            label={t('previewLabel')}
          />
        ) : (
          <span className="rounded-md bg-card/90 px-3 py-1.5 text-sm text-muted-foreground">{t('noPreview')}</span>
        )}
      </PixelBackdrop>

      <div className="flex flex-col gap-6">
        <Dropzone
          accept="image/png"
          onFile={(f) => void onPick(f)}
          disabled={pending}
          hint={t('fileHint', { kb: rules.maxSizeKb, width: rules.maxWidth })}
          error={fields.file}
        >
          {picked ? (
            <>
              <TexturePreview key={`${picked.url}-${type}`} src={picked.url} type={type} height={128} />
              <span className="flex flex-col items-center gap-0.5">
                <span className="max-w-60 truncate text-sm font-medium">{file?.name}</span>
                <span className="text-caption text-muted-foreground">{t('picked', { w: picked.width, h: picked.height })}</span>
              </span>
              <span className="inline-flex items-center gap-1.5 text-caption font-medium text-primary">
                <RefreshCw className="size-3.5" aria-hidden />
                {t('replace')}
              </span>
            </>
          ) : undefined}
        </Dropzone>

        {duplicate !== null && (
          <Alert>
            <AlertDescription>
              {t('duplicate')}{' '}
              <Link href={`/skinlib/show/${duplicate}`} className="font-medium text-primary underline-offset-4 hover:underline">
                {t('viewExisting')}
              </Link>
            </AlertDescription>
          </Alert>
        )}

        <Field data-invalid={fields.name ? true : undefined}>
          <FieldLabel htmlFor="name">{t('name')}</FieldLabel>
          <Input id="name" name="name" value={name} onChange={(e) => setName(e.target.value)} maxLength={50} required aria-invalid={fields.name ? true : undefined} />
          {fields.name && <FieldError>{fields.name}</FieldError>}
        </Field>

        <FieldSet data-invalid={fields.type ? true : undefined}>
          <FieldLegend variant="label">{t('type')}</FieldLegend>
          <RadioGroup value={type} onValueChange={(v) => setType(v as TextureType)} className="flex flex-wrap gap-x-5 gap-y-2">
            {types.map((v) => (
              <Field key={v} orientation="horizontal" className="w-auto">
                <RadioGroupItem value={v} id={`type-${v}`} />
                <FieldLabel htmlFor={`type-${v}`} className="font-normal">
                  {tt(`types.${v}`)}
                </FieldLabel>
              </Field>
            ))}
          </RadioGroup>
          {fields.type ? <FieldError>{fields.type}</FieldError> : <FieldDescription>{t('typeHint')}</FieldDescription>}
        </FieldSet>

        <Field orientation="horizontal">
          <FieldContent>
            <FieldLabel htmlFor="public">{t('public')}</FieldLabel>
            <FieldDescription>{t('publicHint')}</FieldDescription>
          </FieldContent>
          <Switch id="public" checked={isPublic} onCheckedChange={setPublic} />
        </Field>

        <div className="flex items-start gap-2.5 rounded-lg bg-muted px-3.5 py-3 text-sm">
          <Coins className="mt-0.5 size-4 shrink-0 text-highlight" aria-hidden />
          <p className="text-muted-foreground">
            {file ? t('cost', { cost, score }) : t('rates', { pub: rules.rates.perStorage, priv: rules.rates.privatePerStorage })}
          </p>
        </div>

        {rules.contentPolicy && <p className="text-caption whitespace-pre-line text-muted-foreground">{rules.contentPolicy}</p>}

        <FormError message={error} />
        <Button type="submit" size="lg" loading={pending} disabled={!file}>
          {t('submit')}
        </Button>
      </div>
    </form>
  )
}
