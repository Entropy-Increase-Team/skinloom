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
  type NewPersonalToken,
  type OAuthClientList,
  type OAuthClientSecret,
  type OAuthClientView,
  type OAuthGrantList,
  type OAuthGrantView,
  type OAuthScopes,
  type PersonalTokenList,
  type PersonalTokenView,
  resetOAuthClientSecret,
  revokeOAuthGrant,
  updateOAuthClient,
} from '@skinloom/api-client'
import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
  Button,
  Checkbox,
  CopyField,
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
  Field as UiField,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldLabel,
  FieldLegend,
  FieldSet,
  RadioGroup,
  RadioGroupItem,
  RelativeTime,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Skeleton,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  Textarea,
  toast,
  useConfirm,
} from '@skinloom/ui'
import { AppWindow, KeyRound, KeySquare, MoreHorizontal, Pencil, Plus, RotateCcw, ShieldCheck, Trash2 } from 'lucide-react'
import { useTranslations } from 'next-intl'
import { type FormEvent, type ReactNode, useCallback, useEffect, useState } from 'react'

import { Field, FormError } from '@/components/auth/field'
import { ScopeList, scopeKey } from '@/components/oauth/scopes'
import { problemOf } from '@/lib/problem'

/** A secret shown once: copy it now. */
function OneTimeSecret({ label, value }: { label: string; value: string }) {
  const t = useTranslations('oauth')
  return (
    <Alert role="status">
      <KeyRound />
      <AlertTitle>{label}</AlertTitle>
      <AlertDescription className="flex flex-col gap-2">
        <CopyField value={value} label={label} className="bg-background" />
        <span>{t('shownOnce')}</span>
      </AlertDescription>
    </Alert>
  )
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

/** A toast for a failure outside a form. */
function useToastFailure() {
  const t = useTranslations('oauth')
  const tc = useTranslations('common')
  return (err: unknown) => toast.error(problemOf(err).unverified ? tc('verifyFirst') : t('failed'))
}

const lines = (s: string) =>
  s
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)

/** A list row: what it is, facts underneath, actions on the right. */
function Row({ title, badges, meta, actions, children }: { title: ReactNode; badges?: ReactNode; meta?: ReactNode; actions?: ReactNode; children?: ReactNode }) {
  return (
    <li className="flex flex-col gap-3 p-4 sm:p-5">
      <div className="flex items-start gap-3">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="truncate font-medium">{title}</span>
            {badges}
          </div>
          {meta && <p className="flex flex-wrap gap-x-1.5 text-caption text-muted-foreground">{meta}</p>}
        </div>
        {actions}
      </div>
      {children}
    </li>
  )
}

function Loading() {
  return <Skeleton className="h-32 rounded-xl" />
}

function Nothing({ icon, title, body, action }: { icon: ReactNode; title: string; body?: string; action?: ReactNode }) {
  return (
    <Empty className="border border-dashed py-12">
      <EmptyHeader>
        <EmptyMedia variant="icon">{icon}</EmptyMedia>
        <EmptyTitle>{title}</EmptyTitle>
        {body && <EmptyDescription>{body}</EmptyDescription>}
      </EmptyHeader>
      {action && <EmptyContent>{action}</EmptyContent>}
    </Empty>
  )
}

/** Applications you registered, applications you allowed, personal access tokens: one tab each. */
export function Developer({ admin, origin }: { admin: boolean; origin: string }) {
  const t = useTranslations('oauth')
  return (
    <Tabs defaultValue="apps" className="gap-5">
      <TabsList>
        <TabsTrigger value="apps">{t('apps.title')}</TabsTrigger>
        <TabsTrigger value="grants">{t('grants.title')}</TabsTrigger>
        <TabsTrigger value="tokens">{t('tokens.title')}</TabsTrigger>
      </TabsList>
      <TabsContent value="apps">
        <AppsSection origin={origin} />
      </TabsContent>
      <TabsContent value="grants">
        <GrantsSection />
      </TabsContent>
      <TabsContent value="tokens">
        <TokensSection admin={admin} origin={origin} />
      </TabsContent>
    </Tabs>
  )
}

function AppsSection({ origin }: { origin: string }) {
  const t = useTranslations('oauth')
  const confirm = useConfirm()
  const failed = useToastFailure()
  const [apps, setApps] = useState<OAuthClientView[] | null>(null)
  const [secret, setSecret] = useState<{ id: number; value: string } | null>(null)
  const [editing, setEditing] = useState<OAuthClientView | 'new' | null>(null)

  const load = useCallback(async () => {
    try {
      setApps(((await listOAuthClients()).data as OAuthClientList).clients)
    } catch {
      setApps([])
    }
  }, [])
  useEffect(() => void load(), [load])

  const reset = async (app: OAuthClientView) => {
    const ok = await confirm({ title: t('apps.resetTitle', { name: app.name }), description: t('apps.resetConfirm'), confirmLabel: t('apps.resetSecret') })
    if (!ok) return
    try {
      const res = (await resetOAuthClientSecret(app.id)).data as OAuthClientSecret
      setSecret({ id: app.id, value: res.secret })
    } catch (err) {
      failed(err)
    }
  }

  const remove = async (app: OAuthClientView) => {
    const ok = await confirm({ title: t('apps.deleteTitle', { name: app.name }), description: t('apps.deleteConfirm'), confirmLabel: t('apps.delete') })
    if (!ok) return
    try {
      await deleteOAuthClient(app.id)
      if (secret?.id === app.id) setSecret(null)
      toast.success(t('apps.deleted', { name: app.name }))
      await load()
    } catch (err) {
      failed(err)
    }
  }

  const create = (
    <Button onClick={() => setEditing('new')}>
      <Plus />
      {t('apps.new')}
    </Button>
  )

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end gap-3">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <p className="text-sm text-muted-foreground">{t('apps.hint')}</p>
          <p className="font-mono text-caption break-all text-faint-foreground">
            {origin}/oauth/authorize · {origin}/oauth/token · {origin}/api
          </p>
        </div>
        {apps && apps.length > 0 && create}
      </div>
      {apps === null && <Loading />}
      {apps?.length === 0 && <Nothing icon={<AppWindow />} title={t('apps.none')} action={create} />}
      {apps && apps.length > 0 && (
        <ul className="flex flex-col divide-y rounded-xl border bg-card">
          {apps.map((app) => (
            <Row
              key={app.id}
              title={app.name}
              badges={<Badge variant="outline">{app.confidential ? t('apps.confidential') : t('apps.public')}</Badge>}
              meta={<RelativeTime date={app.createdAt} />}
              actions={
                <DropdownMenu>
                  <DropdownMenuTrigger render={<Button variant="ghost" size="icon-sm" aria-label={t('menu', { name: app.name })} title={t('menu', { name: app.name })} />}>
                    <MoreHorizontal />
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="w-44">
                    <DropdownMenuItem onClick={() => setEditing(app)}>
                      <Pencil />
                      {t('edit')}
                    </DropdownMenuItem>
                    {app.confidential && (
                      <DropdownMenuItem onClick={() => void reset(app)}>
                        <RotateCcw />
                        {t('apps.resetSecret')}
                      </DropdownMenuItem>
                    )}
                    <DropdownMenuSeparator />
                    <DropdownMenuItem variant="destructive" onClick={() => void remove(app)}>
                      <Trash2 />
                      {t('apps.delete')}
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              }
            >
              <div className="flex flex-col gap-1.5">
                <span className="text-caption text-muted-foreground">client_id</span>
                <CopyField value={String(app.id)} label="client_id" className="sm:max-w-xs" />
              </div>
              <ul className="flex flex-col gap-0.5 font-mono text-caption break-all text-muted-foreground">
                {app.redirectUris.map((u) => (
                  <li key={u}>{u}</li>
                ))}
              </ul>
              {secret?.id === app.id && <OneTimeSecret label={t('apps.secret')} value={secret.value} />}
            </Row>
          ))}
        </ul>
      )}
      <AppDialog
        editing={editing}
        onClose={() => setEditing(null)}
        onSaved={async (created) => {
          if (created?.secret) setSecret({ id: created.client.id, value: created.secret })
          await load()
        }}
      />
    </div>
  )
}

/** Register an application, or edit its name and redirect URIs. */
function AppDialog({
  editing,
  onClose,
  onSaved,
}: {
  editing: OAuthClientView | 'new' | null
  onClose: () => void
  onSaved: (created: OAuthClientSecret | null) => Promise<void>
}) {
  const t = useTranslations('oauth')
  const tc = useTranslations('common')
  const f = useFailure()
  const [pending, setPending] = useState(false)
  const [kind, setKind] = useState('confidential')
  const app = editing === 'new' ? null : editing

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const data = new FormData(e.currentTarget)
    const body = { name: String(data.get('name') ?? ''), redirectUris: lines(String(data.get('redirectUris') ?? '')) }
    setPending(true)
    f.clear()
    try {
      if (app) {
        await updateOAuthClient(app.id, body)
        await onSaved(null)
      } else {
        await onSaved((await createOAuthClient({ ...body, confidential: kind === 'confidential' })).data as OAuthClientSecret)
      }
      onClose()
    } catch (err) {
      f.fail(err)
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog
      open={editing !== null}
      onOpenChange={(open) => {
        if (!open) {
          setKind('confidential')
          onClose()
        }
        f.clear()
      }}
    >
      <DialogContent className="sm:max-w-md">
        <form key={editing === null ? 'closed' : (app?.id ?? 'new')} onSubmit={(e) => void submit(e)} className="flex flex-col gap-5">
          <DialogHeader>
            <DialogTitle>{app ? t('apps.editTitle', { name: app.name }) : t('apps.new')}</DialogTitle>
            {!app && <DialogDescription>{t('apps.newBody')}</DialogDescription>}
          </DialogHeader>
          <FormError message={f.error} />
          <Field id="app-name" name="name" label={t('apps.name')} defaultValue={app?.name} required maxLength={64} error={f.fields.name} />
          <UiField data-invalid={f.fields.redirectUris ? true : undefined}>
            <FieldLabel htmlFor="app-uris">{t('apps.redirectUris')}</FieldLabel>
            <Textarea
              id="app-uris"
              name="redirectUris"
              rows={3}
              required
              defaultValue={app?.redirectUris.join('\n')}
              placeholder="https://example.com/callback"
              className="font-mono text-caption"
              aria-invalid={f.fields.redirectUris ? true : undefined}
            />
            {f.fields.redirectUris ? <FieldError>{f.fields.redirectUris}</FieldError> : <FieldDescription>{t('apps.redirectHint')}</FieldDescription>}
          </UiField>
          {!app && (
            <FieldSet>
              <FieldLegend variant="label">{t('apps.kind')}</FieldLegend>
              <RadioGroup value={kind} onValueChange={(v) => setKind(String(v))}>
                {(['confidential', 'public'] as const).map((k) => (
                  <UiField key={k} orientation="horizontal">
                    <RadioGroupItem value={k} id={`kind-${k}`} />
                    <FieldContent>
                      <FieldLabel htmlFor={`kind-${k}`}>{t(`apps.${k}`)}</FieldLabel>
                      <FieldDescription>{t(`apps.${k}Hint`)}</FieldDescription>
                    </FieldContent>
                  </UiField>
                ))}
              </RadioGroup>
            </FieldSet>
          )}
          <DialogFooter>
            <DialogClose render={<Button type="button" variant="ghost" />}>{tc('cancel')}</DialogClose>
            <Button type="submit" loading={pending}>
              {app ? tc('save') : t('apps.create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function GrantsSection() {
  const t = useTranslations('oauth')
  const confirm = useConfirm()
  const failed = useToastFailure()
  const [grants, setGrants] = useState<OAuthGrantView[] | null>(null)
  const load = useCallback(async () => {
    try {
      setGrants(((await listOAuthGrants()).data as OAuthGrantList).grants)
    } catch {
      setGrants([])
    }
  }, [])
  useEffect(() => void load(), [load])

  const revoke = async (g: OAuthGrantView) => {
    const ok = await confirm({ title: t('grants.revokeTitle', { name: g.client.name }), description: t('grants.revokeConfirm'), confirmLabel: t('grants.revoke') })
    if (!ok) return
    try {
      await revokeOAuthGrant(g.client.id)
      toast.success(t('grants.revoked', { name: g.client.name }))
      await load()
    } catch (err) {
      failed(err)
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-muted-foreground">{t('grants.hint')}</p>
      {grants === null && <Loading />}
      {grants?.length === 0 && <Nothing icon={<ShieldCheck />} title={t('grants.none')} />}
      {grants && grants.length > 0 && (
        <ul className="flex flex-col divide-y rounded-xl border bg-card">
          {grants.map((g) => (
            <Row
              key={g.client.id}
              title={g.client.name}
              meta={
                <>
                  <span>{g.client.owner ? t('by', { owner: g.client.owner }) : t('dynamicClient')}</span>
                  <span aria-hidden>·</span>
                  <span>{t.rich('grants.since', { t: () => <RelativeTime date={g.updatedAt} /> })}</span>
                </>
              }
              actions={
                <Button size="sm" variant="outline" onClick={() => void revoke(g)}>
                  {t('grants.revoke')}
                </Button>
              }
            >
              <ScopeList scopes={g.scopes} />
            </Row>
          ))}
        </ul>
      )}
    </div>
  )
}

const expiries = [7, 30, 90, 365, 0]

function TokensSection({ admin, origin }: { admin: boolean; origin: string }) {
  const t = useTranslations('oauth')
  const confirm = useConfirm()
  const failed = useToastFailure()
  const [tokens, setTokens] = useState<PersonalTokenView[] | null>(null)
  const [scopes, setScopes] = useState<OAuthScopes['scopes']>([])
  const [created, setCreated] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

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
      (r) => setScopes((r.data as OAuthScopes).scopes.filter((s) => !s.oidc && (admin || !s.admin))),
      () => setScopes([]),
    )
  }, [load, admin])

  const remove = async (tok: PersonalTokenView) => {
    const ok = await confirm({ title: t('tokens.deleteTitle', { name: tok.name }), description: t('tokens.deleteConfirm'), confirmLabel: t('tokens.delete') })
    if (!ok) return
    try {
      await deletePersonalToken(tok.id)
      toast.success(t('tokens.deleted', { name: tok.name }))
      await load()
    } catch (err) {
      failed(err)
    }
  }

  const create = (
    <Button onClick={() => setCreating(true)}>
      <Plus />
      {t('tokens.new')}
    </Button>
  )

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end gap-3">
        <p className="min-w-0 flex-1 text-sm text-muted-foreground">{t('tokens.hint', { api: `${origin}/api/user` })}</p>
        {tokens && tokens.length > 0 && create}
      </div>
      {created && <OneTimeSecret label={t('tokens.created')} value={created} />}
      {tokens === null && <Loading />}
      {tokens?.length === 0 && <Nothing icon={<KeySquare />} title={t('tokens.none')} action={create} />}
      {tokens && tokens.length > 0 && (
        <ul className="flex flex-col divide-y rounded-xl border bg-card">
          {tokens.map((tok) => (
            <Row
              key={tok.id}
              title={tok.name}
              meta={
                <>
                  <span>{t.rich('tokens.createdAt', { t: () => <RelativeTime date={tok.createdAt} /> })}</span>
                  <span aria-hidden>·</span>
                  <span>{tok.expiresAt ? t.rich('tokens.expiresAt', { t: () => <RelativeTime date={tok.expiresAt!} /> }) : t('tokens.never')}</span>
                  <span aria-hidden>·</span>
                  <span>{tok.lastUsedAt ? t.rich('tokens.lastUsed', { t: () => <RelativeTime date={tok.lastUsedAt!} /> }) : t('tokens.unused')}</span>
                </>
              }
              actions={
                <Button size="icon-sm" variant="ghost" aria-label={t('tokens.deleteTitle', { name: tok.name })} title={t('tokens.delete')} onClick={() => void remove(tok)}>
                  <Trash2 />
                </Button>
              }
            >
              <div className="flex flex-wrap gap-1.5">
                {tok.scopes.map((s) => (
                  <Badge key={s} variant="secondary" className="font-mono">
                    {s}
                  </Badge>
                ))}
              </div>
            </Row>
          ))}
        </ul>
      )}
      <TokenDialog
        open={creating}
        scopes={scopes}
        onClose={() => setCreating(false)}
        onCreated={async (token) => {
          setCreated(token)
          await load()
        }}
      />
    </div>
  )
}

function TokenDialog({
  open,
  scopes,
  onClose,
  onCreated,
}: {
  open: boolean
  scopes: OAuthScopes['scopes']
  onClose: () => void
  onCreated: (token: string) => Promise<void>
}) {
  const t = useTranslations('oauth')
  const ts = useTranslations('oauth.scopes')
  const tc = useTranslations('common')
  const f = useFailure()
  const [pending, setPending] = useState(false)
  const [picked, setPicked] = useState<string[]>([])
  const expiryItems = expiries.map((days) => ({ value: String(days), label: days === 0 ? t('tokens.neverOption') : t('tokens.days', { days }) }))

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const data = new FormData(e.currentTarget)
    setPending(true)
    f.clear()
    try {
      const res = (await createPersonalToken({ name: String(data.get('name') ?? ''), scopes: picked, expiresInDays: Number(data.get('expires') ?? 30) }))
        .data as NewPersonalToken
      await onCreated(res.token)
      setPicked([])
      onClose()
    } catch (err) {
      f.fail(err)
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          setPicked([])
          onClose()
        }
        f.clear()
      }}
    >
      <DialogContent className="flex max-h-[90dvh] flex-col sm:max-w-md">
        <form key={open ? 'open' : 'closed'} onSubmit={(e) => void submit(e)} className="flex min-h-0 flex-col gap-5">
          <DialogHeader>
            <DialogTitle>{t('tokens.new')}</DialogTitle>
            <DialogDescription>{t('tokens.newBody')}</DialogDescription>
          </DialogHeader>
          <div className="-mx-1 flex min-h-0 flex-col gap-5 overflow-y-auto px-1">
            <FormError message={f.error} />
            <Field id="token-name" name="name" label={t('tokens.name')} required maxLength={64} error={f.fields.name} />
            <FieldSet data-invalid={f.fields.scopes ? true : undefined}>
              <FieldLegend variant="label">{t('tokens.scopes')}</FieldLegend>
              <div className="flex flex-col gap-3">
                {scopes.map((s) => (
                  <UiField key={s.name} orientation="horizontal">
                    <Checkbox
                      id={`scope-${s.name}`}
                      checked={picked.includes(s.name)}
                      onCheckedChange={(on) => setPicked((l) => (on ? [...l, s.name] : l.filter((x) => x !== s.name)))}
                    />
                    <FieldContent>
                      <FieldLabel htmlFor={`scope-${s.name}`} className="font-mono text-caption">
                        {s.name}
                      </FieldLabel>
                      {ts.has(scopeKey(s.name)) && <FieldDescription>{ts(scopeKey(s.name) as 'User_Read')}</FieldDescription>}
                    </FieldContent>
                  </UiField>
                ))}
              </div>
              {f.fields.scopes && <FieldError>{f.fields.scopes}</FieldError>}
            </FieldSet>
            <UiField>
              <FieldLabel htmlFor="token-expires">{t('tokens.expires')}</FieldLabel>
              <Select name="expires" items={expiryItems} defaultValue="30">
                <SelectTrigger id="token-expires" className="w-full sm:w-48">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {expiryItems.map((i) => (
                    <SelectItem key={i.value} value={i.value}>
                      {i.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </UiField>
          </div>
          <DialogFooter>
            <DialogClose render={<Button type="button" variant="ghost" />}>{tc('cancel')}</DialogClose>
            <Button type="submit" loading={pending}>
              {t('tokens.create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
