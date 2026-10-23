'use client'

import { completeSetup } from '@skinloom/api-client'
import { Button } from '@skinloom/ui'
import { useRouter } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { type FormEvent, useEffect, useState } from 'react'

import { problemOf } from '@/lib/problem'

import { Field, FormError } from './field'

export function SetupForm({ token, siteName }: { token: string; siteName: string }) {
  const t = useTranslations('setup')
  const router = useRouter()
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [fields, setFields] = useState<Record<string, string>>({})
  // The address this page was opened with is the best guess for the public URL.
  const [siteUrl, setSiteUrl] = useState('')
  useEffect(() => setSiteUrl(window.location.origin), [])

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    const get = (k: string) => String(f.get(k) ?? '')
    if (get('password') !== get('confirm')) {
      setFields({ confirm: t('passwordMismatch') })
      return
    }
    setPending(true)
    setError(null)
    setFields({})
    try {
      await completeSetup({
        token: get('token'),
        siteName: get('siteName'),
        siteUrl: get('siteUrl'),
        email: get('email'),
        nickname: get('nickname'),
        password: get('password'),
      })
      router.replace('/')
      router.refresh()
    } catch (err) {
      const p = problemOf(err)
      // The server explains in English; show our own words for the fields we know.
      setFields(
        Object.fromEntries(
          Object.entries(p.fields).map(([k, v]) => [k, t.has(`fieldErrors.${k}`) ? t(`fieldErrors.${k}` as 'fieldErrors.email') : v]),
        ),
      )
      if (p.status === 403) setError(t('errors.token'))
      else if (p.status === 409) setError(t('errors.done'))
      else if (p.status !== 422) setError(t('errors.failed'))
      setPending(false)
    }
  }

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-6" noValidate>
      <FormError message={error} />
      {token ? (
        <input type="hidden" name="token" value={token} />
      ) : (
        <Field id="token" label={t('token')} hint={t('tokenHint')} required autoComplete="off" spellCheck={false} />
      )}

      <fieldset className="flex flex-col gap-4">
        <legend className="mb-2 text-sm font-semibold">{t('site')}</legend>
        <Field id="siteName" label={t('siteName')} defaultValue={siteName} maxLength={64} error={fields.siteName} />
        <Field
          id="siteUrl"
          label={t('siteUrl')}
          hint={t('siteUrlHint')}
          type="url"
          value={siteUrl}
          onChange={(e) => setSiteUrl(e.target.value)}
          error={fields.siteUrl}
        />
      </fieldset>

      <fieldset className="flex flex-col gap-4">
        <legend className="mb-2 text-sm font-semibold">{t('admin')}</legend>
        <Field id="email" label={t('email')} type="email" autoComplete="username" required error={fields.email} />
        <Field id="nickname" label={t('nickname')} autoComplete="nickname" required maxLength={32} error={fields.nickname} />
        <Field
          id="password"
          label={t('password')}
          hint={t('passwordHint')}
          type="password"
          autoComplete="new-password"
          required
          minLength={8}
          maxLength={128}
          error={fields.password}
        />
        <Field id="confirm" label={t('confirm')} type="password" autoComplete="new-password" required error={fields.confirm} />
      </fieldset>

      <Button type="submit" size="lg" disabled={pending}>
        {pending ? t('submitting') : t('submit')}
      </Button>
    </form>
  )
}
