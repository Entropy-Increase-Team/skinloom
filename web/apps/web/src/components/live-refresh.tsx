'use client'

import { useRouter } from 'next/navigation'
import { useEffect, useRef } from 'react'

import { nextRefreshDelay } from '@/lib/refresh'

/**
 * Hot reload for open tabs: the backend announces a new config version over SSE
 * (/events) whenever settings, the theme or plugins change, and we re-render the
 * server components with fresh bootstrap data. No full page reload, no lost state.
 *
 * One EventSource per tab for its whole lifetime (it reconnects by itself). Refreshes
 * are spread over a short random delay so a big site's open tabs do not all hit the
 * server in the same instant.
 */
export function LiveRefresh({ version }: { version: number }) {
  const router = useRouter()
  const current = useRef(version)
  current.current = version
  const offline = version < 0

  useEffect(() => {
    if (offline) return // offline fallback: nothing to subscribe to
    let timer: ReturnType<typeof setTimeout> | undefined
    const es = new EventSource('/events')
    const onChange = (e: MessageEvent<string>) => {
      let next: number
      try {
        next = (JSON.parse(e.data) as { version: number }).version
      } catch {
        return // malformed event
      }
      if (next === current.current || timer) return
      timer = setTimeout(() => {
        timer = undefined
        router.refresh()
      }, nextRefreshDelay())
    }
    es.addEventListener('hello', onChange)
    es.addEventListener('config.changed', onChange)
    return () => {
      es.close()
      if (timer) clearTimeout(timer)
    }
  }, [router, offline])

  return null
}
