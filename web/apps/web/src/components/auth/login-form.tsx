'use client'

import { type Answer, type CaptchaInfo, login } from '@skinloom/api-client'
import { Button } from '@skinloom/ui'
import { useRouter } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { type FormEvent, useState } from 'react'

import { problemOf } from '@/lib/problem'

import { Captcha } from './captcha'
import { Field, FormError } from './field'

export function LoginForm({ next, captcha }: { next: string; captcha: CaptchaInfo }) {
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
      router.replace(next)
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
        <input type="checkbox" name="remember" className="size-4 accent-primary" />
        {t('remember')}
      </label>
      <Button type="submit" disabled={pending}>
        {pending ? t('signingIn') : t('signIn')}
      </Button>
    </form>
  )
}
