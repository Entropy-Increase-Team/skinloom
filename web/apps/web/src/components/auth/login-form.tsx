'use client'

import { type Answer, type CaptchaInfo, linkSocialPending, login, type SocialProviderInfo } from '@skinloom/api-client'
import { Button, Checkbox, Label } from '@skinloom/ui'
import { useRouter } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { type FormEvent, useState } from 'react'

import { findMissingPluginFormErrors, PluginForms } from '@/components/plugin-forms'
import { useBootstrap } from '@/components/providers'
import { Slot } from '@/components/slot'
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
  const boot = useBootstrap()
  const router = useRouter()
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [fields, setFields] = useState<Record<string, string>>({})
  // The server asks for a captcha after a few failed attempts (422 on body.captcha);
  // from then on the form keeps showing one.
  const [needCaptcha, setNeedCaptcha] = useState(false)
  const [captchaError, setCaptchaError] = useState<string>()
  const [answer, setAnswer] = useState<Answer | null>(null)
  const [resetKey, setResetKey] = useState(0)
  const [remember, setRemember] = useState(false)
  const [extraForm, setExtraForm] = useState<Record<string, unknown>>({})

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    if (needCaptcha && !answer) {
      setCaptchaError(tf('captchaRequired'))
      return
    }
    const missingExtra = findMissingPluginFormErrors(boot.forms, 'auth.login', extraForm, tf('required'))
    if (Object.keys(missingExtra).length > 0) {
      setFields(missingExtra)
      return
    }
    setPending(true)
    setError(null)
    setFields({})
    setCaptchaError(undefined)
    try {
      await login({
        email: String(f.get('email') ?? ''),
        password: String(f.get('password') ?? ''),
        remember,
        ...(needCaptcha && answer ? { captcha: answer } : {}),
        ...(Object.keys(extraForm).length > 0 ? { form: extraForm } : {}),
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
      setFields(p.fields)
      if (p.fields.captcha) {
        setCaptchaError(needCaptcha ? tf('captchaWrong') : tf('captchaRequired'))
        setNeedCaptcha(true)
      } else if (p.status === 422 && p.detail && Object.keys(p.fields).length === 0) {
        setError(p.detail)
      } else if (p.status === 422 && Object.keys(p.fields).length > 0) {
        setError(null)
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
      <PluginForms scope="auth.login" values={extraForm} onChange={setExtraForm} errors={fields} />
      {needCaptcha && <Captcha config={captcha} onChange={setAnswer} resetKey={resetKey} error={captchaError} />}
      <Label className="w-fit gap-2 font-normal text-muted-foreground">
        <Checkbox checked={remember} onCheckedChange={setRemember} />
        {t('remember')}
      </Label>
      <Button type="submit" size="lg" loading={pending}>
        {t('signIn')}
      </Button>
      <Slot name="auth.login.below" />
      {!linkPending && (
        <>
          <SocialButtons providers={providers} next={next} remember={() => remember} />
          <Slot name="auth.login.providers" props={{ next }} />
        </>
      )}
    </form>
  )
}
