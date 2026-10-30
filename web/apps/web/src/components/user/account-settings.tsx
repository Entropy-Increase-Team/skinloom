'use client'

import {
  changeEmail,
  changePassword,
  deleteMe,
  listSessions,
  type LocaleInfo,
  revokeOtherSessions,
  revokeSession,
  type SessionView,
  setAvatar,
  signOutLaunchers,
  updateMe,
  type YggdrasilTokens,
  getYggdrasil,
  linkSocial,
  listSocial,
  type SocialLinks,
  type SocialRedirect,
  unlinkSocial,
} from '@skinloom/api-client'
import { Badge, Button, Card, CardContent, CardDescription, CardHeader, CardTitle, Label } from '@skinloom/ui'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { useLocale, useTranslations } from 'next-intl'
import { type FormEvent, type ReactNode, useCallback, useEffect, useState } from 'react'

import { Field, FormError } from '@/components/auth/field'
import { ProviderIcon, SocialError } from '@/components/auth/social-buttons'
import { CopyButton } from '@/components/blocks/copy-button'
import { Avatar } from '@/components/user/avatar'
import { problemOf } from '@/lib/problem'
import { describeUA } from '@/lib/ua'
import type { Viewer } from '@/lib/viewer'

function Section({ title, description, children }: { title: string; description?: string; children: ReactNode }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        {description && <CardDescription>{description}</CardDescription>}
      </CardHeader>
      <CardContent>{children}</CardContent>
    </Card>
  )
}

function Done({ message }: { message: string | null }) {
  if (!message) return null
  return (
    <p role="status" className="rounded-md border border-emerald-500/40 bg-emerald-500/10 px-3 py-2 text-sm">
      {message}
    </p>
  )
}

// Changing the password or email signs other devices out; the device list listens for this.
const SESSIONS_CHANGED = 'skinloom:sessions-changed'
const sessionsChanged = () => window.dispatchEvent(new Event(SESSIONS_CHANGED))

/** Shared submit bookkeeping: pending flag, success/failure message, field errors. */
function useSubmit() {
  const t = useTranslations('profile')
  const tf = useTranslations('fieldErrors')
  const [pending, setPending] = useState(false)
  const [done, setDone] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [fields, setFields] = useState<Record<string, string>>({})
  const run = async (action: () => Promise<unknown>, ok: string) => {
    setPending(true)
    setDone(null)
    setError(null)
    setFields({})
    try {
      await action()
      setDone(ok)
      return true
    } catch (err) {
      const p = problemOf(err)
      setFields(
        Object.fromEntries(
          Object.entries(p.fields).map(([k, v]) => {
            if (k === 'password') return [k, tf('wrongPassword')]
            if (k === 'newPassword') return [k, tf('password')]
            if (k === 'email' && v === 'already registered') return [k, tf('emailTaken')]
            return [k, tf.has(k) ? tf(k as 'email') : v]
          }),
        ),
      )
      if (p.status === 429) setError(t('errors.tooMany'))
      else if (p.status === 403) setError(p.detail ?? t('errors.failed'))
      else if (p.status !== 422) setError(t('errors.failed'))
      return false
    } finally {
      setPending(false)
    }
  }
  return { pending, done, error, fields, setFields, run }
}

/** The avatar: the head of a skin chosen in the closet, flat and as a cube. */
export function AvatarSection({ viewer }: { viewer: Viewer }) {
  const t = useTranslations('profile')
  const router = useRouter()
  const s = useSubmit()
  return (
    <Section title={t('avatar')} description={t('avatarHint')}>
      <div className="flex flex-wrap items-center gap-4">
        <Avatar userId={viewer.id} size={64} version={viewer.avatarTextureId} className="size-16 rounded-md bg-muted" />
        <Avatar userId={viewer.id} size={64} version={viewer.avatarTextureId} cube className="size-16" />
        <div className="flex flex-col gap-2">
          <Button asChild variant="outline" size="sm">
            <Link href="/user/closet">{t('chooseAvatar')}</Link>
          </Button>
          {viewer.avatarTextureId && (
            <Button
              variant="ghost"
              size="sm"
              disabled={s.pending}
              onClick={async () => {
                if (await s.run(() => setAvatar({ textureId: 0 }), t('avatarRemoved'))) router.refresh()
              }}
            >
              {t('removeAvatar')}
            </Button>
          )}
        </div>
      </div>
      <FormError message={s.error} />
      <Done message={s.done} />
    </Section>
  )
}

export function ProfileSection({ viewer, locales }: { viewer: Viewer; locales: LocaleInfo[] }) {
  const t = useTranslations('profile')
  const router = useRouter()
  const s = useSubmit()
  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    const locale = String(f.get('locale') ?? '')
    const ok = await s.run(() => updateMe({ nickname: String(f.get('nickname') ?? ''), locale }), t('saved'))
    if (ok) {
      // The preference also decides the language of mail; apply it to this browser now.
      if (locale) document.cookie = `locale=${encodeURIComponent(locale)}; path=/; max-age=31536000; samesite=lax`
      router.refresh()
    }
  }
  return (
    <Section title={t('profile')}>
      <form onSubmit={onSubmit} className="flex flex-col gap-4">
        <FormError message={s.error} />
        <Done message={s.done} />
        <Field id="nickname" label={t('nickname')} defaultValue={viewer.nickname} maxLength={32} required error={s.fields.nickname} />
        <div className="flex flex-col gap-2">
          <Label htmlFor="locale">{t('language')}</Label>
          <select id="locale" name="locale" defaultValue={viewer.locale} className="h-9 rounded-md border border-input bg-transparent px-3 text-sm">
            <option value="">{t('followBrowser')}</option>
            {locales.map((l) => (
              <option key={l.code} value={l.code}>
                {l.name}
              </option>
            ))}
          </select>
          <p className="text-xs text-muted-foreground">{t('languageHint')}</p>
        </div>
        <Button type="submit" className="self-start" disabled={s.pending}>
          {t('save')}
        </Button>
      </form>
    </Section>
  )
}

/** Change the password; an account created through a provider sets its first one here. */
export function PasswordSection({ viewer }: { viewer: Viewer }) {
  const t = useTranslations('profile')
  const tf = useTranslations('fieldErrors')
  const router = useRouter()
  const s = useSubmit()
  const first = !viewer.hasPassword
  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = e.currentTarget
    const f = new FormData(form)
    if (f.get('newPassword') !== f.get('confirm')) {
      s.setFields({ confirm: tf('passwordMismatch') })
      return
    }
    const ok = await s.run(
      () => changePassword({ currentPassword: String(f.get('password') ?? ''), newPassword: String(f.get('newPassword') ?? '') }),
      first ? t('passwordSet') : t('passwordChanged'),
    )
    if (ok) {
      form.reset()
      sessionsChanged()
      if (first) router.refresh()
    }
  }
  return (
    <Section title={first ? t('setPassword') : t('password')} description={first ? t('setPasswordHint') : t('passwordHint')}>
      <form onSubmit={onSubmit} className="flex flex-col gap-4">
        <FormError message={s.error} />
        <Done message={s.done} />
        {!first && (
          <Field id="password" label={t('currentPassword')} type="password" autoComplete="current-password" required error={s.fields.password} />
        )}
        <Field
          id="newPassword"
          label={t('newPassword')}
          type="password"
          autoComplete="new-password"
          minLength={8}
          maxLength={128}
          required
          error={s.fields.newPassword}
        />
        <Field id="confirm" label={t('confirmPassword')} type="password" autoComplete="new-password" required error={s.fields.confirm} />
        <Button type="submit" className="self-start" disabled={s.pending}>
          {first ? t('setPassword') : t('changePassword')}
        </Button>
      </form>
    </Section>
  )
}

export function EmailSection({ viewer, mail }: { viewer: Viewer; mail: boolean }) {
  const t = useTranslations('profile')
  const router = useRouter()
  const s = useSubmit()
  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = e.currentTarget
    const f = new FormData(form)
    const ok = await s.run(
      () => changeEmail({ email: String(f.get('email') ?? ''), password: String(f.get('emailPassword') ?? '') }),
      mail ? t('emailChangedMail') : t('emailChanged'),
    )
    if (ok) {
      form.reset()
      sessionsChanged()
      router.refresh()
    }
  }
  return (
    <Section title={t('email')} description={t('currentEmail', { email: viewer.email })}>
      <form onSubmit={onSubmit} className="flex flex-col gap-4">
        <FormError message={s.error} />
        <Done message={s.done} />
        <Field id="email" label={t('newEmail')} type="email" autoComplete="email" required error={s.fields.email} />
        {viewer.hasPassword && (
          <Field id="emailPassword" label={t('currentPassword')} type="password" autoComplete="current-password" required error={s.fields.password} />
        )}
        <p className="text-xs text-muted-foreground">{t('emailHint')}</p>
        <Button type="submit" className="self-start" disabled={s.pending}>
          {t('changeEmail')}
        </Button>
      </form>
    </Section>
  )
}

/** Game launchers (authlib-injector): the address to give them, how many are signed in, sign them all out. */
export function LaunchersSection() {
  const t = useTranslations('profile')
  const tl = useTranslations('launcher')
  const s = useSubmit()
  const [info, setInfo] = useState<YggdrasilTokens | null>(null)
  useEffect(() => {
    getYggdrasil().then(
      (r) => setInfo(r.data as YggdrasilTokens),
      () => setInfo(null),
    )
  }, [])
  if (!info) return null
  return (
    <Section title={t('launchers')} description={t('launchersHint')}>
      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <code className="rounded-md bg-muted px-3 py-2 font-mono text-sm">{info.apiUrl}</code>
          <CopyButton value={info.apiUrl} label={tl('copy')} copiedLabel={tl('copied')} />
        </div>
        <p className="text-sm text-muted-foreground">{t('launchersSignedIn', { count: info.tokens })}</p>
        <FormError message={s.error} />
        <Done message={s.done} />
        <Button
          variant="outline"
          className="self-start"
          disabled={s.pending || info.tokens === 0}
          onClick={async () => {
            if (await s.run(() => signOutLaunchers(), t('launchersSignedOut'))) setInfo({ ...info, tokens: 0 })
          }}
        >
          {t('launchersSignOut')}
        </Button>
      </div>
    </Section>
  )
}

export function SessionsSection() {
  const t = useTranslations('profile')
  const locale = useLocale()
  const [list, setList] = useState<SessionView[] | null>(null)
  const [failed, setFailed] = useState(false)
  const load = useCallback(async () => {
    try {
      setList((await listSessions()).data as SessionView[])
    } catch {
      setFailed(true)
    }
  }, [])
  useEffect(() => {
    void load()
    const reload = () => void load()
    window.addEventListener(SESSIONS_CHANGED, reload)
    return () => window.removeEventListener(SESSIONS_CHANGED, reload)
  }, [load])
  const when = (iso: string) => new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(iso))

  return (
    <Section title={t('devices')} description={t('devicesHint')}>
      {failed && <p className="text-sm text-destructive">{t('errors.failed')}</p>}
      {list === null && !failed && <p className="text-sm text-muted-foreground">…</p>}
      {list && (
        <div className="flex flex-col gap-3">
          <ul className="divide-y rounded-md border">
            {list.map((x) => (
              <li key={x.id} className="flex items-center justify-between gap-3 px-3 py-2 text-sm">
                <div className="min-w-0">
                  <div className="flex items-center gap-2 font-medium">
                    {describeUA(x.userAgent)}
                    {x.current && <Badge variant="secondary">{t('thisDevice')}</Badge>}
                  </div>
                  <div className="truncate text-xs text-muted-foreground">
                    {x.ip} · {t('lastSeen', { when: when(x.lastSeenAt) })}
                  </div>
                </div>
                {!x.current && (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={async () => {
                      await revokeSession(x.id).catch(() => undefined)
                      await load()
                    }}
                  >
                    {t('signOutDevice')}
                  </Button>
                )}
              </li>
            ))}
          </ul>
          {list.length > 1 && (
            <Button
              variant="outline"
              className="self-start"
              onClick={async () => {
                await revokeOtherSessions().catch(() => undefined)
                await load()
              }}
            >
              {t('signOutOthers')}
            </Button>
          )}
        </div>
      )}
    </Section>
  )
}

export function DeleteSection({ viewer }: { viewer: Viewer }) {
  const t = useTranslations('profile')
  const router = useRouter()
  const s = useSubmit()
  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    if (!window.confirm(t('deleteConfirm'))) return
    const ok = await s.run(() => deleteMe({ password: String(f.get('deletePassword') ?? '') }), t('deleted'))
    if (ok) {
      router.replace('/')
      router.refresh()
    }
  }
  return (
    <Section title={t('delete')} description={t('deleteHint')}>
      <form onSubmit={onSubmit} className="flex flex-col gap-4">
        <FormError message={s.error} />
        {viewer.hasPassword && (
          <Field id="deletePassword" label={t('currentPassword')} type="password" autoComplete="current-password" required error={s.fields.password} />
        )}
        <Button type="submit" variant="destructive" className="self-start" disabled={s.pending}>
          {t('deleteButton')}
        </Button>
      </form>
    </Section>
  )
}

/** Accounts at sign-in providers linked to this one: sign in with them, link more, unlink. */
export function SocialSection({ viewer, linked, error }: { viewer: Viewer; linked: boolean; error?: string }) {
  const t = useTranslations('social')
  const locale = useLocale()
  const s = useSubmit()
  const [info, setInfo] = useState<SocialLinks | null>(null)
  const load = useCallback(async () => {
    try {
      setInfo((await listSocial()).data as SocialLinks)
    } catch {
      setInfo(null)
    }
  }, [])
  useEffect(() => {
    void load()
  }, [load])
  if (!info || (info.links.length === 0 && info.providers.length === 0)) return null
  const linkedIds = new Set(info.links.map((l) => l.provider))
  const unlinked = info.providers.filter((p) => !linkedIds.has(p.id))
  const when = (iso: string) => new Intl.DateTimeFormat(locale, { dateStyle: 'medium' }).format(new Date(iso))
  // Without a password the last link to a provider the site still offers is the only
  // way in (links to removed providers have no providerType and can always go).
  const usable = info.links.filter((l) => l.providerType)
  const keepLast = !viewer.hasPassword && usable.length === 1

  return (
    <Section title={t('section.title')} description={t('section.hint')}>
      <div className="flex flex-col gap-3">
        <SocialError code={error} />
        {linked && <Done message={t('section.linked')} />}
        <FormError message={s.error} />
        {info.links.length > 0 && (
          <ul className="divide-y rounded-md border">
            {info.links.map((l) => (
              <li key={l.provider} className="flex items-center justify-between gap-3 px-3 py-2 text-sm">
                <div className="flex min-w-0 items-center gap-3">
                  <ProviderIcon type={l.providerType} className="size-5 shrink-0" />
                  <div className="min-w-0">
                    <div className="font-medium">{l.providerName || l.provider}</div>
                    <div className="truncate text-xs text-muted-foreground">
                      {l.name} · {t('section.since', { when: when(l.createdAt) })}
                    </div>
                  </div>
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={s.pending || (keepLast && Boolean(l.providerType))}
                  onClick={async () => {
                    if (!window.confirm(t('section.unlinkConfirm', { provider: l.providerName || l.provider }))) return
                    if (await s.run(() => unlinkSocial(l.provider), '')) await load()
                  }}
                >
                  {t('section.unlink')}
                </Button>
              </li>
            ))}
          </ul>
        )}
        {keepLast && <p className="text-xs text-muted-foreground">{t('section.keepLast')}</p>}
        {unlinked.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {unlinked.map((p) => (
              <Button
                key={p.id}
                variant="outline"
                size="sm"
                disabled={s.pending}
                onClick={() =>
                  void s.run(async () => {
                    const res = await linkSocial(p.id)
                    window.location.assign((res.data as SocialRedirect).url)
                  }, '')
                }
              >
                <ProviderIcon type={p.type} />
                {t('section.link', { name: p.name })}
              </Button>
            ))}
          </div>
        )}
      </div>
    </Section>
  )
}
