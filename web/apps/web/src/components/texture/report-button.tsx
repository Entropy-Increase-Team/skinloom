'use client'

import { reportTexture, type ScoreRates } from '@skinloom/api-client'
import { Button } from '@skinloom/ui'
import { Flag } from 'lucide-react'
import Link from 'next/link'
import { useTranslations } from 'next-intl'
import { type FormEvent, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { problemOf } from '@/lib/problem'

const MAX_REASON = 500

/** Report a texture to the administrators, with what it costs or earns. */
export function ReportButton({ textureId, rates }: { textureId: number; rates: ScoreRates }) {
  const t = useTranslations('texture.report')
  const [open, setOpen] = useState(false)
  const [reason, setReason] = useState('')
  const [pending, setPending] = useState(false)
  const [sent, setSent] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!reason.trim()) return
    setPending(true)
    setError(null)
    try {
      await reportTexture(textureId, { reason: reason.trim() })
      setSent(true)
    } catch (err) {
      const status = problemOf(err).status
      if (status === 409) setSent(true) // reported already: same outcome for the user
      else
        setError(
          status === 402 ? t('errors.score') : status === 403 ? t('errors.own') : status === 429 ? t('errors.tooMany') : t('errors.failed'),
        )
    } finally {
      setPending(false)
    }
  }

  if (sent) {
    return (
      <p role="status" className="text-sm text-muted-foreground">
        {t('sent')}{' '}
        <Link href="/user/reports" className="text-primary hover:underline">
          {t('mine')}
        </Link>
      </p>
    )
  }
  if (!open) {
    return (
      <Button variant="ghost" size="sm" className="self-start text-muted-foreground" onClick={() => setOpen(true)}>
        <Flag />
        {t('open')}
      </Button>
    )
  }
  const mod = rates.reporterModification
  return (
    <form onSubmit={submit} className="flex flex-col gap-2 border-t pt-4">
      <label htmlFor="report-reason" className="text-sm font-medium">
        {t('reason')}
      </label>
      <textarea
        id="report-reason"
        value={reason}
        onChange={(e) => setReason(e.target.value)}
        maxLength={MAX_REASON}
        rows={3}
        required
        placeholder={t('placeholder')}
        className="rounded-md border border-input bg-transparent px-3 py-2 text-sm"
      />
      <p className="text-xs text-muted-foreground">
        {mod < 0 ? t('cost', { score: -mod }) : mod > 0 ? t('earn', { score: mod }) : null}
        {rates.reporterReward > 0 ? ` ${t('reward', { score: rates.reporterReward })}` : null}
      </p>
      <FormError message={error} />
      <div className="flex gap-2">
        <Button type="submit" size="sm" variant="destructive" disabled={pending || !reason.trim()}>
          {t('submit')}
        </Button>
        <Button type="button" size="sm" variant="ghost" onClick={() => setOpen(false)}>
          {t('cancel')}
        </Button>
      </div>
    </form>
  )
}
