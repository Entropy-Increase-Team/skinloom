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
import {
  Badge,
  Button,
  Checkbox,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  Field,
  FieldDescription,
  FieldLabel,
  Input,
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  Label,
  RelativeTime,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  Skeleton,
  type Status,
  StatusDot,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  toast,
  useConfirm,
  UserAvatar,
} from '@skinloom/ui'
import { MoreHorizontal, Pencil, Search, Trash2, Users } from 'lucide-react'
import Link from 'next/link'
import { useTranslations } from 'next-intl'
import { type FormEvent, useCallback, useEffect, useRef, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { problemOf } from '@/lib/problem'

import { FilterSelect } from './filter-select'

const ROLES = ['user', 'admin', 'superadmin'] as const
const STATUSES = ['active', 'banned', 'deleted'] as const
const DOT: Record<AdminUserView['status'], Status> = { active: 'ok', banned: 'error', deleted: 'off' }

export function UsersTable({ viewerId, superadmin }: { viewerId: number; superadmin: boolean }) {
  const t = useTranslations('admin')
  const confirm = useConfirm()
  const [filter, setFilter] = useState<ListUsersParams>({})
  const [users, setUsers] = useState<AdminUserView[] | null>(null)
  const [next, setNext] = useState(0)
  const [loading, setLoading] = useState(false)
  const [editing, setEditing] = useState<AdminUserView | null>(null)
  const [error, setError] = useState<string | null>(null)
  const latest = useRef(0)

  const load = useCallback(
    async (before?: number) => {
      const seq = ++latest.current
      setLoading(true)
      setError(null)
      try {
        const page = (await listUsers({ ...filter, limit: 30, ...(before ? { before } : {}) })).data as UserPage
        if (seq !== latest.current) return
        setUsers((prev) => (before && prev ? [...prev, ...page.users] : page.users))
        setNext(page.next)
      } catch {
        if (seq === latest.current) setError(t('errors.load'))
      } finally {
        if (seq === latest.current) setLoading(false)
      }
    },
    [filter, t],
  )
  useEffect(() => {
    void load()
  }, [load])

  const changed = (u: AdminUserView, updated: AdminUserView | null) =>
    setUsers((list) => list && (updated ? list.map((x) => (x.id === updated.id ? updated : x)) : list.filter((x) => x.id !== u.id)))

  async function remove(u: AdminUserView) {
    const ok = await confirm({
      title: t('deleteTitle', { name: u.nickname }),
      description: t('deleteConfirm', { email: u.email }),
      confirmLabel: t('deleteUser'),
      requireText: u.email,
    })
    if (!ok) return
    try {
      await deleteUser(u.id)
      changed(u, null)
      toast.success(t('deleted', { name: u.nickname }))
    } catch (err) {
      toast.error(problemOf(err).detail ?? t('errors.save'))
    }
  }

  function onSearch(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const q = String(new FormData(e.currentTarget).get('q') ?? '').trim()
    setFilter((f) => ({ ...f, q: q || undefined }))
  }

  // Administrators manage ordinary users; super administrators manage everyone.
  const manageable = (u: AdminUserView) => superadmin || u.role === 'user'

  const menu = (u: AdminUserView) => (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button variant="ghost" size="icon-sm" aria-label={t('menu', { name: u.nickname })} title={t('menu', { name: u.nickname })} />}>
        <MoreHorizontal />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-44">
        {manageable(u) && (
          <DropdownMenuItem onClick={() => setEditing(u)}>
            <Pencil />
            {t('edit')}
          </DropdownMenuItem>
        )}
        <DropdownMenuItem render={<Link href={`/admin/players?user=${u.id}`} />}>
          <Users />
          {t('playersOf')}
        </DropdownMenuItem>
        {manageable(u) && u.id !== viewerId && u.role !== 'superadmin' && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onClick={() => void remove(u)}>
              <Trash2 />
              {t('deleteUser')}
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )

  const who = (u: AdminUserView) => (
    <div className="flex min-w-0 items-center gap-2.5">
      <UserAvatar of={{ user: u.id }} size={28} />
      <div className="flex min-w-0 flex-col">
        <span className="truncate font-medium">{u.nickname}</span>
        <span className="flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground">
          <span className="truncate">{u.email}</span>
          {!u.emailVerified && (
            <Badge variant="outline" className="h-4 px-1 text-[0.6875rem]">
              {t('unverified')}
            </Badge>
          )}
        </span>
      </div>
    </div>
  )

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <form onSubmit={onSearch} role="search" className="w-full sm:w-72">
          <InputGroup>
            <InputGroupAddon>
              <Search aria-hidden />
            </InputGroupAddon>
            <InputGroupInput type="search" name="q" placeholder={t('searchHint')} aria-label={t('search')} />
          </InputGroup>
        </form>
        <FilterSelect
          label={t('role')}
          all={t('allRoles')}
          value={filter.role ?? ''}
          onChange={(v) => setFilter((f) => ({ ...f, role: (v || undefined) as ListUsersParams['role'] }))}
          options={ROLES.map((r) => ({ value: r, label: t(`roles.${r}`) }))}
        />
        <FilterSelect
          label={t('status')}
          all={t('allStatuses')}
          value={filter.status ?? ''}
          onChange={(v) => setFilter((f) => ({ ...f, status: (v || undefined) as ListUsersParams['status'] }))}
          options={STATUSES.map((s) => ({ value: s, label: t(`statuses.${s}`) }))}
        />
      </div>
      <FormError message={error} />

      {users === null && !error && <Skeleton className="h-64 rounded-xl" />}
      {users && (
        <>
          {/* Phones: one card per user. */}
          <ul className="flex flex-col divide-y rounded-xl border bg-card md:hidden">
            {users.map((u) => (
              <li key={u.id} className="flex items-center gap-3 p-3">
                <div className="flex min-w-0 flex-1 flex-col gap-1.5">
                  {who(u)}
                  <div className="flex flex-wrap items-center gap-x-3 gap-y-1 pl-[2.375rem]">
                    <StatusDot status={DOT[u.status]}>{t(`statuses.${u.status}`)}</StatusDot>
                    {u.role !== 'user' && <Badge variant="secondary">{t(`roles.${u.role}`)}</Badge>}
                    <span className="text-caption text-muted-foreground tabular-nums">
                      {t('score')} {u.score}
                    </span>
                  </div>
                </div>
                {menu(u)}
              </li>
            ))}
            {users.length === 0 && <li className="p-6 text-center text-sm text-muted-foreground">{t('empty')}</li>}
          </ul>
          <div className="hidden overflow-hidden rounded-xl border bg-card md:block">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-14 pl-4">ID</TableHead>
                  <TableHead>{t('user')}</TableHead>
                  <TableHead>{t('role')}</TableHead>
                  <TableHead>{t('status')}</TableHead>
                  <TableHead className="text-right">{t('score')}</TableHead>
                  <TableHead>{t('registered')}</TableHead>
                  <TableHead>{t('lastLogin')}</TableHead>
                  <TableHead className="w-12" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {users.map((u) => (
                  <TableRow key={u.id}>
                    <TableCell className="pl-4 font-mono text-caption text-muted-foreground">{u.id}</TableCell>
                    <TableCell className="max-w-72">{who(u)}</TableCell>
                    <TableCell>{u.role === 'user' ? <span className="text-muted-foreground">{t('roles.user')}</span> : <Badge variant="secondary">{t(`roles.${u.role}`)}</Badge>}</TableCell>
                    <TableCell>
                      <StatusDot status={DOT[u.status]}>{t(`statuses.${u.status}`)}</StatusDot>
                    </TableCell>
                    <TableCell className="text-right tabular-nums">{u.score}</TableCell>
                    <TableCell className="text-caption">
                      <RelativeTime date={u.createdAt} />
                      <div className="font-mono text-faint-foreground">{u.registerIp}</div>
                    </TableCell>
                    <TableCell className="text-caption">{u.lastLoginAt ? <RelativeTime date={u.lastLoginAt} /> : '—'}</TableCell>
                    <TableCell className="pr-3 text-right">{menu(u)}</TableCell>
                  </TableRow>
                ))}
                {users.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={8} className="py-10 text-center text-muted-foreground">
                      {t('empty')}
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </div>
        </>
      )}
      {next > 0 && (
        <Button variant="outline" className="self-center" loading={loading} onClick={() => void load(next)}>
          {t('more')}
        </Button>
      )}

      <Sheet open={editing !== null} onOpenChange={(open) => !open && setEditing(null)}>
        <SheetContent className="w-full overflow-y-auto sm:max-w-md">
          {editing && (
            <EditUser
              key={editing.id}
              u={editing}
              self={editing.id === viewerId}
              superadmin={superadmin}
              onChanged={(updated) => {
                changed(editing, updated)
                setEditing(null)
                if (updated) toast.success(t('saved'))
              }}
            />
          )}
        </SheetContent>
      </Sheet>
    </div>
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
  const [role, setRole] = useState<string>(u.role)
  const [status, setStatus] = useState<string>(u.status)
  // Nobody has verified a new address: changing it clears the box (the admin may tick it again).
  const [verified, setVerified] = useState(u.emailVerified)
  const roles = ROLES.map((r) => ({ value: r, label: t(`roles.${r}`) }))
  // Only sent when changed: picking "active" restores a deleted account.
  const statuses = (u.status === 'deleted' ? STATUSES : STATUSES.filter((s) => s !== 'deleted')).map((s) => ({ value: s, label: t(`statuses.${s}`) }))

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    const body: UpdateUserInputBody = {}
    const str = (k: string) => String(f.get(k) ?? '')
    if (str('nickname') !== u.nickname) body.nickname = str('nickname')
    if (str('email') !== u.email) body.email = str('email')
    if (superadmin && !self && role !== u.role) body.role = role as UpdateUserInputBody['role']
    if (!self && status !== u.status) body.status = status as UpdateUserInputBody['status']
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
      const message = (f: string) => (f === 'email' && p.fields[f] === 'already registered' ? tf('emailTaken') : tf.has(f) ? tf(f as 'email') : p.fields[f])
      setError(field ? `${t(`fields.${field}` as 'fields.email')}: ${message(field)}` : (p.detail ?? t('errors.save')))
    } finally {
      setPending(false)
    }
  }

  return (
    <form onSubmit={(e) => void onSubmit(e)} className="flex min-h-full flex-col">
      <SheetHeader>
        <SheetTitle>{t('editTitle', { name: u.nickname })}</SheetTitle>
        <SheetDescription>{t('saveHint')}</SheetDescription>
      </SheetHeader>
      <div className="flex flex-1 flex-col gap-4 px-4">
        <FormError message={error} />
        <Field>
          <FieldLabel htmlFor="u-nickname">{t('fields.nickname')}</FieldLabel>
          <Input id="u-nickname" name="nickname" defaultValue={u.nickname} maxLength={32} />
        </Field>
        <Field>
          <FieldLabel htmlFor="u-email">{t('fields.email')}</FieldLabel>
          <Input
            id="u-email"
            name="email"
            type="email"
            defaultValue={u.email}
            onChange={(e) => {
              if (e.target.value.trim().toLowerCase() !== u.email.toLowerCase()) setVerified(false)
            }}
          />
        </Field>
        <Label className="w-fit gap-2 font-normal">
          <Checkbox checked={verified} onCheckedChange={setVerified} />
          {t('fields.emailVerified')}
        </Label>
        <div className="grid grid-cols-2 gap-4">
          {superadmin && !self && (
            <Field>
              <FieldLabel htmlFor="u-role">{t('fields.role')}</FieldLabel>
              <Select items={roles} value={role} onValueChange={(v) => v && setRole(String(v))}>
                <SelectTrigger id="u-role" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {roles.map((r) => (
                    <SelectItem key={r.value} value={r.value}>
                      {r.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          )}
          {!self && (
            <Field>
              <FieldLabel htmlFor="u-status">{t('fields.status')}</FieldLabel>
              <Select items={statuses} value={status} onValueChange={(v) => v && setStatus(String(v))}>
                <SelectTrigger id="u-status" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {statuses.map((s) => (
                    <SelectItem key={s.value} value={s.value}>
                      {s.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          )}
          <Field>
            <FieldLabel htmlFor="u-score">{t('fields.score')}</FieldLabel>
            <Input id="u-score" name="score" type="number" min={0} defaultValue={u.score} />
          </Field>
        </div>
        <Field>
          <FieldLabel htmlFor="u-password">{t('fields.password')}</FieldLabel>
          <Input id="u-password" name="password" type="password" autoComplete="new-password" />
          <FieldDescription>{t('passwordHint')}</FieldDescription>
        </Field>
      </div>
      <SheetFooter className="flex-row justify-end">
        <Button type="submit" loading={pending}>
          {t('save')}
        </Button>
      </SheetFooter>
    </form>
  )
}
