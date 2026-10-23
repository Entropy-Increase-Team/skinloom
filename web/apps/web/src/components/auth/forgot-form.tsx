'use client'

import { type Answer, type CaptchaInfo, forgotPassword } from '@skinloom/api-client'
import { Button } from '@skinloom/ui'
import { useTranslations } from 'next-intl'
import { type FormEvent, useState } from 'react'

import { problemOf } from '@/lib/problem'

import { Captcha } from './captcha'
import { Field, FormError } from './field'

export function ForgotForm({ captcha }: { captcha: CaptchaInfo }) {
  const t = useTranslations('forgot')
  const tf = useTranslations('fieldErrors')
  const [pending, setPending] = useState(false)
  const [sent, setSent] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [fields, setFields] = useState<Record<string, string>>({})
  const [answer, setAnswer] = useState<Answer | null>(null)
  const [resetKey, setResetKey] = useState(0)

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const email = String(new FormData(e.currentTarget).get('email') ?? '')
    if (captcha.provider !== 'none' && !answer) {
      setFields({ captcha: tf('captchaRequired') })
      return
    }
    setPending(true)
    setError(null)
    setFields({})
    try {
      await forgotPassword({ email, ...(answer ? { captcha: answer } : {}) })
      setSent(true)
    } catch (err) {
      const p = problemOf(err)
      setFields(
        Object.fromEntries(Object.keys(p.fields).map((k) => [k, k === 'captcha' ? tf('captchaWrong') : tf.has(k) ? tf(k as 'email') : p.fields[k]])),
      )
      const key = { 429: 'tooMany', 503: 'unavailable' }[p.status] ?? (p.status === 422 ? null : 'failed')
      setError(key ? t(`errors.${key}` as 'errors.failed') : null)
      setAnswer(null)
      setResetKey((k) => k + 1)
    } finally {
      setPending(false)
    }
  }

  if (sent) {
    // Same message whether or not the address has an account.
    return <p className="rounded-md border bg-muted/40 px-3 py-3 text-sm leading-relaxed">{t('sent')}</p>
  }
  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
      <FormError message={error} />
      <Field id="email" label={t('email')} type="email" autoComplete="email" required autoFocus error={fields.email} />
      <Captcha config={captcha} onChange={setAnswer} resetKey={resetKey} error={fields.captcha} />
      <Button type="submit" disabled={pending}>
        {pending ? t('submitting') : t('submit')}
      </Button>
    </form>
  )
}
