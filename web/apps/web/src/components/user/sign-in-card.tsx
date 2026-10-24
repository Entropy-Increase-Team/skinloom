'use client'

import { type SignInfo, type SignResult, signIn } from '@skinloom/api-client'
import { Button, Card, CardContent, CardHeader, CardTitle } from '@skinloom/ui'
import { CalendarCheck } from 'lucide-react'
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
  const [message, setMessage] = useState<{ error: boolean; text: string } | null>(null)

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
    setMessage(null)
    try {
      const res = (await signIn()).data as SignResult
      setNext(res.nextAt)
      setMessage({ error: false, text: t('gained', { score: res.gained }) })
      router.refresh()
    } catch (err) {
      const early = problemOf(err).status === 409
      setMessage({ error: true, text: early ? t('tooEarly') : t('failed') })
      if (early) router.refresh()
    } finally {
      setPending(false)
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <CalendarCheck className="size-4 text-primary" aria-hidden />
          {t('title')}
        </CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-sm">
        <p className="text-muted-foreground">
          {sign.min === sign.max ? t('fixed', { score: sign.min }) : t('range', { min: sign.min, max: sign.max })}
          {sign.daily ? t('daily') : t('gap', { hours: sign.gapHours })}
        </p>
        <Button className="self-start" disabled={pending || next !== null} onClick={() => void onSign()}>
          {pending ? t('signing') : next ? t('done') : t('button')}
        </Button>
        {next && <p className="text-muted-foreground">{t('next', { time: format.dateTime(new Date(next), { dateStyle: 'medium', timeStyle: 'short' }) })}</p>}
        {message && (
          <p role="status" className={message.error ? 'text-destructive' : 'text-primary'}>
            {message.text}
          </p>
        )}
      </CardContent>
    </Card>
  )
}
