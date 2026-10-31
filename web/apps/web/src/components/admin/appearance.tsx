'use client'

import {
  type Block,
  type BlockType,
  listBlockTypes,
  listSettings,
  listThemes,
  type SettingsList,
  type ThemeView,
  type Tokens,
  updateSettings,
} from '@skinloom/api-client'
import { Badge, Button, Card, CardContent, CardDescription, CardHeader, CardTitle, Input } from '@skinloom/ui'
import { useRouter } from 'next/navigation'
import { useLocale, useTranslations } from 'next-intl'
import { useCallback, useEffect, useState } from 'react'

import { FormError } from '@/components/auth/field'

import { COLOR_TOKENS, type Mode, type Overrides, safeToken, withToken } from './appearance-form'
import { HomeEditor } from './home-editor'
import { SettingsSection } from './settings-center'
import { equal, groupSettings } from './settings-form'

/**
 * The appearance page: the active theme, colour adjustments on top of it, the home page
 * blocks, and the branding and custom CSS settings. Every save applies at once; the page
 * around it re-renders with the new look.
 */
export function Appearance({ superadmin }: { superadmin: boolean }) {
  const t = useTranslations('appearance')
  const router = useRouter()
  const [themes, setThemes] = useState<ThemeView[] | null>(null)
  const [list, setList] = useState<SettingsList | null>(null)
  const [types, setTypes] = useState<BlockType[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  const loadThemes = useCallback(async () => setThemes((await listThemes()).data as ThemeView[]), [])
  // Once: a save refreshes the page, which hands out a new t, and loading again then
  // would replace edits made since.
  useEffect(() => {
    Promise.all([loadThemes(), listSettings(), listBlockTypes()])
      .then(([, s, b]) => {
        setList(s.data as SettingsList)
        setTypes(b.data as BlockType[])
      })
      .catch(() => setError(t('errors.load')))
  }, [])

  if (!themes || !list || !types) return error ? <FormError message={error} /> : <p className="text-sm text-muted-foreground">{t('loading')}</p>

  function saved(next: SettingsList) {
    setList(next)
    void loadThemes().catch(() => undefined)
    router.refresh() // the header, footer and colours around this page
  }

  const active = themes.find((x) => x.active) ?? themes[0]
  const stored = (key: string) => list.settings.find((s) => s.key === key)?.value
  const overrides = (stored('theme.overrides') ?? {}) as Overrides
  const home = (stored('home.blocks') ?? null) as Block[] | null
  return (
    <div className="flex flex-col gap-6">
      <p className="text-sm text-muted-foreground">{t('intro')}</p>
      <ThemePicker themes={themes} onSaved={saved} />
      <ColorEditor key={`${active.id}:${JSON.stringify(overrides)}`} theme={active} overrides={overrides} onSaved={saved} />
      <HomeEditor key={`${active.id}:${JSON.stringify(home)}`} theme={active} types={types} stored={home} onSaved={saved} />
      {groupSettings(list.settings, 'appearance').map((g) => (
        <SettingsSection key={g.id} id={g.id} settings={g.settings} superadmin={superadmin} onSaved={saved} />
      ))}
    </div>
  )
}

/** The text of a theme's name or description for the page's language. */
function pick(m: Record<string, string> | undefined, locale: string): string {
  if (!m) return ''
  return m[locale] || m.en || m['zh-CN'] || Object.values(m)[0] || ''
}

function ThemePicker({ themes, onSaved }: { themes: ThemeView[]; onSaved: (l: SettingsList) => void }) {
  const t = useTranslations('appearance.themes')
  const locale = useLocale()
  const [pending, setPending] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function activate(id: string) {
    setPending(id)
    setError(null)
    try {
      onSaved((await updateSettings({ 'theme.active': id })).data as SettingsList)
    } catch {
      setError(t('failed'))
    } finally {
      setPending(null)
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('title')}</CardTitle>
        <CardDescription>{t('intro')}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="grid gap-4 sm:grid-cols-2">
          {themes.map((th) => (
            <div key={th.id} className={`flex flex-col gap-3 rounded-lg border p-4 ${th.active ? 'border-primary ring-1 ring-primary' : ''}`}>
              <Swatches tokens={th.tokens} />
              <div>
                <div className="flex flex-wrap items-center gap-2 font-medium">
                  {pick(th.name, locale)}
                  {th.active && <Badge>{t('active')}</Badge>}
                </div>
                <p className="text-xs text-muted-foreground">
                  {th.id} · v{th.version}
                </p>
                {th.description && <p className="mt-1 text-sm text-muted-foreground">{pick(th.description, locale)}</p>}
              </div>
              {!th.active && (
                <Button size="sm" variant="outline" className="self-start" disabled={pending !== null} onClick={() => void activate(th.id)}>
                  {t('activate')}
                </Button>
              )}
            </div>
          ))}
        </div>
        <FormError message={error} />
      </CardContent>
    </Card>
  )
}

/** A theme's look in both modes: its background, a primary dot and text. */
function Swatches({ tokens }: { tokens: Tokens }) {
  return (
    <div className="grid grid-cols-2 gap-2" aria-hidden>
      {(['light', 'dark'] as const).map((mode) => {
        const m = tokens[mode] ?? {}
        return (
          <div key={mode} className="flex items-center gap-2 rounded-md border px-3 py-2" style={{ background: m.background, color: m.foreground }}>
            <span className="size-4 rounded-full" style={{ background: m.primary }} />
            <span className="text-sm font-medium">Aa</span>
          </div>
        )
      })}
    </div>
  )
}

function ColorEditor({ theme, overrides, onSaved }: { theme: ThemeView; overrides: Overrides; onSaved: (l: SettingsList) => void }) {
  const t = useTranslations('appearance.colors')
  const [draft, setDraft] = useState<Overrides>(overrides)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const dirty = !equal(draft, overrides)
  const invalid = Object.values(draft).some((g) => Object.values(g ?? {}).some((v) => !safeToken(v)))

  async function save() {
    setPending(true)
    setError(null)
    try {
      onSaved((await updateSettings({ 'theme.overrides': Object.keys(draft).length > 0 ? draft : null })).data as SettingsList)
    } catch {
      setError(t('failed'))
      setPending(false)
    }
  }

  const field = (mode: Mode, name: string, color: boolean) => (
    <TokenInput
      key={mode}
      label={t(mode)}
      value={draft[mode]?.[name] ?? ''}
      fallback={theme.tokens[mode]?.[name] ?? ''}
      color={color}
      onChange={(v) => setDraft((d) => withToken(d, mode, name, v))}
    />
  )
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('title')}</CardTitle>
        <CardDescription>{t('intro')}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {COLOR_TOKENS.map((name) => (
          <div key={name} className="grid gap-2 sm:grid-cols-[10rem_1fr_1fr] sm:items-center">
            <span className="text-sm font-medium">{t(`tokens.${name}`)}</span>
            {field('light', name, true)}
            {field('dark', name, true)}
          </div>
        ))}
        <div className="grid gap-2 sm:grid-cols-[10rem_1fr_1fr] sm:items-center">
          <span className="text-sm font-medium">{t('tokens.radius')}</span>
          {field('light', 'radius', false)}
        </div>
        {invalid && <p className="text-xs text-destructive">{t('unsafe')}</p>}
        <FormError message={error} />
        <div className="flex flex-wrap items-center gap-3">
          <Button disabled={pending || !dirty || invalid} onClick={() => void save()}>
            {t('save')}
          </Button>
          {Object.keys(draft).length > 0 && (
            <Button variant="outline" disabled={pending} onClick={() => setDraft({})}>
              {t('clearAll')}
            </Button>
          )}
          {dirty && <span className="text-xs text-muted-foreground">{t('unsaved')}</span>}
        </div>
      </CardContent>
    </Card>
  )
}

/** One token: the value typed (empty = the theme's, shown as placeholder) and, for colours, a picker. */
function TokenInput({
  label,
  value,
  fallback,
  color,
  onChange,
}: {
  label: string
  value: string
  fallback: string
  color: boolean
  onChange: (v: string) => void
}) {
  const effective = value || fallback
  return (
    <div className="flex items-center gap-2">
      {color && (
        <input
          type="color"
          aria-label={label}
          value={hexOf(effective)}
          onChange={(e) => onChange(e.target.value)}
          className="h-9 w-10 shrink-0 cursor-pointer rounded-md border border-input bg-transparent p-1"
        />
      )}
      <Input aria-label={label} value={value} placeholder={fallback} onChange={(e) => onChange(e.target.value)} className="font-mono text-xs" />
    </div>
  )
}

const hexes = new Map<string, string>()

/**
 * The colour picker needs #rrggbb; theme tokens are any CSS colour (oklch…). The
 * browser converts by painting one pixel.
 */
function hexOf(css: string): string {
  const known = hexes.get(css)
  if (known) return known
  if (typeof document === 'undefined') return '#000000'
  const ctx = document.createElement('canvas').getContext('2d', { willReadFrequently: true })
  if (!ctx) return '#000000'
  ctx.fillStyle = '#000000'
  ctx.fillStyle = css // ignored when not a colour
  ctx.fillRect(0, 0, 1, 1)
  const [r, g, b] = ctx.getImageData(0, 0, 1, 1).data
  const hex = `#${[r, g, b].map((n) => n.toString(16).padStart(2, '0')).join('')}`
  hexes.set(css, hex)
  return hex
}
