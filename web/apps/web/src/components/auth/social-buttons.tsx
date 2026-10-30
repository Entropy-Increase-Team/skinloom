'use client'

import { type SocialProviderInfo, type SocialRedirect, startSocialLogin } from '@skinloom/api-client'
import { Button } from '@skinloom/ui'
import { KeyRound } from 'lucide-react'
import { useTranslations } from 'next-intl'
import { useState } from 'react'

import { problemOf } from '@/lib/problem'

import { FormError } from './field'

/** The provider's mark: GitHub's octocat, Microsoft's four squares, a key for OpenID Connect. */
export function ProviderIcon({ type, className = 'size-4' }: { type: string; className?: string }) {
  if (type === 'github') {
    return (
      <svg viewBox="0 0 24 24" aria-hidden="true" className={className} fill="currentColor">
        <path d="M12 .3a12 12 0 0 0-3.8 23.38c.6.12.83-.26.83-.57L9 21.07c-3.34.72-4.04-1.61-4.04-1.61-.55-1.39-1.34-1.76-1.34-1.76-1.08-.74.09-.73.09-.73 1.2.09 1.84 1.24 1.84 1.24 1.07 1.83 2.81 1.3 3.5 1 .1-.78.42-1.31.76-1.61-2.67-.3-5.47-1.33-5.47-5.93 0-1.31.47-2.38 1.24-3.22-.14-.3-.54-1.52.1-3.18 0 0 1-.32 3.3 1.23a11.5 11.5 0 0 1 6 0c2.28-1.55 3.29-1.23 3.29-1.23.64 1.66.24 2.88.12 3.18a4.65 4.65 0 0 1 1.23 3.22c0 4.61-2.8 5.62-5.48 5.92.42.36.81 1.1.81 2.22l-.01 3.29c0 .31.2.69.82.57A12 12 0 0 0 12 .3" />
      </svg>
    )
  }
  if (type === 'microsoft') {
    return (
      <svg viewBox="0 0 23 23" aria-hidden="true" className={className}>
        <path fill="#f25022" d="M1 1h10v10H1z" />
        <path fill="#7fba00" d="M12 1h10v10H12z" />
        <path fill="#00a4ef" d="M1 12h10v10H1z" />
        <path fill="#ffb900" d="M12 12h10v10H12z" />
      </svg>
    )
  }
  return <KeyRound aria-hidden="true" className={className} />
}

/** "Continue with …" buttons: each sends the browser to the provider's sign-in page. */
export function SocialButtons({ providers, next, remember }: { providers: SocialProviderInfo[]; next: string; remember?: () => boolean }) {
  const t = useTranslations('social')
  const [pending, setPending] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  if (providers.length === 0) return null

  async function go(id: string) {
    setPending(id)
    setError(null)
    try {
      const res = await startSocialLogin(id, { next, remember: remember?.() ?? false })
      window.location.assign((res.data as SocialRedirect).url)
    } catch (err) {
      const { status } = problemOf(err)
      setError(t(status === 429 ? 'errors.tooMany' : status === 502 ? 'errors.unreachable' : 'errors.failed'))
      setPending(null)
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-3 text-xs text-muted-foreground">
        <span className="h-px flex-1 bg-border" />
        {t('or')}
        <span className="h-px flex-1 bg-border" />
      </div>
      <FormError message={error} />
      {providers.map((p) => (
        <Button key={p.id} type="button" variant="outline" disabled={pending !== null} onClick={() => void go(p.id)}>
          <ProviderIcon type={p.type} />
          {pending === p.id ? t('redirecting') : t('continueWith', { name: p.name })}
        </Button>
      ))}
    </div>
  )
}

const errorCodes = ['expired', 'denied', 'failed', 'disabled', 'gone', 'taken', 'already'] as const

/** The message for ?social_error=<code> after the provider sent the browser back. */
export function SocialError({ code }: { code?: string }) {
  const t = useTranslations('social')
  if (!code) return null
  const known = (errorCodes as readonly string[]).includes(code) ? code : 'failed'
  return <FormError message={t(`callback.${known}` as 'callback.failed')} />
}
