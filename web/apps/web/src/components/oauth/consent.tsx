'use client'

import {
  type AuthorizeInfo,
  type AuthorizeResult,
  authorize,
  type GetAuthorizationParams,
  getAuthorization,
  type PlayerOption,
} from '@skinloom/api-client'
import {
  Alert,
  AlertDescription,
  Button,
  ButtonLink,
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
  cn,
  Skeleton,
  UserAvatar,
} from '@skinloom/ui'
import Link from 'next/link'
import { useTranslations } from 'next-intl'
import { type KeyboardEvent, useCallback, useEffect, useRef, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { ScopeList } from '@/components/oauth/scopes'
import { problemOf } from '@/lib/problem'

export function PlayerPicker({
  players,
  selectedId,
  onSelect,
}: {
  players: PlayerOption[]
  selectedId: number | null
  onSelect: (id: number) => void
}) {
  const t = useTranslations('oauth')
  const itemRefs = useRef<Array<HTMLButtonElement | null>>([])
  if (players.length === 0) {
    return (
      <Alert variant="destructive">
        <AlertDescription className="flex flex-col gap-2">
          <span>{t('noPlayersHint')}</span>
          <ButtonLink render={<Link href="/user/player" />} size="sm" variant="outline" className="w-fit">
            {t('goCreatePlayer')}
          </ButtonLink>
        </AlertDescription>
      </Alert>
    )
  }
  const onKeyDown = (idx: number, e: KeyboardEvent<HTMLButtonElement>) => {
    let nextIdx = -1
    if (e.key === 'ArrowDown' || e.key === 'ArrowRight') {
      nextIdx = (idx + 1) % players.length
    } else if (e.key === 'ArrowUp' || e.key === 'ArrowLeft') {
      nextIdx = (idx - 1 + players.length) % players.length
    } else if (e.key === 'Home') {
      nextIdx = 0
    } else if (e.key === 'End') {
      nextIdx = players.length - 1
    }
    if (nextIdx >= 0) {
      e.preventDefault()
      onSelect(players[nextIdx]!.id)
      itemRefs.current[nextIdx]?.focus()
    }
  }
  return (
    <div className="flex flex-col gap-2" role="radiogroup" aria-label={t('selectPlayer')}>
      <p className="text-sm font-medium">{t('selectPlayer')}</p>
      <div className="flex flex-col gap-2">
        {players.map((pl, idx) => {
          const active = selectedId === pl.id
          const tabbable = active || (selectedId === null && idx === 0)
          return (
            <button
              key={pl.id}
              ref={(el) => {
                itemRefs.current[idx] = el
              }}
              type="button"
              role="radio"
              aria-checked={active}
              tabIndex={tabbable ? 0 : -1}
              onClick={() => onSelect(pl.id)}
              onKeyDown={(e) => onKeyDown(idx, e)}
              className={cn(
                'flex cursor-pointer items-center gap-3 rounded-lg border px-3 py-2.5 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                active ? 'border-primary bg-primary/5 ring-1 ring-primary' : 'border-border hover:bg-muted/60',
              )}
            >
              <UserAvatar of={{ player: pl.name }} size={36} />
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm font-medium">{pl.name}</div>
                <div className="truncate font-mono text-xs text-muted-foreground">{pl.uuid}</div>
              </div>
            </button>
          )
        })}
      </div>
    </div>
  )
}

/**
 * The consent screen of the OAuth / OIDC / Yggdrasil Connect authorization flow:
 * shows what an application asks for, optionally lets the user pick a character
 * (Yggdrasil.PlayerProfiles.Select), and sends the browser back with the answer.
 */
export function Consent({ params }: { params: GetAuthorizationParams }) {
  const t = useTranslations('oauth')
  const tc = useTranslations('common')
  const [info, setInfo] = useState<AuthorizeInfo | null>(null)
  const [selectedPlayerId, setSelectedPlayerId] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [pendingAction, setPendingAction] = useState<'allow' | 'deny' | null>(null)
  const started = useRef(false)

  const answer = useCallback(
    async (allow: boolean, playerIdOverride?: number | null) => {
      setPendingAction(allow ? 'allow' : 'deny')
      setError(null)
      try {
        const pid = playerIdOverride ?? selectedPlayerId
        const res = await authorize({
          response_type: params.response_type ?? '',
          client_id: params.client_id ?? '',
          redirect_uri: params.redirect_uri,
          scope: params.scope,
          state: params.state,
          nonce: params.nonce,
          code_challenge: params.code_challenge,
          code_challenge_method: params.code_challenge_method,
          ...(allow && pid ? { playerId: pid } : {}),
          allow,
        })
        window.location.assign((res.data as AuthorizeResult).redirect)
      } catch (err) {
        setError(problemOf(err)?.detail ?? t('failed'))
        setPendingAction(null)
      }
    },
    [params, selectedPlayerId, t],
  )

  const load = useCallback(() => {
    setError(null)
    getAuthorization(params).then(
      (res) => {
        const data = res.data as AuthorizeInfo
        if (data.redirect) {
          window.location.assign(data.redirect)
          return
        }
        setInfo(data)
        if (data.needsPlayerSelect && data.players && data.players.length > 0) {
          setSelectedPlayerId(data.players[0]!.id)
        }
        if (data.granted && !data.needsPlayerSelect) void answer(true)
      },
      (err) => setError(problemOf(err)?.detail ?? t('failed')),
    )
  }, [answer, params, t])

  useEffect(() => {
    if (started.current) return
    started.current = true
    load()
  }, [load])

  if (info?.error) {
    return (
      <Card>
        <CardHeader>
          <CardTitle as="h1">{t('invalidTitle')}</CardTitle>
          <CardDescription>{t('invalidHint')}</CardDescription>
        </CardHeader>
        <CardContent>
          <p className="break-words rounded-md bg-muted px-3 py-2 font-mono text-sm">{info.error}</p>
        </CardContent>
      </Card>
    )
  }
  if (!info?.client || (info.granted && !info.needsPlayerSelect)) {
    return (
      <Card>
        <CardContent className="flex flex-col gap-3 py-6 text-sm text-muted-foreground">
          {error ? (
            <>
              <FormError message={error} />
              <Button type="button" variant="outline" size="sm" className="self-start" onClick={load}>
                {tc('retry')}
              </Button>
            </>
          ) : info?.granted ? (
            <p className="py-2 text-center">{t('redirecting')}</p>
          ) : (
            <div className="flex flex-col gap-3">
              <Skeleton className="h-5 w-2/3" />
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-16 w-full" />
            </div>
          )}
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
  const canAllow = !info.needsPlayerSelect || (selectedPlayerId !== null && selectedPlayerId > 0)
  return (
    <Card>
      <CardHeader>
        <CardTitle as="h1" className="break-words text-xl">{t('title', { app: info.client.name })}</CardTitle>
        <CardDescription>{info.client.owner ? t('by', { owner: info.client.owner }) : t('dynamicClient')}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <p className="text-sm">{t('wants')}</p>
        <ScopeList scopes={info.scopes ?? []} />
        {info.needsPlayerSelect && (
          <PlayerPicker
            players={info.players ?? []}
            selectedId={selectedPlayerId}
            onSelect={setSelectedPlayerId}
          />
        )}
        <p className="break-all text-xs text-muted-foreground">{t('redirectTo', { host })}</p>
        <FormError message={error} />
      </CardContent>
      <CardFooter className="flex flex-wrap justify-end gap-2">
        <Button
          variant="outline"
          loading={pendingAction === 'deny'}
          disabled={pendingAction !== null}
          onClick={() => void answer(false)}
        >
          {t('deny')}
        </Button>
        <Button
          loading={pendingAction === 'allow'}
          disabled={!canAllow || pendingAction !== null}
          onClick={() => void answer(true)}
        >
          {t('allow')}
        </Button>
      </CardFooter>
    </Card>
  )
}
