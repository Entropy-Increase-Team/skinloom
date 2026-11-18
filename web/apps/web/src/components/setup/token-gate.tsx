'use client'

import { createSetupSession, getSetupState, type SetupState } from '@skinloom/api-client'
import {
  Alert,
  AlertDescription,
  Button,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  CopyField,
} from '@skinloom/ui'
import { ChevronDown, KeyRound } from 'lucide-react'
import { useTranslations } from 'next-intl'
import { type FormEvent, useCallback, useEffect, useRef, useState } from 'react'

import { Field, FormError } from '@/components/auth/field'
import { problemOf } from '@/lib/problem'

function extractToken(raw: string): string {
  const trimmed = raw.trim()
  if (!trimmed) return ''
  try {
    if (trimmed.includes('://') || trimmed.includes('token=')) {
      const url = trimmed.includes('://') ? new URL(trimmed) : new URL(trimmed, 'http://localhost')
      const param = url.searchParams.get('token')
      if (param) return param.trim()
    }
  } catch {
    // Fall back to regex/raw value if the user pasted a partial string.
  }
  const m = /[?&]token=([^&#\s]+)/.exec(trimmed)
  return m?.[1] ? m[1].trim() : trimmed
}

export function TokenGate({
  initialToken,
  onVerified,
}: {
  initialToken: string
  onVerified: (state: SetupState) => void
}) {
  const t = useTranslations('setup.gate')
  const te = useTranslations('setup.errors')
  const [input, setInput] = useState(initialToken)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [helpOpen, setHelpOpen] = useState(false)
  const autoTried = useRef(false)

  const verify = useCallback(
    async (raw: string) => {
      const token = extractToken(raw)
      if (!token) {
        setError(t('empty'))
        return
      }
      setPending(true)
      setError(null)
      try {
        await createSetupSession({ token })
        const res = await getSetupState()
        onVerified(res.data as SetupState)
      } catch (err) {
        const p = problemOf(err)
        if (p.status === 403) setError(te('token'))
        else if (p.status === 409) setError(te('done'))
        else setError(p.detail ?? te('failed'))
        setPending(false)
      }
    },
    [onVerified, t, te],
  )

  useEffect(() => {
    // Strip ?token= immediately so the one-time secret cannot linger in history or Referer.
    const qs = new URLSearchParams(window.location.search)
    const urlToken = qs.get('token') ?? initialToken
    if (qs.has('token')) {
      window.history.replaceState({}, '', window.location.pathname)
    }
    if (urlToken && !autoTried.current) {
      autoTried.current = true
      setInput(urlToken)
      void verify(urlToken)
    }
  }, [initialToken, verify])

  function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    void verify(input)
  }

  return (
    <div className="flex flex-col gap-5">
      <div className="flex items-start gap-3">
        <span className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
          <KeyRound className="size-5" aria-hidden />
        </span>
        <div className="flex min-w-0 flex-col gap-1">
          <h2 className="text-lg font-medium">{t('title')}</h2>
          <p className="text-sm text-muted-foreground">{t('intro')}</p>
        </div>
      </div>

      <FormError message={error} />

      <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
        <Field
          id="setup-token"
          label={t('label')}
          hint={t('hint')}
          value={input}
          onChange={(e) => setInput(e.target.value)}
          autoComplete="off"
          spellCheck={false}
          required
        />
        <Button type="submit" loading={pending} className="w-full sm:w-auto sm:self-end">
          {t('verify')}
        </Button>
      </form>

      <Collapsible open={helpOpen} onOpenChange={setHelpOpen} className="rounded-lg border bg-muted/30 p-3">
        <CollapsibleTrigger className="flex w-full items-center justify-between gap-2 text-left text-sm font-medium">
          <span>{t('whereTitle')}</span>
          <ChevronDown className={`size-4 text-muted-foreground transition-transform ${helpOpen ? 'rotate-180' : ''}`} aria-hidden />
        </CollapsibleTrigger>
        <CollapsibleContent className="flex flex-col gap-3 pt-3">
          <Alert>
            <AlertDescription>{t('whereDesc')}</AlertDescription>
          </Alert>
          <div className="flex flex-col gap-1.5">
            <span className="text-xs text-muted-foreground">{t('cmdBinary')}</span>
            <CopyField value="skinloom setup-link" />
          </div>
          <div className="flex flex-col gap-1.5">
            <span className="text-xs text-muted-foreground">{t('cmdDocker')}</span>
            <CopyField value="docker compose exec skinloom skinloom setup-link" />
          </div>
          <div className="flex flex-col gap-1.5">
            <span className="text-xs text-muted-foreground">{t('cmdSystemd')}</span>
            <CopyField value="journalctl -u skinloom -n 50 --no-pager | grep setup" />
          </div>
        </CollapsibleContent>
      </Collapsible>
    </div>
  )
}
