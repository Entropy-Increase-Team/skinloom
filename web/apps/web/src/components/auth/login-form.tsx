'use client'

import { type Answer, type CaptchaInfo, linkSocialPending, login, type SocialProviderInfo } from '@skinloom/api-client'
import { Button } from '@skinloom/ui'
import { useRouter } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { type FormEvent, useRef, useState } from 'react'

import { problemOf } from '@/lib/problem'

import { Captcha } from './captcha'
import { Field, FormError } from './field'
import { SocialButtons } from './social-buttons'

/**
 * The email and password form, then the sign-in providers. With linkPending the
 * person came from /auth/social: once signed in, the provider account they just used
 * is linked to this account.
 */
export function LoginForm({
  next,
  captcha,
  providers = [],
  linkPending = false,
}: {
  next: string
  captcha: CaptchaInfo
  providers?: SocialProviderInfo[]
  linkPending?: boolean
}) {
  const t = useTranslations('auth')
  const tf = useTranslations('fieldErrors')
  const router = useRouter()
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // The server asks for a captcha after a few failed attempts (422 on body.captcha);
  // from then on the form keeps showing one.
  const [needCaptcha, setNeedCaptcha] = useState(false)
  const [captchaError, setCaptchaError] = useState<string>()
  const [answer, setAnswer] = useState<Answer | null>(null)
  const [resetKey, setResetKey] = useState(0)
  const remember = useRef<HTMLInputElement>(null)

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    if (needCaptcha && !answer) {
      setCaptchaError(tf('captchaRequired'))
      return
    }
    setPending(true)
    setError(null)
    setCaptchaError(undefined)
    try {
      await login({
        email: String(f.get('email') ?? ''),
        password: String(f.get('password') ?? ''),
        remember: f.get('remember') === 'on',
        ...(needCaptcha && answer ? { captcha: answer } : {}),
      })
      if (linkPending) {
        const code = await linkSocialPending().then(
          () => 'social_linked=1',
          (err) => `social_error=${problemOf(err).status === 409 ? 'already' : 'expired'}`,
        )
        router.replace(`/user/profile?${code}`)
      } else {
        router.replace(next)
      }
      router.refresh()
    } catch (err) {
      const p = problemOf(err)
      if (p.fields.captcha) {
        setCaptchaError(needCaptcha ? tf('captchaWrong') : tf('captchaRequired'))
        setNeedCaptcha(true)
      } else {
        // The server deliberately gives one answer for unknown accounts and wrong passwords.
        const key = { 401: 'invalid', 403: 'disabled', 429: 'tooMany', 503: 'captchaDown' }[p.status] ?? 'failed'
        setError(t(`errors.${key}` as 'errors.failed'))
      }
      if (needCaptcha) {
        setAnswer(null)
        setResetKey((k) => k + 1) // each answer is checked once
      }
      setPending(false)
    }
  }

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
      <FormError message={error} />
      <Field id="email" label={t('email')} type="email" autoComplete="username" required autoFocus />
      <Field id="password" label={t('password')} type="password" autoComplete="current-password" required />
      {needCaptcha && <Captcha config={captcha} onChange={setAnswer} resetKey={resetKey} error={captchaError} />}
      <label className="flex items-center gap-2 text-sm text-muted-foreground">
        <input ref={remember} type="checkbox" name="remember" className="size-4 accent-primary" />
        {t('remember')}
      </label>
      <Button type="submit" disabled={pending}>
        {pending ? t('signingIn') : t('signIn')}
      </Button>
      {!linkPending && <SocialButtons providers={providers} next={next} remember={() => remember.current?.checked ?? false} />}
    </form>
  )
}
