'use client'

import { type Answer, type CaptchaInfo, getCaptchaImage } from '@skinloom/api-client'
import { Button, Input, Label } from '@skinloom/ui'
import { RefreshCw } from 'lucide-react'
import { useTranslations } from 'next-intl'
import { useCallback, useEffect, useRef, useState } from 'react'

/** Widget providers: script URL (explicit rendering, onload callback) and global name. */
const widgets = {
  turnstile: { src: 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit', global: 'turnstile' },
  // recaptcha.net serves the same API and is reachable from mainland China.
  recaptcha: { src: 'https://www.recaptcha.net/recaptcha/api.js?render=explicit', global: 'grecaptcha' },
  hcaptcha: { src: 'https://js.hcaptcha.com/1/api.js?render=explicit&recaptchacompat=off', global: 'hcaptcha' },
} as const

type WidgetName = keyof typeof widgets

interface WidgetAPI {
  render(el: HTMLElement, opts: Record<string, unknown>): string | number
  reset(id?: string | number): void
  remove?(id: string | number): void
}

const loading: Partial<Record<WidgetName, Promise<WidgetAPI>>> = {}

/** Loads a provider's script once per page and resolves with its API. */
function loadWidget(name: WidgetName): Promise<WidgetAPI> {
  const w = window as unknown as Record<string, WidgetAPI | (() => void) | undefined>
  loading[name] ??= new Promise<WidgetAPI>((resolve, reject) => {
    const cb = `__skinloomCaptcha_${name}`
    w[cb] = () => resolve(w[widgets[name].global] as WidgetAPI)
    const s = document.createElement('script')
    s.src = `${widgets[name].src}&onload=${cb}`
    s.async = true
    s.onerror = () => {
      delete loading[name]
      reject(new Error(`cannot load ${name}`))
    }
    document.head.appendChild(s)
  })
  return loading[name]
}

/**
 * The captcha configured in captcha.provider. Reports the current answer through
 * onChange (null while unanswered); bump resetKey to get a fresh challenge, e.g. after
 * the server rejected the previous answer (each answer works only once).
 *
 * onChange must keep its identity across renders (a state setter): a new function
 * would load a new challenge on every render.
 */
export function Captcha({
  config,
  onChange,
  resetKey,
  error,
}: {
  config: CaptchaInfo
  onChange: (answer: Answer | null) => void
  resetKey: number
  error?: string
}) {
  if (config.provider === 'none') return null
  return (
    <div className="flex flex-col gap-2">
      {config.provider === 'image' ? (
        <ImageCaptcha onChange={onChange} resetKey={resetKey} />
      ) : (
        <WidgetCaptcha name={config.provider} siteKey={config.siteKey ?? ''} onChange={onChange} resetKey={resetKey} />
      )}
      {error && (
        <p role="alert" className="text-xs text-destructive">
          {error}
        </p>
      )}
    </div>
  )
}

function ImageCaptcha({ onChange, resetKey }: { onChange: (a: Answer | null) => void; resetKey: number }) {
  const t = useTranslations('captcha')
  const [challenge, setChallenge] = useState<{ id: string; image: string } | null>(null)
  const [value, setValue] = useState('')
  const [failed, setFailed] = useState(false)

  const refresh = useCallback(async () => {
    setValue('')
    onChange(null)
    try {
      const res = await getCaptchaImage()
      setChallenge(res.data as { id: string; image: string })
      setFailed(false)
    } catch {
      setFailed(true)
    }
  }, [onChange])

  useEffect(() => {
    void refresh()
  }, [refresh, resetKey])

  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor="captcha">{t('label')}</Label>
      <div className="flex items-center gap-2">
        <Input
          id="captcha"
          name="captcha"
          autoComplete="off"
          autoCapitalize="characters"
          spellCheck={false}
          maxLength={8}
          value={value}
          onChange={(e) => {
            setValue(e.target.value)
            onChange(challenge && e.target.value.trim() ? { id: challenge.id, value: e.target.value } : null)
          }}
          className="w-32 font-mono uppercase tracking-widest"
          required
        />
        {challenge ? (
          // a plain <img>: next/image adds nothing for a one-off data URL
          <img src={challenge.image} alt={t('alt')} width={180} height={64} className="h-10 w-auto rounded border" />
        ) : (
          <span className="text-xs text-muted-foreground">{failed ? t('loadFailed') : t('loading')}</span>
        )}
        <Button type="button" variant="ghost" size="icon" title={t('refresh')} aria-label={t('refresh')} onClick={() => void refresh()}>
          <RefreshCw />
        </Button>
      </div>
    </div>
  )
}

function WidgetCaptcha({
  name,
  siteKey,
  onChange,
  resetKey,
}: {
  name: WidgetName
  siteKey: string
  onChange: (a: Answer | null) => void
  resetKey: number
}) {
  const t = useTranslations('captcha')
  const el = useRef<HTMLDivElement>(null)
  const widget = useRef<{ api: WidgetAPI; id: string | number } | null>(null)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    let cancelled = false
    loadWidget(name)
      .then((api) => {
        if (cancelled || !el.current) return
        const id = api.render(el.current, {
          sitekey: siteKey,
          callback: (token: string) => onChange({ value: token }),
          'expired-callback': () => onChange(null),
          'error-callback': () => onChange(null),
        })
        widget.current = { api, id }
      })
      .catch(() => setFailed(true))
    return () => {
      cancelled = true
      const w = widget.current
      widget.current = null
      if (w?.api.remove) w.api.remove(w.id)
    }
  }, [name, siteKey, onChange])

  useEffect(() => {
    if (resetKey === 0 || !widget.current) return
    widget.current.api.reset(widget.current.id)
    onChange(null)
  }, [resetKey, onChange])

  return (
    <div>
      <div ref={el} />
      {failed && <p className="text-xs text-destructive">{t('loadFailed')}</p>}
    </div>
  )
}
