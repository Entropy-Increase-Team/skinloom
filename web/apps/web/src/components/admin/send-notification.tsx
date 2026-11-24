'use client'

import { sendNotification, type SendNotificationInputBodyTo } from '@skinloom/api-client'
import {
  Button,
  Field,
  FieldDescription,
  FieldLabel,
  Input,
  ResponsiveDialog,
  ResponsiveDialogClose,
  ResponsiveDialogContent,
  ResponsiveDialogDescription,
  ResponsiveDialogFooter,
  ResponsiveDialogHeader,
  ResponsiveDialogTitle,
  ResponsiveDialogTrigger,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Textarea,
  toast,
} from '@skinloom/ui'
import { Send } from 'lucide-react'
import { useTranslations } from 'next-intl'
import { type FormEvent, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { problemOf } from '@/lib/problem'

/** Sends a notification to everyone, to ordinary users, or to one user (by id or email): a button and its dialog. */
export function SendNotification() {
  const t = useTranslations('adminNotify')
  const tc = useTranslations('common')
  const [open, setOpen] = useState(false)
  const [to, setTo] = useState<SendNotificationInputBodyTo>('all')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const targets = (['all', 'users', 'user'] as const).map((v) => ({ value: v, label: t(v === 'all' ? 'toAll' : v === 'users' ? 'toUsers' : 'toUser') }))

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    const target = String(f.get('target') ?? '').trim()
    setPending(true)
    setError(null)
    try {
      await sendNotification({
        to,
        title: String(f.get('title') ?? ''),
        content: String(f.get('content') ?? ''),
        ...(to === 'user' ? (/^\d+$/.test(target) ? { userId: Number(target) } : { email: target }) : {}),
      })
      toast.success(to === 'user' ? t('sentOne') : t('sentAll'))
      setOpen(false)
      setTo('all')
    } catch (err) {
      const p = problemOf(err)
      setError(p.fields.userId ? t('errors.noUser') : p.fields.title ? t('errors.title') : p.fields.content ? t('errors.content') : t('errors.failed'))
    } finally {
      setPending(false)
    }
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        setError(null)
      }}
    >
      <ResponsiveDialogTrigger render={<Button variant="outline" />}>
        <Send />
        {t('title')}
      </ResponsiveDialogTrigger>
      <ResponsiveDialogContent className="md:max-w-lg">
        <form onSubmit={(e) => void onSubmit(e)} className="flex flex-col gap-5" noValidate>
          <ResponsiveDialogHeader>
            <ResponsiveDialogTitle>{t('title')}</ResponsiveDialogTitle>
            <ResponsiveDialogDescription>{t('intro')}</ResponsiveDialogDescription>
          </ResponsiveDialogHeader>
          <FormError message={error} />
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-[12rem_minmax(0,1fr)]">
            <Field>
              <FieldLabel htmlFor="notify-to">{t('to')}</FieldLabel>
              <Select items={targets} value={to} onValueChange={(v) => v && setTo(v as SendNotificationInputBodyTo)}>
                <SelectTrigger id="notify-to" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {targets.map((x) => (
                    <SelectItem key={x.value} value={x.value}>
                      {x.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            {to === 'user' && (
              <Field>
                <FieldLabel htmlFor="notify-target">{t('target')}</FieldLabel>
                <Input id="notify-target" name="target" required placeholder={t('targetHint')} maxLength={255} />
              </Field>
            )}
          </div>
          <Field>
            <FieldLabel htmlFor="notify-title">{t('titleField')}</FieldLabel>
            <Input id="notify-title" name="title" required maxLength={64} />
          </Field>
          <Field>
            <FieldLabel htmlFor="notify-content">{t('content')}</FieldLabel>
            <Textarea id="notify-content" name="content" rows={5} maxLength={10000} />
            <FieldDescription>{t('contentHint')}</FieldDescription>
          </Field>
          <ResponsiveDialogFooter>
            <ResponsiveDialogClose render={<Button type="button" variant="ghost" />}>{tc('cancel')}</ResponsiveDialogClose>
            <Button type="submit" loading={pending}>
              <Send />
              {t('send')}
            </Button>
          </ResponsiveDialogFooter>
        </form>
      </ResponsiveDialogContent>
    </ResponsiveDialog>
  )
}
