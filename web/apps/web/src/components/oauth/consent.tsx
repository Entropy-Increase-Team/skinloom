'use client'

import { type AuthorizeInfo, type AuthorizeResult, authorize, type GetAuthorizationParams, getAuthorization } from '@skinloom/api-client'
import { Button, Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@skinloom/ui'
import { useTranslations } from 'next-intl'
import { useEffect, useRef, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { ScopeList } from '@/components/oauth/scopes'

/**
 * The consent screen of the OAuth authorization flow: shows what an application asks
 * for and sends the browser back to it with the answer. An application the user
 * already allowed (for no more than this) is approved without asking, as Blessing
 * Skin did.
 */
export function Consent({ params }: { params: GetAuthorizationParams }) {
  const t = useTranslations('oauth')
  const [info, setInfo] = useState<AuthorizeInfo | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)
  const started = useRef(false)

  const answer = async (allow: boolean) => {
    setPending(true)
    setError(null)
    try {
      const res = await authorize({
        response_type: params.response_type ?? '',
        client_id: params.client_id ?? '',
        redirect_uri: params.redirect_uri,
        scope: params.scope,
        state: params.state,
        code_challenge: params.code_challenge,
        code_challenge_method: params.code_challenge_method,
        allow,
      })
      window.location.assign((res.data as AuthorizeResult).redirect)
    } catch {
      setError(t('failed'))
      setPending(false)
    }
  }

  useEffect(() => {
    if (started.current) return
    started.current = true
    getAuthorization(params).then(
      (res) => {
        const data = res.data as AuthorizeInfo
        if (data.redirect) {
          window.location.assign(data.redirect)
          return
        }
        setInfo(data)
        if (data.granted) void answer(true)
      },
      () => setError(t('failed')),
    )
  }, [])

  if (info?.error) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{t('invalidTitle')}</CardTitle>
          <CardDescription>{t('invalidHint')}</CardDescription>
        </CardHeader>
        <CardContent>
          <p className="rounded-md bg-muted px-3 py-2 font-mono text-sm">{info.error}</p>
        </CardContent>
      </Card>
    )
  }
  if (!info?.client || info.granted) {
    return (
      <Card>
        <CardContent className="py-8 text-center text-sm text-muted-foreground">
          {error ? <FormError message={error} /> : t('redirecting')}
        </CardContent>
      </Card>
    )
  }

  let host = info.redirectUri ?? ''
  try {
    host = new URL(host).host || host
  } catch {
    // an app scheme such as com.example.app:/cb: show it whole
  }
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-xl">{t('title', { app: info.client.name })}</CardTitle>
        <CardDescription>{t('by', { owner: info.client.owner })}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <p className="text-sm">{t('wants')}</p>
        <ScopeList scopes={info.scopes ?? []} />
        <p className="text-xs text-muted-foreground">{t('redirectTo', { host })}</p>
        <FormError message={error} />
      </CardContent>
      <CardFooter className="flex justify-end gap-2">
        <Button variant="outline" disabled={pending} onClick={() => void answer(false)}>
          {t('deny')}
        </Button>
        <Button disabled={pending} onClick={() => void answer(true)}>
          {t('allow')}
        </Button>
      </CardFooter>
    </Card>
  )
}
