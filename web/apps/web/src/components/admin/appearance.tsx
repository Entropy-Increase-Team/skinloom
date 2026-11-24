'use client'

import {
  type Block,
  type BlockType,
  listBlockTypes,
  listSettings,
  listThemes,
  reloadThemes,
  setThemePreview,
  type Setting,
  type SettingsList,
  type ThemeView,
  type Tokens,
  updateSettings,
} from '@skinloom/api-client'
import {
  Alert,
  AlertAction,
  AlertDescription,
  Badge,
  Button,
  ButtonLink,
  cn,
  Empty,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
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
import { AlertCircle, Check, CircleAlert, Download, Palette, RefreshCw, SlidersHorizontal, Store } from 'lucide-react'
import Link from 'next/link'
import type { ReactNode } from 'react'
import { useRouter } from 'next/navigation'
import { useLocale, useTranslations } from 'next-intl'
import { useCallback, useEffect, useRef, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { useBootstrap } from '@/components/providers'
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
import { resolveLocalizedFieldText } from './plugins-settings-form'
import { SettingsSection } from './settings-center'
import { equal, groupSettings } from './settings-form'

/**
 * The appearance page: the active theme, its declared settings, colour adjustments on top
 * of it, the home page blocks, and the branding and custom CSS/JS settings. Organised into
 * tabs (docs/design/07 §6.3, ADR 0043). Every save applies at once.
 */
export function Appearance({ superadmin }: { superadmin: boolean }) {
  const t = useTranslations('appearance')
  const tc = useTranslations('common')
  const tRef = useRef(t)
  tRef.current = t
  const boot = useBootstrap()
  const router = useRouter()
  const [themes, setThemes] = useState<ThemeView[] | null>(null)
  const [list, setList] = useState<SettingsList | null>(null)
  const [types, setTypes] = useState<BlockType[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [dirtySections, setDirtySections] = useState<Set<string>>(() => new Set())
  const themesRef = useRef<ThemeView[] | null>(null)
  themesRef.current = themes
  const prevBootThemeRef = useRef<{ id: string; previewing: boolean }>({
    id: boot.theme.id,
    previewing: Boolean(boot.theme.previewing),
  })

  const loadThemes = useCallback(async () => setThemes((await listThemes()).data as ThemeView[]), [])
  const loadAll = useCallback(() => {
    setError(null)
    Promise.all([loadThemes(), listSettings(), listBlockTypes()])
      .then(([, s, b]) => {
        setList(s.data as SettingsList)
        setTypes(b.data as BlockType[])
      })
      .catch(() => setError(tRef.current('errors.load')))
  }, [loadThemes])

  // Once: a save refreshes the page, which hands out a new t, and loading again then
  // would replace edits made since.
  useEffect(() => {
    loadAll()
  }, [loadAll])

  // Keep ThemePicker in sync when ThemePreviewBar applies a theme and calls router.refresh().
  useEffect(() => {
    const prev = prevBootThemeRef.current
    const curPreviewing = Boolean(boot.theme.previewing)
    prevBootThemeRef.current = { id: boot.theme.id, previewing: curPreviewing }
    if (prev.id === boot.theme.id && prev.previewing === curPreviewing) return
    if (!themesRef.current || !boot.theme.id || curPreviewing) return
    if (!themesRef.current.some((th) => th.id === boot.theme.id && !th.error && th.active)) {
      void loadThemes().catch(() => undefined)
    }
  }, [boot.theme.id, boot.theme.previewing, loadThemes])

  useEffect(() => {
    if (dirtySections.size === 0) return
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [dirtySections.size])

  const handleSectionDirty = useCallback((sectionId: string, isDirty: boolean) => {
    setDirtySections((prev) => {
      const has = prev.has(sectionId)
      if (has === isDirty) return prev
      const next = new Set(prev)
      if (isDirty) next.add(sectionId)
      else next.delete(sectionId)
      return next
    })
  }, [])

  if (!themes || !list || !types) {
    return error ? (
      <Alert variant="destructive" role="alert">
        <CircleAlert />
        <AlertDescription>{error}</AlertDescription>
        <AlertAction>
          <Button type="button" variant="outline" size="sm" onClick={loadAll}>
            {tc('retry')}
          </Button>
        </AlertAction>
      </Alert>
    ) : (
      <Skeleton className="h-96 rounded-xl" />
    )
  }

  function saved(next: SettingsList) {
    setList(next)
    void loadThemes().catch(() => undefined)
    router.refresh() // the header, footer and colours around this page
  }

  const active = themes.find((x) => x.active) ?? themes.find((x) => !x.error) ?? themes[0]
  const stored = (key: string) => list.settings.find((s) => s.key === key)?.value
  const overrides = (stored('theme.overrides') ?? {}) as Overrides
  const themeSettings = (stored('theme.settings') ?? {}) as ThemeSettingsMap
  const trustedWeb = Array.isArray(stored('theme.trusted_web')) ? (stored('theme.trusted_web') as string[]) : []
  const home = (stored('home.blocks') ?? null) as Block[] | null
  const sections = groupSettings(list.settings, 'appearance')
  const branding = sections.filter((g) => g.id === 'branding')
  const custom = sections.filter((g) => g.id !== 'branding')

  return (
    <Tabs defaultValue="themes" className="flex flex-col gap-6">
      <TabsList variant="line" className="no-scrollbar h-auto w-full justify-start overflow-x-auto overflow-y-hidden pb-1.5">
        <TabsTrigger value="themes">{t('tabs.themes')}</TabsTrigger>
        <TabsTrigger value="settings">{t('tabs.settings')}</TabsTrigger>
        <TabsTrigger value="colors">{t('tabs.colors')}</TabsTrigger>
        <TabsTrigger value="home">{t('tabs.home')}</TabsTrigger>
        <TabsTrigger value="branding">{t('tabs.branding')}</TabsTrigger>
        <TabsTrigger value="custom">{t('tabs.custom')}</TabsTrigger>
      </TabsList>

      <TabsContent value="themes" keepMounted>
        <ThemePicker
          themes={themes}
          superadmin={superadmin}
          trustedWeb={trustedWeb}
          onReloaded={(next) => {
            setThemes(next)
            router.refresh()
          }}
          onSaved={saved}
        />
      </TabsContent>

      <TabsContent value="settings" keepMounted>
        {active ? (
          <ThemeSettingsEditor
            key={`${active.id}:${JSON.stringify(themeSettings)}`}
            theme={active}
            stored={themeSettings}
            onSaved={saved}
          />
        ) : (
          <Empty className="border border-dashed py-16">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <Palette />
              </EmptyMedia>
              <EmptyTitle>{t('themeSettings.empty')}</EmptyTitle>
            </EmptyHeader>
          </Empty>
        )}
      </TabsContent>

      <TabsContent value="colors" keepMounted>
        {active ? (
          <ColorEditor key={`${active.id}:${JSON.stringify(overrides)}`} theme={active} overrides={overrides} onSaved={saved} />
        ) : (
          <Empty className="border border-dashed py-16">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <Palette />
              </EmptyMedia>
              <EmptyTitle>{t('errors.load')}</EmptyTitle>
            </EmptyHeader>
          </Empty>
        )}
      </TabsContent>

      <TabsContent value="home" keepMounted>
        {active ? (
          <HomeEditor key={`${active.id}:${JSON.stringify(home)}`} theme={active} types={types} stored={home} onSaved={saved} />
        ) : (
          <Empty className="border border-dashed py-16">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <Palette />
              </EmptyMedia>
              <EmptyTitle>{t('home.empty')}</EmptyTitle>
            </EmptyHeader>
          </Empty>
        )}
      </TabsContent>

      <TabsContent value="branding" keepMounted>
        <div className="flex flex-col gap-10">
          {branding.map((g) => (
            <SettingsSection key={g.id} id={g.id} settings={g.settings} superadmin={superadmin} onSaved={saved} onDirtyChange={handleSectionDirty} />
          ))}
        </div>
      </TabsContent>

      <TabsContent value="custom" keepMounted>
        <div className="flex flex-col gap-10">
          {custom.map((g) => (
            <SettingsSection key={g.id} id={g.id} settings={g.settings} superadmin={superadmin} onSaved={saved} onDirtyChange={handleSectionDirty} />
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
        <div className="flex min-w-0 flex-1 basis-64 flex-col gap-1">
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
  return resolveLocalizedFieldText(m, locale, '')
}

function ThemePicker({
  themes,
  superadmin,
  trustedWeb,
  onReloaded,
  onSaved,
}: {
  themes: ThemeView[]
  superadmin: boolean
  trustedWeb: string[]
  onReloaded: (next: ThemeView[]) => void
  onSaved: (l: SettingsList) => void
}) {
  const t = useTranslations('appearance.themes')
  const locale = useLocale()
  const router = useRouter()
  const [pending, setPending] = useState<string | null>(null)
  const [reloading, setReloading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function activate(id: string) {
    setPending(id)
    setError(null)
    try {
      await setThemePreview({ themeId: '' }).catch(() => undefined)
      onSaved((await updateSettings({ 'theme.active': id })).data as SettingsList)
      toast.success(t('activated'))
    } catch {
      setError(t('failed'))
    } finally {
      setPending(null)
    }
  }

  async function preview(id: string) {
    setPending(`preview:${id}`)
    setError(null)
    try {
      await setThemePreview({ themeId: id })
      router.refresh()
    } catch {
      setError(t('failed'))
    } finally {
      setPending(null)
    }
  }

  async function trustWeb(id: string) {
    setPending(`trust:${id}`)
    setError(null)
    try {
      const next = trustedWeb.includes(id) ? trustedWeb : [...trustedWeb, id]
      onSaved((await updateSettings({ 'theme.trusted_web': next })).data as SettingsList)
      toast.success(t('trustedWeb'))
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
        <div className="flex flex-wrap items-center gap-2">
          <ButtonLink
            variant="outline"
            size="sm"
            className="pointer-coarse:h-10"
            render={<Link href="/admin/plugins?tab=market&category=theme" />}
          >
            <Store />
            {t('browseMarket')}
          </ButtonLink>
          <Button variant="outline" size="sm" className="pointer-coarse:h-10" loading={reloading} disabled={pending !== null} onClick={() => void rescan()}>
            <RefreshCw />
            {t('reload')}
          </Button>
        </div>
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
                  {th.hasWeb && th.webTrusted && <Badge variant="outline">{t('trustedWeb')}</Badge>}
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
              {!th.error && (
                <div className="mt-auto flex flex-wrap items-center gap-2">
                  {!th.active && (
                    <Button
                      size="sm"
                      variant="outline"
                      className="pointer-coarse:h-10"
                      loading={pending === th.id}
                      disabled={pending !== null || reloading}
                      onClick={() => void activate(th.id)}
                    >
                      {t('activate')}
                    </Button>
                  )}
                  {!th.active && (
                    <Button
                      size="sm"
                      variant="ghost"
                      className="pointer-coarse:h-10"
                      loading={pending === `preview:${th.id}`}
                      disabled={pending !== null || reloading}
                      onClick={() => void preview(th.id)}
                    >
                      {t('preview')}
                    </Button>
                  )}
                  {th.hasWeb && !th.webTrusted && superadmin && (
                    <Button
                      size="sm"
                      variant="ghost"
                      className="pointer-coarse:h-10"
                      loading={pending === `trust:${th.id}`}
                      disabled={pending !== null || reloading}
                      onClick={() => void trustWeb(th.id)}
                    >
                      {t('trustWeb')}
                    </Button>
                  )}
                </div>
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
  const tc = useTranslations('appearance.colors')
  const th = useTranslations('appearance.home')
  const locale = useLocale()
  const [draft, setDraft] = useState<ThemeSettingsMap>(stored)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const entries = Object.entries(theme.settings ?? {})
  const cur = draft[theme.id] ?? {}
  const orig = stored[theme.id] ?? {}
  const changedKeys = new Set([...Object.keys(cur), ...Object.keys(orig)])
  const dirtyCount = Array.from(changedKeys).filter((k) => !equal(cur[k], orig[k])).length
  const invalid = Object.entries(cur).some(([, v]) => typeof v === 'string' && !safeToken(v))

  useEffect(() => {
    if (dirtyCount === 0) return
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [dirtyCount])

  if (entries.length === 0) {
    return (
      <Group title={t('title')} intro={t('intro')}>
        <Empty className="border border-dashed py-12">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <SlidersHorizontal />
            </EmptyMedia>
            <EmptyTitle>{t('empty')}</EmptyTitle>
          </EmptyHeader>
        </Empty>
      </Group>
    )
  }

  async function save() {
    if (invalid) return
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
          <Button variant="ghost" size="sm" className="pointer-coarse:h-10" disabled={pending} onClick={resetTheme}>
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
      {invalid && <p className="text-caption text-destructive">{tc('unsafe')}</p>}
      <FormError message={error} />
      <SaveBar count={dirtyCount} disabled={invalid} saving={pending} onSave={() => void save()} onDiscard={() => setDraft(stored)} />
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
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="text-sm font-medium">{label}</span>
        <span className="min-w-0 break-all font-mono text-caption text-faint-foreground">{def.token ? `--${def.token}` : `--sl-setting-${name}`}</span>
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
  const dirtyCount = Array.from(new Set([...Object.keys(draft), ...Object.keys(overrides)])).reduce((sum, group) => {
    const a = draft[group as keyof Overrides] ?? {}
    const b = overrides[group as keyof Overrides] ?? {}
    const keys = new Set([...Object.keys(a), ...Object.keys(b)])
    return sum + Array.from(keys).filter((k) => (a[k] ?? '') !== (b[k] ?? '')).length
  }, 0)
  const invalid = Object.values(draft).some((g) => Object.values(g ?? {}).some((v) => !safeToken(v)))

  useEffect(() => {
    if (dirtyCount === 0) return
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [dirtyCount])

  async function save() {
    if (invalid) return
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
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  }

  const field = (mode: Mode, name: string, color: boolean, showMobileLabel = color) => (
    <TokenInput
      key={mode}
      label={t(mode)}
      ariaLabel={color ? `${t(`tokens.${name}`)} (${t(mode)})` : t(`tokens.${name}`)}
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
          <Button variant="outline" size="sm" className="pointer-coarse:h-10" disabled={invalid} onClick={downloadThemeZip}>
            <Download />
            {t('exportTheme')}
          </Button>
          {Object.keys(draft).length > 0 && (
            <Button variant="ghost" size="sm" className="pointer-coarse:h-10" disabled={pending} onClick={() => setDraft({})}>
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
      <SaveBar count={dirtyCount} disabled={invalid} saving={pending} onSave={() => void save()} onDiscard={() => setDraft(overrides)} />
    </Group>
  )
}

/** One token: the value typed (empty = the theme's, shown as placeholder) and, for colours, a picker. */
function TokenInput({
  label,
  ariaLabel,
  showMobileLabel = false,
  value,
  fallback,
  color,
  onChange,
}: {
  label: string
  ariaLabel?: string
  showMobileLabel?: boolean
  value: string
  fallback: string
  color: boolean
  onChange: (v: string) => void
}) {
  const effective = value || fallback
  const accessibleName = ariaLabel ?? label
  return (
    <div className="flex items-center gap-2">
      {showMobileLabel && <span className="min-w-10 shrink-0 text-caption text-muted-foreground sm:hidden">{label}</span>}
      {color && (
        <input
          type="color"
          aria-label={accessibleName}
          value={hexOf(effective)}
          onChange={(e) => onChange(e.target.value)}
          className="h-9 w-10 shrink-0 cursor-pointer rounded-md border border-input bg-transparent p-1 pointer-coarse:h-10"
        />
      )}
      <Input aria-label={accessibleName} value={value} placeholder={fallback} onChange={(e) => onChange(e.target.value)} className="font-mono text-xs" />
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
