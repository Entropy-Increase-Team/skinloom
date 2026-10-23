'use client'

import { Languages } from 'lucide-react'
import { useRouter } from 'next/navigation'
import { useTransition } from 'react'

import { useBootstrap } from './providers'

export function LocaleSwitch({ label }: { label: string }) {
  const { locale, locales } = useBootstrap()
  const router = useRouter()
  const [pending, startTransition] = useTransition()

  return (
    <label className="inline-flex items-center gap-1 text-sm text-muted-foreground">
      <Languages className="size-4" aria-hidden />
      <span className="sr-only">{label}</span>
      <select
        className="cursor-pointer rounded-md bg-transparent py-1 pr-1 text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
        value={locale}
        disabled={pending}
        onChange={(e) => {
          document.cookie = `locale=${encodeURIComponent(e.target.value)}; path=/; max-age=31536000; samesite=lax`
          startTransition(() => router.refresh())
        }}
      >
        {locales.map((l) => (
          <option key={l.code} value={l.code}>
            {l.name}
          </option>
        ))}
      </select>
    </label>
  )
}
