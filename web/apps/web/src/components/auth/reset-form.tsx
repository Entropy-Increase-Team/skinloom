'use client'

import { resetPassword } from '@skinloom/api-client'
import { Button } from '@skinloom/ui'
import Link from 'next/link'
import { useTranslations } from 'next-intl'
import { type FormEvent, useState } from 'react'

import { problemOf } from '@/lib/problem'

import { Field, FormError } from './field'

export function ResetForm({ token }: { token: string }) {
  const t = useTranslations('reset')
  const tf = useTranslations('fieldErrors')
  const [pending, setPending] = useState(false)
  const [done, setDone] = useState(false)
  const [invalid, setInvalid] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [fields, setFields] = useState<Record<string, string>>({})

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    const password = String(f.get('password') ?? '')
    if (password !== String(f.get('confirm') ?? '')) {
      setFields({ confirm: tf('passwordMismatch') })
      return
    }
    setPending(true)
    setError(null)
    setFields({})
    try {
      await resetPassword({ token, password })
      setDone(true)
    } catch (err) {
      const p = problemOf(err)
      if (p.status === 403) setInvalid(true)
      else if (p.status === 422) setFields({ password: tf('password') })
      else setError(t('errors.failed'))
    } finally {
      setPending(false)
    }
  }

  if (done || invalid) {
    return (
      <div className="flex flex-col gap-4">
        <p className="rounded-md border bg-muted/40 px-3 py-3 text-sm leading-relaxed">{done ? t('done') : t('invalid')}</p>
        <Button asChild>
          <Link href={done ? '/auth/login' : '/auth/forgot'}>{done ? t('signIn') : t('again')}</Link>
        </Button>
      </div>
    )
  }
  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
      <FormError message={error} />
      <Field
        id="password"
        label={t('password')}
        hint={t('passwordHint')}
        type="password"
        autoComplete="new-password"
        required
        minLength={8}
        maxLength={128}
        autoFocus
        error={fields.password}
      />
      <Field id="confirm" label={t('confirm')} type="password" autoComplete="new-password" required error={fields.confirm} />
      <Button type="submit" disabled={pending}>
        {pending ? t('submitting') : t('submit')}
      </Button>
    </form>
  )
}
