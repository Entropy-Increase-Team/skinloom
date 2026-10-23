'use client'

import { login } from '@skinloom/api-client'
import { Button } from '@skinloom/ui'
import { useRouter } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { type FormEvent, useState } from 'react'

import { problemOf } from '@/lib/problem'

import { Field, FormError } from './field'

export function LoginForm({ next }: { next: string }) {
  const t = useTranslations('auth')
  const router = useRouter()
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    setPending(true)
    setError(null)
    try {
      await login({
        email: String(f.get('email') ?? ''),
        password: String(f.get('password') ?? ''),
        remember: f.get('remember') === 'on',
      })
      router.replace(next)
      router.refresh()
    } catch (err) {
      const p = problemOf(err)
      // The server deliberately gives one answer for unknown accounts and wrong passwords.
      const key = { 401: 'invalid', 403: 'disabled', 429: 'tooMany' }[p.status] ?? 'failed'
      setError(t(`errors.${key}` as 'errors.failed'))
      setPending(false)
    }
  }

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
      <FormError message={error} />
      <Field id="email" label={t('email')} type="email" autoComplete="username" required autoFocus />
      <Field id="password" label={t('password')} type="password" autoComplete="current-password" required />
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
