'use client'

import { useEffect, useState } from 'react'

const UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
  ['year', 365 * 24 * 3600],
  ['month', 30 * 24 * 3600],
  ['week', 7 * 24 * 3600],
  ['day', 24 * 3600],
  ['hour', 3600],
  ['minute', 60],
]

/** "3 分钟前" / "in 2 days" for `date` seen from `now`; under a minute is "now". */
export function formatRelative(date: Date, now: Date, locale: string): string {
  const seconds = Math.round((date.getTime() - now.getTime()) / 1000)
  const fmt = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' })
  for (const [unit, size] of UNITS) {
    if (Math.abs(seconds) >= size) return fmt.format(Math.trunc(seconds / size), unit)
  }
  return fmt.format(0, 'second')
}

/**
 * A time in lists: relative ("3 分钟前"), with the full date and time on hover. The page
 * language decides the words. Before hydration it shows the full date, so server and
 * client agree.
 */
export function RelativeTime({ date, className }: { date: string | Date; className?: string }) {
  const d = typeof date === 'string' ? new Date(date) : date
  const [now, setNow] = useState<Date | null>(null)
  useEffect(() => {
    setNow(new Date())
    const id = setInterval(() => setNow(new Date()), 60_000)
    return () => clearInterval(id)
  }, [])
  const locale = typeof document !== 'undefined' ? document.documentElement.lang || undefined : undefined
  const full = d.toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' })
  return (
    <time data-slot="relative-time" dateTime={d.toISOString()} title={full} className={className} suppressHydrationWarning>
      {now ? formatRelative(d, now, locale ?? 'en') : full}
    </time>
  )
}
