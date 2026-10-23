'use client'

import { type Answer, type CaptchaInfo, register } from '@skinloom/api-client'
import { Button } from '@skinloom/ui'
import { useRouter } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { type FormEvent, useState } from 'react'

import { problemOf } from '@/lib/problem'

import { Captcha } from './captcha'
import { Field, FormError } from './field'

export function RegisterForm({ captcha }: { captcha: CaptchaInfo }) {
  const t = useTranslations('register')
  const tf = useTranslations('fieldErrors')
  const router = useRouter()
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [fields, setFields] = useState<Record<string, string>>({})
  const [answer, setAnswer] = useState<Answer | null>(null)
  const [resetKey, setResetKey] = useState(0)

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    const get = (k: string) => String(f.get(k) ?? '')
    if (get('password') !== get('confirm')) {
      setFields({ confirm: tf('passwordMismatch') })
      return
    }
    if (captcha.provider !== 'none' && !answer) {
      setFields({ captcha: tf('captchaRequired') })
      return
    }
    setPending(true)
    setError(null)
    setFields({})
    try {
      await register({
        email: get('email'),
        nickname: get('nickname'),
        password: get('password'),
        ...(answer ? { captcha: answer } : {}),
      })
      router.replace('/')
      router.refresh()
    } catch (err) {
      const p = problemOf(err)
      setFields(
        Object.fromEntries(
          Object.entries(p.fields).map(([k, v]) => {
            if (k === 'email' && v === 'already registered') return [k, tf('emailTaken')]
            if (k === 'captcha') return [k, tf('captchaWrong')]
            return [k, tf.has(k) ? tf(k as 'email') : v]
          }),
        ),
      )
      const key = { 403: 'closed', 429: 'tooMany', 503: 'captchaDown' }[p.status] ?? (p.status === 422 ? null : 'failed')
      setError(key ? t(`errors.${key}` as 'errors.failed') : null)
      // An answer is checked once, right or wrong: always start a fresh challenge.
      setAnswer(null)
      setResetKey((k) => k + 1)
      setPending(false)
    }
  }

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
      <FormError message={error} />
      <Field id="email" label={t('email')} type="email" autoComplete="email" required autoFocus error={fields.email} />
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
      <Captcha config={captcha} onChange={setAnswer} resetKey={resetKey} error={fields.captcha} />
      <Button type="submit" disabled={pending}>
        {pending ? t('submitting') : t('submit')}
      </Button>
    </form>
  )
}
