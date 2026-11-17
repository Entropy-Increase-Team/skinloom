'use client'

import { type SignInfo, type SignResult, signIn } from '@skinloom/api-client'
import { Button, toast } from '@skinloom/ui'
import { CalendarCheck, Check } from 'lucide-react'
import { useRouter } from 'next/navigation'
import { useFormatter, useTranslations } from 'next-intl'
import { useEffect, useState } from 'react'

import { problemOf } from '@/lib/problem'

/** Check-in: random score once a day (or every gapHours), as the site sets it. */
export function SignInCard({ sign }: { sign: SignInfo }) {
  const t = useTranslations('dashboard.sign')
  const format = useFormatter()
  const router = useRouter()
  const [next, setNext] = useState(sign.nextAt ?? null)
  const [pending, setPending] = useState(false)

  // The server only sends nextAt while it is in the future; enable the button when it comes.
  useEffect(() => setNext(sign.nextAt ?? null), [sign.nextAt])
  useEffect(() => {
    if (!next) return
    const wait = new Date(next).getTime() - Date.now()
    if (wait > 2 ** 31 - 1) return // setTimeout's limit; a reload will do
    const timer = setTimeout(() => setNext(null), Math.max(0, wait))
    return () => clearTimeout(timer)
  }, [next])

  async function onSign() {
    setPending(true)
    try {
      const res = (await signIn()).data as SignResult
      setNext(res.nextAt)
      toast.success(t('gained', { score: res.gained }))
      router.refresh()
    } catch (err) {
      const early = problemOf(err).status === 409
      toast.error(early ? t('tooEarly') : t('failed'))
      if (early) router.refresh()
    } finally {
      setPending(false)
    }
  }

  return (
    <section className="flex flex-col gap-4 rounded-xl border bg-card p-5" aria-labelledby="sign-title">
      <div className="flex flex-col gap-1">
        <h2 id="sign-title" className="flex items-center gap-2 text-sm font-medium">
          <CalendarCheck className="size-4 text-muted-foreground" aria-hidden />
          {t('title')}
        </h2>
        <p className="text-caption text-muted-foreground">
          {sign.min === sign.max ? t('fixed', { score: sign.min }) : t('range', { min: sign.min, max: sign.max })}
          {sign.daily ? t('daily') : t('gap', { hours: sign.gapHours })}
        </p>
      </div>
      {next ? (
        <div className="flex flex-col gap-1">
          <Button variant="outline" disabled>
            <Check />
            {t('done')}
          </Button>
          <p className="text-caption text-muted-foreground">{t('next', { time: format.dateTime(new Date(next), { dateStyle: 'medium', timeStyle: 'short' }) })}</p>
        </div>
      ) : (
        <Button loading={pending} onClick={() => void onSign()}>
          {t('button')}
        </Button>
      )}
    </section>
  )
}
