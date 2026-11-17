'use client'

import {
  type Block,
  type BlockType,
  listBlockTypes,
  listSettings,
  listThemes,
  reloadThemes,
  type Setting,
  type SettingsList,
  type ThemeView,
  type Tokens,
  updateSettings,
} from '@skinloom/api-client'
import {
  Badge,
  Button,
  cn,
  Input,
  SaveBar,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Skeleton,
  Switch,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  toast,
} from '@skinloom/ui'
import { AlertCircle, Check, Download, RefreshCw } from 'lucide-react'
import type { ReactNode } from 'react'
import { useRouter } from 'next/navigation'
import { useLocale, useTranslations } from 'next-intl'
import { useCallback, useEffect, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { safeImageUrl } from '@/lib/safe-url'

import {
  buildThemeZip,
  COLOR_TOKENS,
  type Mode,
  type Overrides,
  safeToken,
  type ThemeSettingsMap,
  withThemeSetting,
  withToken,
} from './appearance-form'
import { HomeEditor } from './home-editor'
import { SettingsSection } from './settings-center'
import { equal, groupSettings } from './settings-form'

/**
 * The appearance page: the active theme, its declared settings, colour adjustments on top
 * of it, the home page blocks, and the branding and custom CSS/JS settings. Organised into
 * tabs (docs/design/07 §6.3, ADR 0043). Every save applies at once.
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

  if (!themes || !list || !types) return error ? <FormError message={error} /> : <Skeleton className="h-96 rounded-xl" />

  function saved(next: SettingsList) {
    setList(next)
    void loadThemes().catch(() => undefined)
    router.refresh() // the header, footer and colours around this page
  }

  const active = themes.find((x) => x.active) ?? themes.find((x) => !x.error) ?? themes[0]
  const stored = (key: string) => list.settings.find((s) => s.key === key)?.value
  const overrides = (stored('theme.overrides') ?? {}) as Overrides
  const themeSettings = (stored('theme.settings') ?? {}) as ThemeSettingsMap
  const home = (stored('home.blocks') ?? null) as Block[] | null
  const sections = groupSettings(list.settings, 'appearance')
  const branding = sections.filter((g) => g.id === 'branding')
  const custom = sections.filter((g) => g.id !== 'branding')

  return (
    <Tabs defaultValue="themes" className="flex flex-col gap-6">
      <TabsList variant="line" className="no-scrollbar w-full justify-start overflow-x-auto overflow-y-hidden">
        <TabsTrigger value="themes">{t('tabs.themes')}</TabsTrigger>
        <TabsTrigger value="settings">{t('tabs.settings')}</TabsTrigger>
        <TabsTrigger value="colors">{t('tabs.colors')}</TabsTrigger>
        <TabsTrigger value="home">{t('tabs.home')}</TabsTrigger>
        <TabsTrigger value="branding">{t('tabs.branding')}</TabsTrigger>
        <TabsTrigger value="custom">{t('tabs.custom')}</TabsTrigger>
      </TabsList>

      <TabsContent value="themes">
        <ThemePicker
          themes={themes}
          onReloaded={(next) => {
            setThemes(next)
            router.refresh()
          }}
          onSaved={saved}
        />
      </TabsContent>

      <TabsContent value="settings">
        <ThemeSettingsEditor
          key={`${active.id}:${JSON.stringify(themeSettings)}`}
          theme={active}
          stored={themeSettings}
          onSaved={saved}
        />
      </TabsContent>

      <TabsContent value="colors">
        <ColorEditor key={`${active.id}:${JSON.stringify(overrides)}`} theme={active} overrides={overrides} onSaved={saved} />
      </TabsContent>

      <TabsContent value="home">
        <HomeEditor key={`${active.id}:${JSON.stringify(home)}`} theme={active} types={types} stored={home} onSaved={saved} />
      </TabsContent>

      <TabsContent value="branding">
        <div className="flex flex-col gap-10">
          {branding.map((g) => (
            <SettingsSection key={g.id} id={g.id} settings={g.settings} superadmin={superadmin} onSaved={saved} />
          ))}
        </div>
      </TabsContent>

      <TabsContent value="custom">
        <div className="flex flex-col gap-10">
          {custom.map((g) => (
            <SettingsSection key={g.id} id={g.id} settings={g.settings} superadmin={superadmin} onSaved={saved} />
          ))}
        </div>
      </TabsContent>
    </Tabs>
  )
}

/** One part of the page: a heading with its intro, and the controls under it. */
export function Group({ title, intro, action, children }: { title: string; intro?: string; action?: ReactNode; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <h2 className="text-lg font-medium">{title}</h2>
          {intro && <p className="text-sm text-muted-foreground">{intro}</p>}
        </div>
        {action}
      </div>
      {children}
    </section>
  )
}

/** The text of a theme's name or description for the page's language. */
function pick(m: Record<string, string> | undefined, locale: string): string {
  if (!m) return ''
  return m[locale] || m.en || m['zh-CN'] || Object.values(m)[0] || ''
}

function ThemePicker({
  themes,
  onReloaded,
  onSaved,
}: {
  themes: ThemeView[]
  onReloaded: (next: ThemeView[]) => void
  onSaved: (l: SettingsList) => void
}) {
  const t = useTranslations('appearance.themes')
  const locale = useLocale()
  const [pending, setPending] = useState<string | null>(null)
  const [reloading, setReloading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function activate(id: string) {
    setPending(id)
    setError(null)
    try {
      onSaved((await updateSettings({ 'theme.active': id })).data as SettingsList)
      toast.success(t('activated'))
    } catch {
      setError(t('failed'))
    } finally {
      setPending(null)
    }
  }

  async function rescan() {
    setReloading(true)
    setError(null)
    try {
      const res = await reloadThemes()
      onReloaded(res.data as ThemeView[])
      toast.success(t('reloaded'))
    } catch {
      setError(t('failed'))
    } finally {
      setReloading(false)
    }
  }

  return (
    <Group
      title={t('title')}
      intro={t('intro')}
      action={
        <Button variant="outline" size="sm" loading={reloading} disabled={pending !== null} onClick={() => void rescan()}>
          <RefreshCw />
          {t('reload')}
        </Button>
      }
    >
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
        {themes.map((th) => {
          const shot = safeImageUrl(th.screenshot)
          return (
            <div
              key={`${th.id}:${th.source}:${th.error ?? ''}`}
              className={cn(
                'flex flex-col gap-3 rounded-xl border bg-card p-4',
                th.active && 'border-primary ring-1 ring-primary',
                th.error && 'border-destructive/40',
              )}
            >
              {shot ? (
                <div className="aspect-[16/10] overflow-hidden rounded-md border bg-muted">
                  <img src={shot} alt={pick(th.name, locale)} className="size-full object-cover" />
                </div>
              ) : !th.error ? (
                <Swatches tokens={th.tokens} />
              ) : null}
              <div className="flex flex-col gap-0.5">
                <div className="flex flex-wrap items-center gap-2 font-medium">
                  {pick(th.name, locale)}
                  <Badge variant="secondary">{th.source === 'builtin' ? t('builtin') : t('local')}</Badge>
                  {th.active && (
                    <Badge>
                      <Check />
                      {t('active')}
                    </Badge>
                  )}
                </div>
                <p className="font-mono text-caption text-faint-foreground">
                  {th.id}
                  {th.version ? ` · v${th.version}` : ''}
                  {th.extends ? ` · ${t('extends', { parent: th.extends })}` : ''}
                </p>
                {th.description && <p className="mt-1 text-sm text-muted-foreground">{pick(th.description, locale)}</p>}
                {th.error && (
                  <div className="mt-2 flex items-start gap-2 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive">
                    <AlertCircle className="mt-0.5 size-3.5 shrink-0" aria-hidden />
                    <span className="font-mono break-words">{th.error}</span>
                  </div>
                )}
              </div>
              {!th.active && !th.error && (
                <Button
                  size="sm"
                  variant="outline"
                  className="mt-auto self-start"
                  loading={pending === th.id}
                  disabled={pending !== null || reloading}
                  onClick={() => void activate(th.id)}
                >
                  {t('activate')}
                </Button>
              )}
            </div>
          )
        })}
      </div>
      <FormError message={error} />
    </Group>
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

/** Auto-generated form for the active theme's declared settings (ADR 0043). */
function ThemeSettingsEditor({
  theme,
  stored,
  onSaved,
}: {
  theme: ThemeView
  stored: ThemeSettingsMap
  onSaved: (l: SettingsList) => void
}) {
  const t = useTranslations('appearance.themeSettings')
  const th = useTranslations('appearance.home')
  const locale = useLocale()
  const [draft, setDraft] = useState<ThemeSettingsMap>(stored)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const entries = Object.entries(theme.settings ?? {})
  if (entries.length === 0) {
    return (
      <Group title={t('title')} intro={t('intro')}>
        <div className="rounded-xl border bg-card px-5 py-8 text-center text-sm text-muted-foreground">{t('empty')}</div>
      </Group>
    )
  }

  const cur = draft[theme.id] ?? {}
  const dirty = !equal(draft, stored)
  const invalid = Object.entries(cur).some(([, v]) => typeof v === 'string' && !safeToken(v))

  async function save() {
    setPending(true)
    setError(null)
    try {
      const payload = Object.keys(draft).length > 0 ? draft : null
      onSaved((await updateSettings({ 'theme.settings': payload })).data as SettingsList)
      toast.success(t('saved'))
    } catch {
      setError(t('failed'))
    } finally {
      setPending(false)
    }
  }

  function resetTheme() {
    const next = { ...draft }
    delete next[theme.id]
    setDraft(next)
  }

  return (
    <Group
      title={t('title')}
      intro={t('intro')}
      action={
        Object.keys(cur).length > 0 && (
          <Button variant="ghost" size="sm" disabled={pending} onClick={resetTheme}>
            {t('reset')}
          </Button>
        )
      }
    >
      <div className="flex flex-col divide-y rounded-xl border bg-card">
        {entries.map(([name, def]) => (
          <ThemeSettingField
            key={name}
            name={name}
            def={def}
            theme={theme}
            locale={locale}
            linkHint={th('linkHint')}
            value={cur[name]}
            onChange={(v) => setDraft((d) => withThemeSetting(d, theme.id, name, v))}
          />
        ))}
      </div>
      <FormError message={error} />
      <SaveBar count={dirty && !invalid ? 1 : 0} saving={pending} onSave={() => void save()} onDiscard={() => setDraft(stored)} />
    </Group>
  )
}

function ThemeSettingField({
  name,
  def,
  theme,
  locale,
  linkHint,
  value,
  onChange,
}: {
  name: string
  def: Setting
  theme: ThemeView
  locale: string
  linkHint: string
  value: string | boolean | undefined
  onChange: (v: string | boolean | null) => void
}) {
  const label = pick(def.label, locale) || name
  const fallbackStr =
    typeof def.default === 'string' ? def.default : def.token ? (theme.tokens.light?.[def.token] ?? '') : ''

  return (
    <div className="grid grid-cols-1 gap-2 px-5 py-3.5 sm:grid-cols-[12rem_1fr] sm:items-center sm:gap-4">
      <div className="flex flex-col gap-0.5">
        <span className="text-sm font-medium">{label}</span>
        <span className="font-mono text-caption text-faint-foreground">{def.token ? `--${def.token}` : `--sl-setting-${name}`}</span>
      </div>
      {def.type === 'color' && (
        <TokenInput
          label={label}
          value={typeof value === 'string' ? value : ''}
          fallback={fallbackStr}
          color
          onChange={(v) => onChange(v)}
        />
      )}
      {def.type === 'text' && (
        <Input
          aria-label={label}
          value={typeof value === 'string' ? value : ''}
          placeholder={fallbackStr}
          onChange={(e) => onChange(e.target.value)}
        />
      )}
      {def.type === 'image' && (
        <Input
          aria-label={label}
          value={typeof value === 'string' ? value : ''}
          placeholder={fallbackStr || linkHint}
          onChange={(e) => onChange(e.target.value)}
          className="font-mono text-xs"
        />
      )}
      {def.type === 'choice' && (
        <Select
          value={typeof value === 'string' && value !== '' ? value : String(def.default ?? def.options?.[0] ?? '')}
          onValueChange={(v) => onChange(String(v))}
        >
          <SelectTrigger aria-label={label} className="max-w-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {(def.options ?? []).map((opt) => (
              <SelectItem key={opt} value={opt}>
                {opt}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
      {def.type === 'boolean' && (
        <Switch
          aria-label={label}
          checked={typeof value === 'boolean' ? value : Boolean(def.default)}
          onCheckedChange={(checked) => onChange(Boolean(checked))}
        />
      )}
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
      toast.success(t('saved'))
    } catch {
      setError(t('failed'))
    } finally {
      setPending(false)
    }
  }

  function downloadThemeZip() {
    const bytes = buildThemeZip(theme, draft)
    const blob = new Blob([bytes.buffer as ArrayBuffer], { type: 'application/zip' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${theme.id}-custom.zip`
    a.click()
    URL.revokeObjectURL(url)
  }

  const field = (mode: Mode, name: string, color: boolean, showMobileLabel = color) => (
    <TokenInput
      key={mode}
      label={t(mode)}
      showMobileLabel={showMobileLabel}
      value={draft[mode]?.[name] ?? ''}
      fallback={theme.tokens[mode]?.[name] ?? ''}
      color={color}
      onChange={(v) => setDraft((d) => withToken(d, mode, name, v))}
    />
  )
  return (
    <Group
      title={t('title')}
      intro={t('intro')}
      action={
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="outline" size="sm" disabled={invalid} onClick={downloadThemeZip}>
            <Download />
            {t('exportTheme')}
          </Button>
          {Object.keys(draft).length > 0 && (
            <Button variant="ghost" size="sm" disabled={pending} onClick={() => setDraft({})}>
              {t('clearAll')}
            </Button>
          )}
        </div>
      }
    >
      <div className="flex flex-col divide-y rounded-xl border bg-card">
        <div className="hidden grid-cols-[10rem_1fr_1fr] gap-3 px-5 py-2.5 text-caption text-muted-foreground sm:grid">
          <span />
          <span>{t('light')}</span>
          <span>{t('dark')}</span>
        </div>
        {COLOR_TOKENS.map((name) => (
          <div key={name} className="grid grid-cols-1 gap-2 px-5 py-3 sm:grid-cols-[10rem_1fr_1fr] sm:items-center sm:gap-3">
            <span className="text-sm font-medium">{t(`tokens.${name}`)}</span>
            {field('light', name, true)}
            {field('dark', name, true)}
          </div>
        ))}
        <div className="grid grid-cols-1 gap-2 px-5 py-3 sm:grid-cols-[10rem_1fr_1fr] sm:items-center sm:gap-3">
          <span className="text-sm font-medium">{t('tokens.radius')}</span>
          {field('light', 'radius', false)}
        </div>
      </div>
      {invalid && <p className="text-caption text-destructive">{t('unsafe')}</p>}
      <FormError message={error} />
      <SaveBar count={dirty && !invalid ? 1 : 0} saving={pending} onSave={() => void save()} onDiscard={() => setDraft(overrides)} />
    </Group>
  )
}

/** One token: the value typed (empty = the theme's, shown as placeholder) and, for colours, a picker. */
function TokenInput({
  label,
  showMobileLabel = false,
  value,
  fallback,
  color,
  onChange,
}: {
  label: string
  showMobileLabel?: boolean
  value: string
  fallback: string
  color: boolean
  onChange: (v: string) => void
}) {
  const effective = value || fallback
  return (
    <div className="flex items-center gap-2">
      {showMobileLabel && <span className="min-w-10 shrink-0 text-caption text-muted-foreground sm:hidden">{label}</span>}
      {color && (
        <input
          type="color"
          aria-label={label}
          value={hexOf(effective)}
          onChange={(e) => onChange(e.target.value)}
          className="h-9 w-10 shrink-0 cursor-pointer rounded-md border border-input bg-transparent p-1 pointer-coarse:h-10"
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
