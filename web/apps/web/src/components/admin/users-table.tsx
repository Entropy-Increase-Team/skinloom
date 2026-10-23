'use client'

import {
  type AdminUserView,
  deleteUser,
  type ListUsersParams,
  listUsers,
  type UpdateUserInputBody,
  type UserPage,
  updateUser,
} from '@skinloom/api-client'
import { Badge, Button, Input, Label } from '@skinloom/ui'
import { useLocale, useTranslations } from 'next-intl'
import { type FormEvent, useCallback, useEffect, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { problemOf } from '@/lib/problem'

const selectClass = 'h-9 rounded-md border border-input bg-transparent px-2 text-sm'

export function UsersTable({ viewerId, superadmin }: { viewerId: number; superadmin: boolean }) {
  const t = useTranslations('admin')
  const locale = useLocale()
  const [filter, setFilter] = useState<ListUsersParams>({})
  const [users, setUsers] = useState<AdminUserView[]>([])
  const [next, setNext] = useState(0)
  const [loading, setLoading] = useState(false)
  const [editing, setEditing] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(
    async (before?: number) => {
      setLoading(true)
      setError(null)
      try {
        const page = (await listUsers({ ...filter, limit: 30, ...(before ? { before } : {}) })).data as UserPage
        setUsers((prev) => (before ? [...prev, ...page.users] : page.users))
        setNext(page.next)
      } catch {
        setError(t('errors.load'))
      } finally {
        setLoading(false)
      }
    },
    [filter, t],
  )
  useEffect(() => {
    void load()
  }, [load])

  const date = (iso?: string) =>
    iso ? new Intl.DateTimeFormat(locale, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(iso)) : '—'

  function onSearch(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    const pick = (k: string) => String(f.get(k) ?? '') || undefined
    setFilter({ q: pick('q'), role: pick('role') as ListUsersParams['role'], status: pick('status') as ListUsersParams['status'] })
  }

  return (
    <div className="flex flex-col gap-4">
      <form onSubmit={onSearch} className="flex flex-wrap items-end gap-2">
        <div className="flex flex-col gap-1">
          <Label htmlFor="q">{t('search')}</Label>
          <Input id="q" name="q" placeholder={t('searchHint')} className="w-64" />
        </div>
        <select name="role" className={selectClass} aria-label={t('role')} defaultValue="">
          <option value="">{t('allRoles')}</option>
          {(['user', 'admin', 'superadmin'] as const).map((r) => (
            <option key={r} value={r}>
              {t(`roles.${r}`)}
            </option>
          ))}
        </select>
        <select name="status" className={selectClass} aria-label={t('status')} defaultValue="">
          <option value="">{t('allStatuses')}</option>
          {(['active', 'banned', 'deleted'] as const).map((s) => (
            <option key={s} value={s}>
              {t(`statuses.${s}`)}
            </option>
          ))}
        </select>
        <Button type="submit" variant="outline">
          {t('search')}
        </Button>
      </form>
      <FormError message={error} />

      <div className="overflow-x-auto rounded-md border">
        <table className="w-full text-sm">
          <thead className="bg-muted/50 text-left text-xs text-muted-foreground">
            <tr>
              <th className="px-3 py-2">ID</th>
              <th className="px-3 py-2">{t('user')}</th>
              <th className="px-3 py-2">{t('role')}</th>
              <th className="px-3 py-2">{t('status')}</th>
              <th className="px-3 py-2 text-right">{t('score')}</th>
              <th className="px-3 py-2">{t('registered')}</th>
              <th className="px-3 py-2">{t('lastLogin')}</th>
              <th className="px-3 py-2" />
            </tr>
          </thead>
          <tbody className="divide-y">
            {users.map((u) => (
              <UserRow
                key={u.id}
                u={u}
                self={u.id === viewerId}
                superadmin={superadmin}
                editing={editing === u.id}
                onEdit={() => setEditing(editing === u.id ? null : u.id)}
                onChanged={(updated) => {
                  setUsers((list) => (updated ? list.map((x) => (x.id === updated.id ? updated : x)) : list.filter((x) => x.id !== u.id)))
                  setEditing(null)
                }}
                date={date}
              />
            ))}
            {users.length === 0 && !loading && (
              <tr>
                <td colSpan={8} className="px-3 py-6 text-center text-muted-foreground">
                  {t('empty')}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {next > 0 && (
        <Button variant="outline" className="self-center" disabled={loading} onClick={() => void load(next)}>
          {t('more')}
        </Button>
      )}
    </div>
  )
}

function UserRow({
  u,
  self,
  superadmin,
  editing,
  onEdit,
  onChanged,
  date,
}: {
  u: AdminUserView
  self: boolean
  superadmin: boolean
  editing: boolean
  onEdit: () => void
  onChanged: (updated: AdminUserView | null) => void
  date: (iso?: string) => string
}) {
  const t = useTranslations('admin')
  // Administrators manage ordinary users; super administrators manage everyone.
  const manageable = superadmin || u.role === 'user'
  return (
    <>
      <tr className={u.status !== 'active' ? 'text-muted-foreground' : undefined}>
        <td className="px-3 py-2 font-mono text-xs">{u.id}</td>
        <td className="px-3 py-2">
          <div className="font-medium text-foreground">{u.nickname}</div>
          <div className="text-xs">
            {u.email} {!u.emailVerified && <span>· {t('unverified')}</span>}
          </div>
        </td>
        <td className="px-3 py-2">{u.role === 'user' ? t('roles.user') : <Badge variant="secondary">{t(`roles.${u.role}`)}</Badge>}</td>
        <td className="px-3 py-2">{u.status === 'active' ? t('statuses.active') : <Badge variant="destructive">{t(`statuses.${u.status}`)}</Badge>}</td>
        <td className="px-3 py-2 text-right tabular-nums">{u.score}</td>
        <td className="px-3 py-2 text-xs">
          {date(u.createdAt)}
          <div className="text-muted-foreground">{u.registerIp}</div>
        </td>
        <td className="px-3 py-2 text-xs">{date(u.lastLoginAt)}</td>
        <td className="px-3 py-2 text-right">
          {manageable && (
            <Button variant="ghost" size="sm" onClick={onEdit}>
              {editing ? t('close') : t('edit')}
            </Button>
          )}
        </td>
      </tr>
      {editing && (
        <tr>
          <td colSpan={8} className="bg-muted/30 px-3 py-4">
            <EditUser u={u} self={self} superadmin={superadmin} onChanged={onChanged} />
          </td>
        </tr>
      )}
    </>
  )
}

function EditUser({
  u,
  self,
  superadmin,
  onChanged,
}: {
  u: AdminUserView
  self: boolean
  superadmin: boolean
  onChanged: (updated: AdminUserView | null) => void
}) {
  const t = useTranslations('admin')
  const tf = useTranslations('fieldErrors')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // Nobody has verified a new address: changing it clears the box (the admin may tick it again).
  const [verified, setVerified] = useState(u.emailVerified)

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    const body: UpdateUserInputBody = {}
    const str = (k: string) => String(f.get(k) ?? '')
    if (str('nickname') !== u.nickname) body.nickname = str('nickname')
    if (str('email') !== u.email) body.email = str('email')
    if (f.has('role') && str('role') !== u.role) body.role = str('role') as UpdateUserInputBody['role']
    if (f.has('status') && str('status') !== u.status) body.status = str('status') as UpdateUserInputBody['status']
    if (Number(str('score')) !== u.score) body.score = Number(str('score'))
    if (body.email !== undefined || verified !== u.emailVerified) body.emailVerified = verified
    if (str('password')) body.password = str('password')
    if (Object.keys(body).length === 0) return onChanged(u)
    setPending(true)
    setError(null)
    try {
      onChanged((await updateUser(u.id, body)).data as AdminUserView)
    } catch (err) {
      const p = problemOf(err)
      const field = Object.keys(p.fields)[0]
      const message = (f: string) =>
        f === 'email' && p.fields[f] === 'already registered' ? tf('emailTaken') : tf.has(f) ? tf(f as 'email') : p.fields[f]
      setError(field ? `${t(`fields.${field}` as 'fields.email')}: ${message(field)}` : (p.detail ?? t('errors.save')))
    } finally {
      setPending(false)
    }
  }

  async function onDelete() {
    if (!window.confirm(t('deleteConfirm', { email: u.email }))) return
    try {
      await deleteUser(u.id)
      onChanged(null)
    } catch (err) {
      setError(problemOf(err).detail ?? t('errors.save'))
    }
  }

  return (
    <form onSubmit={onSubmit} className="grid gap-3 sm:grid-cols-3">
      <div className="sm:col-span-3">
        <FormError message={error} />
      </div>
      <label className="flex flex-col gap-1 text-xs">
        {t('fields.nickname')}
        <Input name="nickname" defaultValue={u.nickname} maxLength={32} />
      </label>
      <label className="flex flex-col gap-1 text-xs">
        {t('fields.email')}
        <Input
          name="email"
          type="email"
          defaultValue={u.email}
          onChange={(e) => {
            if (e.target.value.trim().toLowerCase() !== u.email.toLowerCase()) setVerified(false)
          }}
        />
      </label>
      <label className="flex flex-col gap-1 text-xs">
        {t('fields.score')}
        <Input name="score" type="number" min={0} defaultValue={u.score} />
      </label>
      {superadmin && !self && (
        <label className="flex flex-col gap-1 text-xs">
          {t('fields.role')}
          <select name="role" defaultValue={u.role} className={selectClass}>
            {(['user', 'admin', 'superadmin'] as const).map((r) => (
              <option key={r} value={r}>
                {t(`roles.${r}`)}
              </option>
            ))}
          </select>
        </label>
      )}
      {!self && (
        <label className="flex flex-col gap-1 text-xs">
          {t('fields.status')}
          <select name="status" defaultValue={u.status} className={selectClass}>
            {/* Only sent when changed: picking "active" restores a deleted account. */}
            {u.status === 'deleted' && <option value="deleted">{t('statuses.deleted')}</option>}
            <option value="active">{t('statuses.active')}</option>
            <option value="banned">{t('statuses.banned')}</option>
          </select>
        </label>
      )}
      <label className="flex flex-col gap-1 text-xs">
        {t('fields.password')}
        <Input name="password" type="password" autoComplete="new-password" placeholder={t('passwordHint')} />
      </label>
      <label className="flex items-center gap-2 text-xs">
        <input type="checkbox" checked={verified} onChange={(e) => setVerified(e.target.checked)} className="size-4" />
        {t('fields.emailVerified')}
      </label>
      <div className="flex items-center gap-2 sm:col-span-3">
        <Button type="submit" size="sm" disabled={pending}>
          {t('save')}
        </Button>
        {!self && u.role !== 'superadmin' && (
          <Button type="button" size="sm" variant="destructive" onClick={onDelete}>
            {t('delete')}
          </Button>
        )}
        <span className="text-xs text-muted-foreground">{t('saveHint')}</span>
      </div>
    </form>
  )
}
