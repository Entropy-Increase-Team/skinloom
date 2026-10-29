'use client'

import { sendNotification, type SendNotificationInputBodyTo } from '@skinloom/api-client'
import { Button, Card, CardContent, CardDescription, CardHeader, CardTitle, Input, Label } from '@skinloom/ui'
import { useTranslations } from 'next-intl'
import { type FormEvent, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { problemOf } from '@/lib/problem'

const selectClass = 'h-9 rounded-md border border-input bg-transparent px-2 text-sm'

/** Sends a notification to everyone, to ordinary users, or to one user (by id or email). */
export function SendNotification() {
  const t = useTranslations('adminNotify')
  const [to, setTo] = useState<SendNotificationInputBodyTo>('all')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [sent, setSent] = useState<string | null>(null)

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = e.currentTarget
    const f = new FormData(form)
    const target = String(f.get('target') ?? '').trim()
    setPending(true)
    setError(null)
    setSent(null)
    try {
      await sendNotification({
        to,
        title: String(f.get('title') ?? ''),
        content: String(f.get('content') ?? ''),
        ...(to === 'user' ? (/^\d+$/.test(target) ? { userId: Number(target) } : { email: target }) : {}),
      })
      setSent(to === 'user' ? t('sentOne') : t('sentAll'))
      form.reset()
      setTo('all')
    } catch (err) {
      const p = problemOf(err)
      setError(p.fields.userId ? t('errors.noUser') : p.fields.title ? t('errors.title') : p.fields.content ? t('errors.content') : t('errors.failed'))
    } finally {
      setPending(false)
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('title')}</CardTitle>
        <CardDescription>{t('intro')}</CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={onSubmit} className="flex flex-col gap-3">
          <div className="flex flex-wrap items-end gap-2">
            <div className="flex flex-col gap-1">
              <Label htmlFor="notify-to">{t('to')}</Label>
              <select id="notify-to" value={to} onChange={(e) => setTo(e.target.value as SendNotificationInputBodyTo)} className={selectClass}>
                <option value="all">{t('toAll')}</option>
                <option value="users">{t('toUsers')}</option>
                <option value="user">{t('toUser')}</option>
              </select>
            </div>
            {to === 'user' && (
              <div className="flex flex-1 flex-col gap-1">
                <Label htmlFor="notify-target">{t('target')}</Label>
                <Input id="notify-target" name="target" required placeholder={t('targetHint')} maxLength={255} />
              </div>
            )}
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="notify-title">{t('titleField')}</Label>
            <Input id="notify-title" name="title" required maxLength={64} />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="notify-content">{t('content')}</Label>
            <textarea id="notify-content" name="content" rows={4} maxLength={10000} className="rounded-md border border-input bg-transparent px-3 py-2 text-sm" />
            <p className="text-xs text-muted-foreground">{t('contentHint')}</p>
          </div>
          <FormError message={error} />
          {sent && (
            <p role="status" className="text-sm text-primary">
              {sent}
            </p>
          )}
          <Button type="submit" className="self-start" disabled={pending}>
            {t('send')}
          </Button>
        </form>
      </CardContent>
    </Card>
  )
}
