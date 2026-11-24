'use client'

import { reportTexture, type ScoreRates } from '@skinloom/api-client'
import {
  Button,
  Field,
  FieldDescription,
  FieldLabel,
  ResponsiveDialog,
  ResponsiveDialogClose,
  ResponsiveDialogContent,
  ResponsiveDialogDescription,
  ResponsiveDialogFooter,
  ResponsiveDialogHeader,
  ResponsiveDialogTitle,
  ResponsiveDialogTrigger,
  Textarea,
  toast,
} from '@skinloom/ui'
import { Flag } from 'lucide-react'
import { useRouter } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { type FormEvent, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { problemOf } from '@/lib/problem'

const MAX_REASON = 500

/** Report a texture to the administrators, saying what it costs or earns. */
export function ReportButton({ textureId, rates }: { textureId: number; rates: ScoreRates }) {
  const t = useTranslations('texture.report')
  const tc = useTranslations('common')
  const router = useRouter()
  const [open, setOpen] = useState(false)
  const [reason, setReason] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  function sent() {
    setOpen(false)
    setReason('')
    toast.success(t('sent'), { action: { label: t('mine'), onClick: () => router.push('/user/reports') } })
  }

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!reason.trim()) return
    setPending(true)
    setError(null)
    try {
      await reportTexture(textureId, { reason: reason.trim() })
      sent()
    } catch (err) {
      const { status, detail, unverified } = problemOf(err)
      if (status === 409) sent() // reported already: same outcome for the user
      else
        setError(
          unverified
            ? tc('verifyFirst')
            : status === 402
              ? t('errors.score')
              : status === 403
                ? t('errors.own')
                : status === 429
                  ? t('errors.tooMany')
                  : status === 422 && detail
                    ? detail
                    : t('errors.failed'),
        )
    } finally {
      setPending(false)
    }
  }

  const mod = rates.reporterModification
  const note = [mod < 0 ? t('cost', { score: -mod }) : mod > 0 ? t('earn', { score: mod }) : null, rates.reporterReward > 0 ? t('reward', { score: rates.reporterReward }) : null]
    .filter(Boolean)
    .join(' ')

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (next) setError(null)
      }}
    >
      <ResponsiveDialogTrigger render={<Button variant="ghost" size="sm" className="self-start text-muted-foreground" />}>
        <Flag />
        {t('open')}
      </ResponsiveDialogTrigger>
      <ResponsiveDialogContent className="md:max-w-md">
        <form onSubmit={(e) => void submit(e)} className="flex flex-col gap-5" noValidate>
          <ResponsiveDialogHeader>
            <ResponsiveDialogTitle>{t('title')}</ResponsiveDialogTitle>
            <ResponsiveDialogDescription>{t('body')}</ResponsiveDialogDescription>
          </ResponsiveDialogHeader>
          <Field>
            <FieldLabel htmlFor="report-reason">{t('reason')}</FieldLabel>
            <Textarea
              id="report-reason"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              maxLength={MAX_REASON}
              rows={4}
              required
              placeholder={t('placeholder')}
            />
            {note && <FieldDescription>{note}</FieldDescription>}
          </Field>
          <FormError message={error} />
          <ResponsiveDialogFooter>
            <ResponsiveDialogClose render={<Button type="button" variant="ghost" />}>{tc('cancel')}</ResponsiveDialogClose>
            <Button type="submit" variant="destructive" loading={pending} disabled={!reason.trim()}>
              {t('submit')}
            </Button>
          </ResponsiveDialogFooter>
        </form>
      </ResponsiveDialogContent>
    </ResponsiveDialog>
  )
}
