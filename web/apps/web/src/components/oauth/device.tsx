'use client'

import { approveDeviceCode, type DeviceCodeInfo, lookupDeviceCode } from '@skinloom/api-client'
import { Alert, AlertDescription, Button, Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle, Input } from '@skinloom/ui'
import { CheckCircle2, ShieldAlert, XCircle } from 'lucide-react'
import { useTranslations } from 'next-intl'
import { type FormEvent, useEffect, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { PlayerPicker } from '@/components/oauth/consent'
import { ScopeList } from '@/components/oauth/scopes'
import { problemOf } from '@/lib/problem'

function countAlnum(raw: string): number {
  return raw.replace(/[^A-Za-z0-9]/g, '').length
}

/** RFC 8628 Device Authorization Grant verification & consent screen. */
export function DeviceConsent({ initialCode }: { initialCode: string }) {
  const t = useTranslations('oauth')
  const [code, setCode] = useState(initialCode)
  const [info, setInfo] = useState<DeviceCodeInfo | null>(null)
  const [selectedPlayerId, setSelectedPlayerId] = useState<number | null>(null)
  const [loading, setLoading] = useState(() => initialCode.trim().length > 0)
  const [pendingAction, setPendingAction] = useState<'allow' | 'deny' | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [done, setDone] = useState<'approved' | 'denied' | null>(null)

  const lookup = async (rawCode: string) => {
    const trimmed = rawCode.trim()
    if (!trimmed) return
    setLoading(true)
    setError(null)
    setInfo(null)
    try {
      const res = await lookupDeviceCode({ user_code: trimmed })
      const data = res.data as DeviceCodeInfo
      if (data.error) {
        setError(data.error)
      } else {
        setInfo(data)
        if (data.needsPlayerSelect && data.players && data.players.length > 0) {
          setSelectedPlayerId(data.players[0]!.id)
        }
      }
    } catch (err) {
      setError(problemOf(err)?.detail ?? t('failed'))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    if (initialCode.trim()) {
      void lookup(initialCode)
    }
  }, [initialCode])

  const onSubmitCode = (e: FormEvent) => {
    e.preventDefault()
    void lookup(code)
  }

  const answer = async (allow: boolean) => {
    setPendingAction(allow ? 'allow' : 'deny')
    setError(null)
    try {
      await approveDeviceCode({
        user_code: code.trim(),
        allow,
        ...(allow && selectedPlayerId ? { playerId: selectedPlayerId } : {}),
      })
      setDone(allow ? 'approved' : 'denied')
    } catch (err) {
      setError(problemOf(err)?.detail ?? t('failed'))
    } finally {
      setPendingAction(null)
    }
  }

  if (done) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center gap-3 py-10 text-center">
          {done === 'approved' ? (
            <CheckCircle2 className="size-10 text-success" aria-hidden />
          ) : (
            <XCircle className="size-10 text-muted-foreground" aria-hidden />
          )}
          <h1 className="text-lg font-medium">{done === 'approved' ? t('device.approvedTitle') : t('device.deniedTitle')}</h1>
          <p className="text-sm text-muted-foreground">{done === 'approved' ? t('device.approvedHint') : t('device.deniedHint')}</p>
        </CardContent>
      </Card>
    )
  }

  if (!info?.client) {
    return (
      <Card>
        <CardHeader>
          <CardTitle as="h1" className="text-xl">{t('device.pageTitle')}</CardTitle>
          <CardDescription>{t('device.enterCodeHint')}</CardDescription>
        </CardHeader>
        <form onSubmit={onSubmitCode} className="flex flex-col gap-(--card-spacing)">
          <CardContent className="flex flex-col gap-4">
            <Input
              value={code}
              maxLength={32}
              onChange={(e) => setCode(e.target.value.toUpperCase())}
              placeholder="ABCD-EFGH"
              autoComplete="off"
              className="text-center font-mono text-lg tracking-widest uppercase"
              aria-label={t('device.codeLabel')}
            />
            <FormError message={error} />
          </CardContent>
          <CardFooter className="flex justify-end">
            <Button type="submit" loading={loading} disabled={countAlnum(code) < 6}>
              {t('device.continue')}
            </Button>
          </CardFooter>
        </form>
      </Card>
    )
  }

  const canAllow = !info.needsPlayerSelect || (selectedPlayerId !== null && selectedPlayerId > 0)
  return (
    <Card>
      <CardHeader>
        <CardTitle as="h1" className="break-words text-xl">{t('title', { app: info.client.name })}</CardTitle>
        <CardDescription>{info.client.owner ? t('by', { owner: info.client.owner }) : t('dynamicClient')}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <Alert>
          <ShieldAlert className="size-4" aria-hidden />
          <AlertDescription className="break-words">{t('device.phishingWarning', { code: code.trim().toUpperCase() })}</AlertDescription>
        </Alert>
        <p className="text-sm">{t('wants')}</p>
        <ScopeList scopes={info.scopes ?? []} />
        {info.needsPlayerSelect && (
          <PlayerPicker
            players={info.players ?? []}
            selectedId={selectedPlayerId}
            onSelect={setSelectedPlayerId}
          />
        )}
        <FormError message={error} />
      </CardContent>
      <CardFooter className="flex flex-wrap items-center justify-between gap-2">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          disabled={pendingAction !== null}
          onClick={() => {
            setInfo(null)
            setError(null)
          }}
        >
          {t('device.useAnotherCode')}
        </Button>
        <div className="flex flex-wrap gap-2">
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
        </div>
      </CardFooter>
    </Card>
  )
}
