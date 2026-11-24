'use client'

import { type PluginFrontend, setThemePreview, updateSettings } from '@skinloom/api-client'
import * as PluginSdkWeb from '@skinloom/plugin-sdk/web'
import {
  PluginWebProvider,
  type PluginWebRuntimeContextValue,
  type PluginWebViewer,
  type WebPluginDefinition,
} from '@skinloom/plugin-sdk/web'
import * as SkinLoomUI from '@skinloom/ui'
import { Button, toast } from '@skinloom/ui'
import * as TanStackQuery from '@tanstack/react-query'
import { useTranslations } from 'next-intl'
import { useTheme as useNextTheme } from 'next-themes'
import * as React from 'react'
import { Component, createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import * as ReactDOM from 'react-dom'
import * as ReactDOMClient from 'react-dom/client'
import * as JsxRuntime from 'react/jsx-runtime'

import type { SiteBootstrap } from '@/lib/bootstrap'
import type { Viewer } from '@/lib/viewer'

declare global {
  var __SKINLOOM_SHARED__: Record<string, unknown> | undefined
  var __SKINLOOM_PLUGIN_BRIDGE__: PluginSdkWeb.PluginHostBridge | undefined
}

let sharedInitialized = false

/** Populates globalThis.__SKINLOOM_SHARED__ once so external plugin ESM bundles share React and @skinloom/ui. */
export function initSharedGlobals(): void {
  if (typeof globalThis === 'undefined') return
  if (sharedInitialized && globalThis.__SKINLOOM_SHARED__) return
  globalThis.__SKINLOOM_SHARED__ = {
    react: React,
    'react-dom': ReactDOM,
    'react-dom/client': ReactDOMClient,
    'react/jsx-runtime': JsxRuntime,
    'react/jsx-dev-runtime': {
      ...JsxRuntime,
      jsxDEV: (JsxRuntime as Record<string, unknown>).jsx,
    },
    '@tanstack/react-query': TanStackQuery,
    '@skinloom/ui': SkinLoomUI,
    '@skinloom/plugin-sdk/web': PluginSdkWeb,
  }
  sharedInitialized = true
}

export type DynamicModuleImporter = (url: string) => Promise<unknown>

const defaultImporter: DynamicModuleImporter = (url) =>
  import(/* webpackIgnore: true */ url)

export interface LoadedPluginEntry {
  meta: PluginFrontend
  definition: WebPluginDefinition
  runtimeContext: PluginWebRuntimeContextValue
}

export interface PluginRuntimeState {
  plugins: LoadedPluginEntry[]
  pluginsById: Map<string, LoadedPluginEntry>
  themeOverrideModule: WebPluginDefinition | null
  themeRuntimeContext: PluginWebRuntimeContextValue | null
  loading: boolean
}

const emptyState: PluginRuntimeState = {
  plugins: [],
  pluginsById: new Map(),
  themeOverrideModule: null,
  themeRuntimeContext: null,
  loading: false,
}

const PluginRuntimeContext = createContext<PluginRuntimeState>(emptyState)

export function usePluginRuntime(): PluginRuntimeState {
  return useContext(PluginRuntimeContext)
}

/** Isolates unexpected render failures inside a plugin UI slot, page, block, or theme override. */
export class PluginErrorBoundary extends Component<
  { pluginId: string; fallback?: ReactNode; children: ReactNode },
  { hasError: boolean }
> {
  state = { hasError: false }

  static getDerivedStateFromError(): { hasError: boolean } {
    return { hasError: true }
  }

  componentDidCatch(error: Error): void {
    console.warn(`[SkinLoom plugin "${this.props.pluginId}" render error]`, error)
  }

  render(): ReactNode {
    if (this.state.hasError) {
      return this.props.fallback ?? null
    }
    return this.props.children
  }
}

function toSdkViewer(viewer?: Viewer | null): PluginWebViewer | null {
  if (!viewer) return null
  return {
    id: viewer.id,
    email: viewer.email,
    nickname: viewer.nickname,
    role: viewer.role,
    score: viewer.score,
    locale: viewer.locale,
    avatarTextureId: viewer.avatarTextureId ?? null,
  }
}

function normalizeWebModule(mod: unknown): WebPluginDefinition | null {
  if (!mod || typeof mod !== 'object') return null
  const record = mod as { default?: unknown }
  const candidate = (record.default && typeof record.default === 'object' ? record.default : mod) as WebPluginDefinition
  return candidate
}

function buildRuntimeContext(
  id: string,
  settings: Record<string, unknown>,
  messages: Record<string, string>,
  bootstrap: SiteBootstrap,
  viewer: PluginWebViewer | null,
  dark: boolean,
  bridge?: PluginSdkWeb.PluginHostBridge,
): PluginWebRuntimeContextValue {
  return {
    pluginId: id,
    settings,
    messages,
    session: viewer,
    site: {
      name: bootstrap.site.name,
      url: bootstrap.site.url,
      description: bootstrap.site.description,
      locale: bootstrap.locale,
      locales: bootstrap.locales.map((l) => l.code),
    },
    theme: {
      id: bootstrap.theme.id,
      name: bootstrap.theme.previewName || bootstrap.theme.id,
      colorMode: dark ? 'dark' : 'light',
      tokens: (dark ? bootstrap.theme.tokens.dark : bootstrap.theme.tokens.light) ?? {},
    },
    bridge,
  }
}

export function PluginRuntimeProvider({
  bootstrap,
  viewer,
  bridge,
  importModule = defaultImporter,
  onRefresh,
  children,
}: {
  bootstrap: SiteBootstrap
  viewer?: Viewer | null
  bridge?: PluginSdkWeb.PluginHostBridge
  importModule?: DynamicModuleImporter
  onRefresh?: () => void
  children: ReactNode
}) {
  initSharedGlobals()

  const { resolvedTheme } = useNextTheme()
  const dark =
    bootstrap.theme.colorScheme === 'dark'
      ? true
      : bootstrap.theme.colorScheme === 'light'
        ? false
        : resolvedTheme === 'dark' || (resolvedTheme === undefined && typeof document !== 'undefined' && document.documentElement.classList.contains('dark'))

  const [loadedMap, setLoadedMap] = useState<Map<string, WebPluginDefinition>>(() => new Map())
  const [loadedTheme, setLoadedTheme] = useState<{ key: string; def: WebPluginDefinition } | null>(null)
  const [loading, setLoading] = useState(() =>
    Boolean((bootstrap.plugins ?? []).some((p) => Boolean(p.entry)) || bootstrap.theme.webEntry),
  )

  const sdkViewer = useMemo(() => toSdkViewer(viewer), [viewer])
  const pluginSpecsJson = useMemo(
    () =>
      JSON.stringify(
        (bootstrap.plugins ?? []).map((p) => ({
          id: p.id,
          rev: p.rev || p.version,
          entry: p.entry ?? '',
          style: p.style ?? '',
        })),
      ),
    [bootstrap.plugins],
  )
  const styleSpecsJson = useMemo(
    () =>
      JSON.stringify(
        (bootstrap.plugins ?? [])
          .filter((p) => Boolean(p.style))
          .map((p) => ({
            id: p.id,
            rev: p.rev || p.version,
            style: p.style ?? '',
          })),
      ),
    [bootstrap.plugins],
  )
  const hasPendingSpecs = useMemo(
    () => Boolean((bootstrap.plugins ?? []).some((p) => Boolean(p.entry)) || bootstrap.theme.webEntry),
    [bootstrap.plugins, bootstrap.theme.webEntry],
  )
  const loadTargetKey = useMemo(
    () =>
      `${pluginSpecsJson}|${bootstrap.theme.webEntry ? `${bootstrap.theme.id}@${bootstrap.theme.version}:${bootstrap.theme.webEntry}` : ''}`,
    [bootstrap.theme.id, bootstrap.theme.version, bootstrap.theme.webEntry, pluginSpecsJson],
  )
  const [settledKey, setSettledKey] = useState<string>(() => (hasPendingSpecs ? '' : loadTargetKey))

  useEffect(() => {
    if (typeof globalThis === 'undefined' || !bridge) return
    globalThis.__SKINLOOM_PLUGIN_BRIDGE__ = bridge
    return () => {
      if (globalThis.__SKINLOOM_PLUGIN_BRIDGE__ === bridge) {
        delete globalThis.__SKINLOOM_PLUGIN_BRIDGE__
      }
    }
  }, [bridge])

  useEffect(() => {
    initSharedGlobals()
    if (typeof document === 'undefined') return
    const specs = JSON.parse(styleSpecsJson) as Array<{ id: string; rev: string; style: string }>
    const styleTags: HTMLLinkElement[] = []
    for (const p of specs) {
      if (!p.style) continue
      const link = document.createElement('link')
      link.rel = 'stylesheet'
      link.href = p.style
      link.dataset.slPluginStyle = `${p.id}@${p.rev}`
      document.head.appendChild(link)
      styleTags.push(link)
    }
    return () => {
      for (const el of styleTags) {
        el.remove()
      }
    }
  }, [styleSpecsJson])

  useEffect(() => {
    let cancelled = false
    const specs = JSON.parse(pluginSpecsJson) as Array<{ id: string; rev: string; entry: string }>
    const withEntries = specs.filter((p) => Boolean(p.entry))
    const themeKey = bootstrap.theme.webEntry ? `${bootstrap.theme.id}@${bootstrap.theme.version}:${bootstrap.theme.webEntry}` : ''

    if (withEntries.length === 0 && !themeKey) {
      setLoadedMap(new Map())
      setLoadedTheme(null)
      setSettledKey(loadTargetKey)
      setLoading(false)
      return
    }

    setLoading(true)
    void (async () => {
      const nextPlugins = new Map<string, WebPluginDefinition>()
      await Promise.all(
        withEntries.map(async (p) => {
          try {
            const raw = await importModule(p.entry)
            const def = normalizeWebModule(raw)
            if (def && !cancelled) {
              nextPlugins.set(`${p.id}@${p.rev}`, def)
            }
          } catch (err) {
            console.warn(`[SkinLoom plugin "${p.id}" load failed]`, err)
          }
        }),
      )

      let nextTheme: { key: string; def: WebPluginDefinition } | null = null
      if (themeKey) {
        try {
          const rawTheme = await importModule(bootstrap.theme.webEntry!)
          const def = normalizeWebModule(rawTheme)
          if (def) nextTheme = { key: themeKey, def }
        } catch (err) {
          console.warn(`[SkinLoom theme "${bootstrap.theme.id}" override load failed]`, err)
        }
      }

      if (!cancelled) {
        setLoadedMap(nextPlugins)
        setLoadedTheme(nextTheme)
        setSettledKey(loadTargetKey)
        setLoading(false)
      }
    })()

    return () => {
      cancelled = true
    }
  }, [bootstrap.theme.id, bootstrap.theme.version, bootstrap.theme.webEntry, importModule, loadTargetKey, pluginSpecsJson])

  const effectiveLoading = loading || (hasPendingSpecs && settledKey !== loadTargetKey)

  const state = useMemo<PluginRuntimeState>(() => {
    const entries: LoadedPluginEntry[] = []
    const byId = new Map<string, LoadedPluginEntry>()

    for (const meta of bootstrap.plugins ?? []) {
      const cacheKey = `${meta.id}@${meta.rev || meta.version}`
      const definition = loadedMap.get(cacheKey)
      if (!definition) continue
      const entry: LoadedPluginEntry = {
        meta,
        definition,
        runtimeContext: buildRuntimeContext(
          meta.id,
          (meta.settings ?? {}) as Record<string, unknown>,
          (meta.messages ?? {}) as Record<string, string>,
          bootstrap,
          sdkViewer,
          dark,
          bridge,
        ),
      }
      entries.push(entry)
      byId.set(meta.id, entry)
    }

    const themeKey = bootstrap.theme.webEntry ? `${bootstrap.theme.id}@${bootstrap.theme.version}:${bootstrap.theme.webEntry}` : ''
    const themeOverrideModule = loadedTheme && loadedTheme.key === themeKey ? loadedTheme.def : null
    const themePluginMeta = (bootstrap.plugins ?? []).find((p) => p.id === bootstrap.theme.id)
    const themeMergedSettings: Record<string, unknown> = {
      ...((themePluginMeta?.settings ?? {}) as Record<string, unknown>),
      ...((bootstrap.theme.settings ?? {}) as Record<string, unknown>),
    }
    const themeRuntimeContext = buildRuntimeContext(
      bootstrap.theme.id,
      themeMergedSettings,
      (themePluginMeta?.messages ?? {}) as Record<string, string>,
      bootstrap,
      sdkViewer,
      dark,
      bridge,
    )

    return {
      plugins: entries,
      pluginsById: byId,
      themeOverrideModule,
      themeRuntimeContext,
      loading: effectiveLoading,
    }
  }, [bootstrap, bridge, dark, effectiveLoading, loadedMap, loadedTheme, sdkViewer])

  return (
    <PluginRuntimeContext.Provider value={state}>
      {children}
      {bootstrap.theme.previewing && <ThemePreviewBar bootstrap={bootstrap} onRefresh={onRefresh} />}
    </PluginRuntimeContext.Provider>
  )
}

/** Floating administrator bar displayed when a session-scoped theme preview cookie is active. */
export function ThemePreviewBar({ bootstrap, onRefresh }: { bootstrap: SiteBootstrap; onRefresh?: () => void }) {
  const t = useTranslations('appearance.themes')
  const [busy, setBusy] = useState<'apply' | 'exit' | null>(null)
  const label = bootstrap.theme.previewName || bootstrap.theme.id

  async function apply() {
    setBusy('apply')
    try {
      await updateSettings({ 'theme.active': bootstrap.theme.id })
      await setThemePreview({ themeId: '' })
      toast.success(t('activated'))
      onRefresh?.()
    } catch {
      toast.error(t('failed'))
    } finally {
      setBusy(null)
    }
  }

  async function exit() {
    setBusy('exit')
    try {
      await setThemePreview({ themeId: '' })
      onRefresh?.()
    } catch {
      toast.error(t('failed'))
    } finally {
      setBusy(null)
    }
  }

  return (
    <div
      role="region"
      aria-label={t('previewBar', { name: label })}
      data-sl-theme-preview={bootstrap.theme.id}
      className="fixed inset-x-0 top-16 z-40 mx-auto flex w-fit max-w-[calc(100vw-2rem)] flex-wrap items-center gap-3 rounded-xl border bg-card/95 px-4 py-2.5 text-sm text-card-foreground shadow-(--shadow-dialog) backdrop-blur"
    >
      <span role="status" className="font-medium">
        {t('previewBar', { name: label })}
      </span>
      <div className="flex items-center gap-2">
        <Button size="sm" className="pointer-coarse:h-10" loading={busy === 'apply'} disabled={busy !== null} onClick={() => void apply()}>
          {t('applyPreview')}
        </Button>
        <Button size="sm" variant="outline" className="pointer-coarse:h-10" loading={busy === 'exit'} disabled={busy !== null} onClick={() => void exit()}>
          {t('exitPreview')}
        </Button>
      </div>
    </div>
  )
}

export { PluginWebProvider, PluginWebProvider as PluginWebRuntimeProvider }
