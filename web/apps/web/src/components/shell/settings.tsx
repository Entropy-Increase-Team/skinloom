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
import { Languages, Monitor, Moon, Sun } from 'lucide-react'
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

/** The header's language button for visitors. */
export function LanguageMenu() {
  const t = useTranslations('shell')
  const lang = useLocaleSwitch()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button variant="ghost" size="icon" aria-label={t('language')} title={t('language')} />}>
        <Languages />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-44">
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
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/** The header's appearance button for visitors. */
export function AppearanceMenu() {
  const t = useTranslations('shell')
  const { theme: activeTheme } = useBootstrap()
  const canSwitchColor = !activeTheme.colorScheme || activeTheme.colorScheme === 'both'
  const { theme = 'system', setTheme } = useTheme()
  if (!canSwitchColor) return null
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button variant="ghost" size="icon" aria-label={t('appearance')} title={t('appearance')} />}>
        <Sun className="dark:hidden" />
        <Moon className="hidden dark:block" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-40">
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
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/** Combined language and appearance dropdown for visitors (design.md §9.2). */
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

const THEME_ICONS = {
  light: Sun,
  dark: Moon,
  system: Monitor,
} as const

/** The same choices as connected segmented controls, for the mobile menu drawer. */
export function SettingsToggles() {
  const t = useTranslations('shell')
  const lang = useLocaleSwitch()
  const { theme: activeTheme } = useBootstrap()
  const canSwitchColor = !activeTheme.colorScheme || activeTheme.colorScheme === 'both'
  const { theme = 'system', setTheme } = useTheme()
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-2">
        <span className="text-xs font-medium text-faint-foreground">{t('language')}</span>
        <ToggleGroup
          variant="outline"
          spacing={0}
          value={[lang.locale]}
          onValueChange={(v) => v[0] && lang.set(v[0])}
          className="w-full"
        >
          {lang.locales.map((l) => (
            <ToggleGroupItem
              key={l.code}
              value={l.code}
              className="h-9 flex-1 text-xs pointer-coarse:h-10 sm:text-caption"
              disabled={lang.pending}
            >
              {l.name}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>
      {canSwitchColor && (
        <div className="flex flex-col gap-2">
          <span className="text-xs font-medium text-faint-foreground">{t('appearance')}</span>
          <ToggleGroup
            variant="outline"
            spacing={0}
            value={[theme]}
            onValueChange={(v) => v[0] && setTheme(v[0])}
            className="w-full"
          >
            {THEMES.map((m) => {
              const Icon = THEME_ICONS[m]
              return (
                <ToggleGroupItem
                  key={m}
                  value={m}
                  className="h-9 flex-1 gap-1.5 px-2 text-xs pointer-coarse:h-10 sm:text-caption"
                >
                  <Icon className="size-3.5" aria-hidden />
                  <span>{t(m)}</span>
                </ToggleGroupItem>
              )
            })}
          </ToggleGroup>
        </div>
      )}
    </div>
  )
}
