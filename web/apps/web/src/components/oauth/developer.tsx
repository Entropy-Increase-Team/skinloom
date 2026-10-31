'use client'

import {
  createOAuthClient,
  createPersonalToken,
  deleteOAuthClient,
  deletePersonalToken,
  listOAuthClients,
  listOAuthGrants,
  listOAuthScopes,
  listPersonalTokens,
  type OAuthClientList,
  type OAuthClientSecret,
  type OAuthClientView,
  type OAuthGrantList,
  type OAuthGrantView,
  type OAuthScopes,
  type PersonalTokenList,
  type PersonalTokenView,
  type NewPersonalToken,
  resetOAuthClientSecret,
  revokeOAuthGrant,
  updateOAuthClient,
} from '@skinloom/api-client'
import { Badge, Button, Card, CardContent, CardDescription, CardHeader, CardTitle, Label } from '@skinloom/ui'
import { useLocale, useTranslations } from 'next-intl'
import { type FormEvent, type ReactNode, useCallback, useEffect, useState } from 'react'

import { Field, FormError } from '@/components/auth/field'
import { CopyButton } from '@/components/blocks/copy-button'
import { ScopeList, scopeKey } from '@/components/oauth/scopes'
import { problemOf } from '@/lib/problem'

const selectClass = 'h-9 rounded-md border border-input bg-transparent px-2 text-sm'
const textareaClass = 'rounded-md border border-input bg-transparent px-3 py-2 font-mono text-sm'

function Section({ title, description, children }: { title: string; description?: ReactNode; children: ReactNode }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        {description && <CardDescription>{description}</CardDescription>}
      </CardHeader>
      <CardContent className="flex flex-col gap-4">{children}</CardContent>
    </Card>
  )
}

/** A secret shown once, with a copy button. */
function OneTimeSecret({ label, value }: { label: string; value: string }) {
  const t = useTranslations('oauth')
  return (
    <div role="status" className="flex flex-col gap-2 rounded-md border border-emerald-500/40 bg-emerald-500/10 p-3">
      <p className="text-sm font-medium">{label}</p>
      <div className="flex flex-wrap items-center gap-2">
        <code className="break-all rounded bg-background px-2 py-1 font-mono text-sm">{value}</code>
        <CopyButton value={value} label={t('copy')} copiedLabel={t('copied')} />
      </div>
      <p className="text-xs text-muted-foreground">{t('shownOnce')}</p>
    </div>
  )
}

function useDate() {
  const locale = useLocale()
  return (iso: string) => new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(iso))
}

/** Field errors of a failed request, and a general message for anything else. */
function useFailure() {
  const t = useTranslations('oauth')
  const tc = useTranslations('common')
  const [error, setError] = useState<string | null>(null)
  const [fields, setFields] = useState<Record<string, string>>({})
  return {
    error,
    fields,
    clear: () => {
      setError(null)
      setFields({})
    },
    fail: (err: unknown) => {
      const p = problemOf(err)
      setFields(p.fields)
      setError(p.unverified ? tc('verifyFirst') : p.status === 422 ? null : t('failed'))
    },
  }
}

const lines = (s: string) =>
  s
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)

export function Developer({ admin, origin }: { admin: boolean; origin: string }) {
  return (
    <div className="flex flex-col gap-6">
      <AppsSection origin={origin} />
      <GrantsSection />
      <TokensSection admin={admin} origin={origin} />
    </div>
  )
}

function AppsSection({ origin }: { origin: string }) {
  const t = useTranslations('oauth')
  const when = useDate()
  const f = useFailure()
  const [apps, setApps] = useState<OAuthClientView[] | null>(null)
  const [secret, setSecret] = useState<{ id: number; value: string } | null>(null)
  const [editing, setEditing] = useState<number | null>(null)
  const [pending, setPending] = useState(false)

  const load = useCallback(async () => {
    try {
      setApps(((await listOAuthClients()).data as OAuthClientList).clients)
    } catch {
      setApps([])
    }
  }, [])
  useEffect(() => void load(), [load])

  const create = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const form = e.currentTarget
    const data = new FormData(form)
    setPending(true)
    f.clear()
    try {
      const res = (
        await createOAuthClient({
          name: String(data.get('name') ?? ''),
          redirectUris: lines(String(data.get('redirectUris') ?? '')),
          confidential: data.get('kind') === 'confidential',
        })
      ).data as OAuthClientSecret
      if (res.secret) setSecret({ id: res.client.id, value: res.secret })
      form.reset()
      await load()
    } catch (err) {
      f.fail(err)
    } finally {
      setPending(false)
    }
  }

  const save = async (app: OAuthClientView, e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const data = new FormData(e.currentTarget)
    setPending(true)
    f.clear()
    try {
      await updateOAuthClient(app.id, { name: String(data.get('name') ?? ''), redirectUris: lines(String(data.get('redirectUris') ?? '')) })
      setEditing(null)
      await load()
    } catch (err) {
      f.fail(err)
    } finally {
      setPending(false)
    }
  }

  const reset = async (app: OAuthClientView) => {
    if (!window.confirm(t('apps.resetConfirm', { name: app.name }))) return
    try {
      const res = (await resetOAuthClientSecret(app.id)).data as OAuthClientSecret
      setSecret({ id: app.id, value: res.secret })
    } catch (err) {
      f.fail(err)
    }
  }

  const remove = async (app: OAuthClientView) => {
    if (!window.confirm(t('apps.deleteConfirm', { name: app.name }))) return
    try {
      await deleteOAuthClient(app.id)
      if (secret?.id === app.id) setSecret(null)
      await load()
    } catch (err) {
      f.fail(err)
    }
  }

  return (
    <Section
      title={t('apps.title')}
      description={
        <>
          {t('apps.hint')}{' '}
          <span className="font-mono text-xs">
            {origin}/oauth/authorize · {origin}/oauth/token · {origin}/api
          </span>
        </>
      }
    >
      {apps === null && <p className="text-sm text-muted-foreground">…</p>}
      {apps?.length === 0 && <p className="text-sm text-muted-foreground">{t('apps.none')}</p>}
      <ul className="flex flex-col divide-y">
        {apps?.map((app) => (
          <li key={app.id} className="flex flex-col gap-3 py-3">
            {editing === app.id ? (
              <form onSubmit={(e) => void save(app, e)} className="flex flex-col gap-3">
                <Field id={`name-${app.id}`} name="name" label={t('apps.name')} defaultValue={app.name} required maxLength={64} error={f.fields.name} />
                <div className="flex flex-col gap-2">
                  <Label htmlFor={`uris-${app.id}`}>{t('apps.redirectUris')}</Label>
                  <textarea id={`uris-${app.id}`} name="redirectUris" rows={3} defaultValue={app.redirectUris.join('\n')} className={textareaClass} />
                  {f.fields.redirectUris && <p className="text-xs text-destructive">{f.fields.redirectUris}</p>}
                </div>
                <div className="flex gap-2">
                  <Button type="submit" size="sm" disabled={pending}>
                    {t('save')}
                  </Button>
                  <Button type="button" size="sm" variant="ghost" onClick={() => setEditing(null)}>
                    {t('cancel')}
                  </Button>
                </div>
              </form>
            ) : (
              <>
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{app.name}</span>
                  <Badge variant="outline">{app.confidential ? t('apps.confidential') : t('apps.public')}</Badge>
                  <span className="text-xs text-muted-foreground">{when(app.createdAt)}</span>
                </div>
                <div className="flex flex-wrap items-center gap-2 text-sm">
                  <span className="text-muted-foreground">client_id</span>
                  <code className="rounded bg-muted px-2 py-0.5 font-mono">{app.id}</code>
                  <CopyButton value={String(app.id)} label={t('copy')} copiedLabel={t('copied')} />
                </div>
                <ul className="flex flex-col gap-1 font-mono text-xs text-muted-foreground">
                  {app.redirectUris.map((u) => (
                    <li key={u} className="break-all">
                      {u}
                    </li>
                  ))}
                </ul>
                {secret?.id === app.id && <OneTimeSecret label={t('apps.secret')} value={secret.value} />}
                <div className="flex flex-wrap gap-2">
                  <Button size="sm" variant="outline" onClick={() => setEditing(app.id)}>
                    {t('edit')}
                  </Button>
                  {app.confidential && (
                    <Button size="sm" variant="outline" onClick={() => void reset(app)}>
                      {t('apps.resetSecret')}
                    </Button>
                  )}
                  <Button size="sm" variant="destructive" onClick={() => void remove(app)}>
                    {t('delete')}
                  </Button>
                </div>
              </>
            )}
          </li>
        ))}
      </ul>
      <form onSubmit={(e) => void create(e)} className="flex flex-col gap-3 border-t pt-4">
        <p className="font-medium">{t('apps.new')}</p>
        <Field id="app-name" name="name" label={t('apps.name')} required maxLength={64} error={editing === null ? f.fields.name : undefined} />
        <div className="flex flex-col gap-2">
          <Label htmlFor="app-uris">{t('apps.redirectUris')}</Label>
          <textarea id="app-uris" name="redirectUris" rows={2} required placeholder="https://example.com/callback" className={textareaClass} />
          <p className="text-xs text-muted-foreground">{t('apps.redirectHint')}</p>
          {editing === null && f.fields.redirectUris && <p className="text-xs text-destructive">{f.fields.redirectUris}</p>}
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="app-kind">{t('apps.kind')}</Label>
          <select id="app-kind" name="kind" defaultValue="confidential" className={selectClass}>
            <option value="confidential">{t('apps.confidentialHint')}</option>
            <option value="public">{t('apps.publicHint')}</option>
          </select>
        </div>
        <FormError message={f.error} />
        <Button type="submit" className="self-start" disabled={pending}>
          {t('apps.create')}
        </Button>
      </form>
    </Section>
  )
}

function GrantsSection() {
  const t = useTranslations('oauth')
  const when = useDate()
  const [grants, setGrants] = useState<OAuthGrantView[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const load = useCallback(async () => {
    try {
      setGrants(((await listOAuthGrants()).data as OAuthGrantList).grants)
    } catch {
      setGrants([])
    }
  }, [])
  useEffect(() => void load(), [load])

  const revoke = async (g: OAuthGrantView) => {
    if (!window.confirm(t('grants.revokeConfirm', { name: g.client.name }))) return
    try {
      await revokeOAuthGrant(g.client.id)
      await load()
    } catch {
      setError(t('failed'))
    }
  }

  return (
    <Section title={t('grants.title')} description={t('grants.hint')}>
      {grants === null && <p className="text-sm text-muted-foreground">…</p>}
      {grants?.length === 0 && <p className="text-sm text-muted-foreground">{t('grants.none')}</p>}
      <FormError message={error} />
      <ul className="flex flex-col divide-y">
        {grants?.map((g) => (
          <li key={g.client.id} className="flex flex-col gap-2 py-3">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-medium">{g.client.name}</span>
              <span className="text-xs text-muted-foreground">
                {t('by', { owner: g.client.owner })} · {t('grants.since', { date: when(g.updatedAt) })}
              </span>
            </div>
            <ScopeList scopes={g.scopes} />
            <Button size="sm" variant="outline" className="self-start" onClick={() => void revoke(g)}>
              {t('grants.revoke')}
            </Button>
          </li>
        ))}
      </ul>
    </Section>
  )
}

const expiries = [7, 30, 90, 365, 0]

function TokensSection({ admin, origin }: { admin: boolean; origin: string }) {
  const t = useTranslations('oauth')
  const ts = useTranslations('oauth.scopes')
  const when = useDate()
  const f = useFailure()
  const [tokens, setTokens] = useState<PersonalTokenView[] | null>(null)
  const [scopes, setScopes] = useState<OAuthScopes['scopes']>([])
  const [created, setCreated] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  const load = useCallback(async () => {
    try {
      setTokens(((await listPersonalTokens()).data as PersonalTokenList).tokens)
    } catch {
      setTokens([])
    }
  }, [])
  useEffect(() => {
    void load()
    listOAuthScopes().then(
      (r) => setScopes((r.data as OAuthScopes).scopes.filter((s) => admin || !s.admin)),
      () => setScopes([]),
    )
  }, [load, admin])

  const create = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const form = e.currentTarget
    const data = new FormData(form)
    setPending(true)
    f.clear()
    try {
      const res = (
        await createPersonalToken({
          name: String(data.get('name') ?? ''),
          scopes: data.getAll('scopes').map(String),
          expiresInDays: Number(data.get('expires') ?? 30),
        })
      ).data as NewPersonalToken
      setCreated(res.token)
      form.reset()
      await load()
    } catch (err) {
      f.fail(err)
    } finally {
      setPending(false)
    }
  }

  const remove = async (tok: PersonalTokenView) => {
    if (!window.confirm(t('tokens.deleteConfirm', { name: tok.name }))) return
    try {
      await deletePersonalToken(tok.id)
      await load()
    } catch (err) {
      f.fail(err)
    }
  }

  return (
    <Section title={t('tokens.title')} description={t('tokens.hint', { api: `${origin}/api/user` })}>
      {created && <OneTimeSecret label={t('tokens.created')} value={created} />}
      {tokens === null && <p className="text-sm text-muted-foreground">…</p>}
      {tokens?.length === 0 && <p className="text-sm text-muted-foreground">{t('tokens.none')}</p>}
      <ul className="flex flex-col divide-y">
        {tokens?.map((tok) => (
          <li key={tok.id} className="flex flex-col gap-2 py-3">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-medium">{tok.name}</span>
              {tok.scopes.map((s) => (
                <Badge key={s} variant="secondary" className="font-mono">
                  {s}
                </Badge>
              ))}
            </div>
            <p className="text-xs text-muted-foreground">
              {t('tokens.createdAt', { date: when(tok.createdAt) })} ·{' '}
              {tok.expiresAt ? t('tokens.expiresAt', { date: when(tok.expiresAt) }) : t('tokens.never')} ·{' '}
              {tok.lastUsedAt ? t('tokens.lastUsed', { date: when(tok.lastUsedAt) }) : t('tokens.unused')}
            </p>
            <Button size="sm" variant="outline" className="self-start" onClick={() => void remove(tok)}>
              {t('delete')}
            </Button>
          </li>
        ))}
      </ul>
      <form onSubmit={(e) => void create(e)} className="flex flex-col gap-3 border-t pt-4">
        <p className="font-medium">{t('tokens.new')}</p>
        <Field id="token-name" name="name" label={t('tokens.name')} required maxLength={64} error={f.fields.name} />
        <fieldset className="flex flex-col gap-2">
          <legend className="mb-2 text-sm font-medium">{t('tokens.scopes')}</legend>
          {scopes.map((s) => (
            <label key={s.name} className="flex items-start gap-2 text-sm">
              <input type="checkbox" name="scopes" value={s.name} className="mt-0.5 size-4 accent-primary" />
              <span>
                <span className="font-mono">{s.name}</span>
                <span className="text-muted-foreground"> — {ts.has(scopeKey(s.name)) ? ts(scopeKey(s.name) as 'User_Read') : ''}</span>
              </span>
            </label>
          ))}
          {f.fields.scopes && <p className="text-xs text-destructive">{f.fields.scopes}</p>}
        </fieldset>
        <div className="flex flex-col gap-2">
          <Label htmlFor="token-expires">{t('tokens.expires')}</Label>
          <select id="token-expires" name="expires" defaultValue="30" className={selectClass}>
            {expiries.map((days) => (
              <option key={days} value={days}>
                {days === 0 ? t('tokens.neverOption') : t('tokens.days', { days })}
              </option>
            ))}
          </select>
        </div>
        <FormError message={f.error} />
        <Button type="submit" className="self-start" disabled={pending}>
          {t('tokens.create')}
        </Button>
      </form>
    </Section>
  )
}
