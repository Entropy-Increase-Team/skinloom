'use client'

import {
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  ToggleGroup,
  ToggleGroupItem,
} from '@skinloom/ui'
import { Languages } from 'lucide-react'
import { useRouter } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { useTheme } from 'next-themes'
import { useTransition } from 'react'

import { useBootstrap } from '@/components/providers'

/** The visitor's language: a cookie the backend reads (ADR 0004), then fresh server components. */
export function useLocaleSwitch() {
  const { locale, locales } = useBootstrap()
  const router = useRouter()
  const [pending, startTransition] = useTransition()
  return {
    locale,
    locales,
    pending,
    set(code: string) {
      if (code === locale) return
      document.cookie = `locale=${encodeURIComponent(code)}; path=/; max-age=31536000; samesite=lax`
      startTransition(() => router.refresh())
    },
  }
}

export const THEMES = ['light', 'dark', 'system'] as const

/** Language and colour mode, as radio groups inside a dropdown (header, account menu). */
export function SettingsMenuItems() {
  const t = useTranslations('shell')
  const lang = useLocaleSwitch()
  const { theme: activeTheme } = useBootstrap()
  const canSwitchColor = !activeTheme.colorScheme || activeTheme.colorScheme === 'both'
  const { theme = 'system', setTheme } = useTheme()
  return (
    <>
      <DropdownMenuGroup>
        <DropdownMenuLabel>{t('language')}</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={lang.locale} onValueChange={(v) => lang.set(String(v))}>
          {lang.locales.map((l) => (
            <DropdownMenuRadioItem key={l.code} value={l.code} disabled={lang.pending}>
              {l.name}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuGroup>
      {canSwitchColor && (
        <>
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            <DropdownMenuLabel>{t('appearance')}</DropdownMenuLabel>
            <DropdownMenuRadioGroup value={theme} onValueChange={(v) => setTheme(String(v))}>
              {THEMES.map((m) => (
                <DropdownMenuRadioItem key={m} value={m}>
                  {t(m)}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuGroup>
        </>
      )}
    </>
  )
}

/** The header's language-and-appearance button for visitors (design.md §9.2). */
export function SettingsMenu() {
  const t = useTranslations('shell')
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button variant="ghost" size="icon" aria-label={t('settings')} title={t('settings')} />}>
        <Languages />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-48">
        <SettingsMenuItems />
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/** The same choices as segmented controls, for the mobile menu sheet. */
export function SettingsToggles() {
  const t = useTranslations('shell')
  const lang = useLocaleSwitch()
  const { theme: activeTheme } = useBootstrap()
  const canSwitchColor = !activeTheme.colorScheme || activeTheme.colorScheme === 'both'
  const { theme = 'system', setTheme } = useTheme()
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-2">
        <span className="text-xs text-faint-foreground">{t('language')}</span>
        <ToggleGroup variant="outline" value={[lang.locale]} onValueChange={(v) => v[0] && lang.set(v[0])} className="w-full">
          {lang.locales.map((l) => (
            <ToggleGroupItem key={l.code} value={l.code} className="flex-1" disabled={lang.pending}>
              {l.name}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>
      {canSwitchColor && (
        <div className="flex flex-col gap-2">
          <span className="text-xs text-faint-foreground">{t('appearance')}</span>
          <ToggleGroup variant="outline" value={[theme]} onValueChange={(v) => v[0] && setTheme(v[0])} className="w-full">
            {THEMES.map((m) => (
              <ToggleGroupItem key={m} value={m} className="flex-1">
                {t(m)}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </div>
      )}
    </div>
  )
}
