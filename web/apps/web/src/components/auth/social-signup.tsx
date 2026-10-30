'use client'

import { getSocialPending, linkSocialPending, registerSocial, type SocialPending } from '@skinloom/api-client'
import { Button } from '@skinloom/ui'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { type FormEvent, useEffect, useState } from 'react'

import { problemOf } from '@/lib/problem'
import { safeNext } from '@/lib/safe-url'

import { Field, FormError } from './field'
import { ProviderIcon } from './social-buttons'

/**
 * After a sign-in with a provider account that no account here is linked to: create
 * an account for it, or link it to an existing one (the signed-in one, or one to
 * sign in to).
 */
export function SocialSignup({ signedInAs, mail }: { signedInAs?: string; mail: boolean }) {
  const t = useTranslations('social')
  const tf = useTranslations('fieldErrors')
  const router = useRouter()
  const [pending, setPending] = useState<SocialPending | null | undefined>(undefined)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [fields, setFields] = useState<Record<string, string>>({})
  const [email, setEmail] = useState('')

  useEffect(() => {
    getSocialPending().then(
      (r) => {
        const p = r.data as SocialPending
        setPending(p)
        setEmail(p.email)
      },
      () => setPending(null),
    )
  }, [])

  if (pending === undefined) return <p className="text-sm text-muted-foreground">…</p>
  if (pending === null) {
    return (
      <div className="flex flex-col gap-4">
        <p className="text-sm">{t('pending.expired')}</p>
        <Button asChild className="self-start">
          <Link href="/auth/login">{t('pending.again')}</Link>
        </Button>
      </div>
    )
  }

  const provider = pending.providerName || pending.provider
  const verified = pending.emailVerified && email.trim().toLowerCase() === pending.email.toLowerCase()

  async function link() {
    setBusy(true)
    setError(null)
    try {
      await linkSocialPending()
      router.replace('/user/profile?social_linked=1')
      router.refresh()
    } catch (err) {
      setError(t(problemOf(err).status === 409 ? 'callback.already' : 'callback.expired'))
      setBusy(false)
    }
  }

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    setBusy(true)
    setError(null)
    setFields({})
    try {
      await registerSocial({ email: String(f.get('email') ?? ''), nickname: String(f.get('nickname') ?? '') })
      router.replace(safeNext(pending?.next || '/user'))
      router.refresh()
    } catch (err) {
      const p = problemOf(err)
      setFields(
        Object.fromEntries(
          Object.entries(p.fields).map(([k, v]) => [k, k === 'email' && v === 'already registered' ? t('pending.emailTaken') : tf.has(k) ? tf(k as 'email') : v]),
        ),
      )
      if (p.status === 429) setError(t('pending.tooMany'))
      else if (p.status === 404) setError(t('pending.expired'))
      else if (p.status === 403) setError(t('pending.closed'))
      else if (p.status !== 422) setError(t('errors.failed'))
      setBusy(false)
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center gap-3 rounded-md border px-3 py-2 text-sm">
        <ProviderIcon type={pending.providerType} className="size-5" />
        <span>{t('pending.signedInWith', { provider, name: pending.name })}</span>
      </div>
      <FormError message={error} />

      {signedInAs ? (
        <div className="flex flex-col gap-3">
          <p className="text-sm text-muted-foreground">{t('pending.linkCurrent', { provider, nickname: signedInAs })}</p>
          <Button className="self-start" disabled={busy} onClick={() => void link()}>
            {t('pending.linkButton')}
          </Button>
        </div>
      ) : (
        <>
          {pending.registration === 'open' ? (
            <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
              <p className="text-sm text-muted-foreground">{t('pending.createHint')}</p>
              <Field
                id="email"
                label={t('pending.email')}
                type="email"
                autoComplete="email"
                required
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                hint={verified ? t('pending.emailVerified', { provider }) : mail ? t('pending.emailMail') : undefined}
                error={fields.email}
              />
              <Field id="nickname" label={t('pending.nickname')} defaultValue={pending.nickname.slice(0, 32)} maxLength={32} required error={fields.nickname} />
              <Button type="submit" disabled={busy}>
                {t('pending.create')}
              </Button>
            </form>
          ) : (
            <p className="text-sm text-muted-foreground">{t('pending.closed')}</p>
          )}
          <p className="text-sm text-muted-foreground">
            {t('pending.haveAccount')}{' '}
            <Link href="/auth/login?social=link" className="font-medium text-foreground underline-offset-4 hover:underline">
              {t('pending.signInToLink', { provider })}
            </Link>
          </p>
        </>
      )}
    </div>
  )
}
