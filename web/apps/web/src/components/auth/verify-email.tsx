'use client'

import { resendVerification, verifyEmail } from '@skinloom/api-client'
import { Button } from '@skinloom/ui'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { useState } from 'react'

import { problemOf } from '@/lib/problem'

/**
 * Confirms a verification link. It takes a click (a POST), not just opening the page:
 * mail scanners that open links must not use them up.
 */
export function VerifyEmail({ token }: { token: string }) {
  const t = useTranslations('verify')
  const router = useRouter()
  const [state, setState] = useState<'idle' | 'pending' | 'done' | 'invalid' | 'failed'>('idle')

  async function confirm() {
    setState('pending')
    try {
      await verifyEmail({ token })
      setState('done')
      router.refresh() // the header and banner pick up the new status
    } catch (err) {
      setState(problemOf(err).status === 403 ? 'invalid' : 'failed')
    }
  }

  if (state === 'done' || state === 'invalid') {
    return (
      <div className="flex flex-col gap-4">
        <p className="rounded-md border bg-muted/40 px-3 py-3 text-sm">{state === 'done' ? t('done') : t('invalid')}</p>
        <Button asChild variant="outline">
          <Link href="/">{t('home')}</Link>
        </Button>
      </div>
    )
  }
  return (
    <div className="flex flex-col gap-4">
      {state === 'failed' && <p className="text-sm text-destructive">{t('failed')}</p>}
      <Button onClick={confirm} disabled={state === 'pending'}>
        {state === 'pending' ? t('confirming') : t('confirm')}
      </Button>
    </div>
  )
}

/** Shown to signed-in users whose address is not verified yet. */
/**
 * Reminds a signed-in user to verify their address. When the site requires it, it says
 * what is blocked until then; without working mail there is nothing to resend.
 */
export function VerifyBanner({ email, required, canResend }: { email: string; required: boolean; canResend: boolean }) {
  const t = useTranslations('verify')
  const [state, setState] = useState<'idle' | 'pending' | 'sent' | 'wait' | 'failed'>('idle')
  async function resend() {
    setState('pending')
    try {
      await resendVerification()
      setState('sent')
    } catch (err) {
      setState(problemOf(err).status === 429 ? 'wait' : 'failed')
    }
  }
  return (
    <div className="flex flex-wrap items-center justify-center gap-x-3 gap-y-1 bg-amber-500/15 px-4 py-2 text-sm">
      <span>{t(required ? 'bannerRequired' : 'banner', { email })}</span>
      {!canResend ? (
        <span className="text-muted-foreground">{t('noMail')}</span>
      ) : state === 'sent' ? (
        <span className="text-muted-foreground">{t('resent')}</span>
      ) : (
        <Button variant="link" size="sm" className="h-auto p-0" onClick={resend} disabled={state === 'pending'}>
          {state === 'wait' ? t('wait') : state === 'failed' ? t('resendFailed') : t('resend')}
        </Button>
      )}
    </div>
  )
}
