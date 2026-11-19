import { type UseMutationOptions, type UseQueryOptions, useMutation, useQuery } from '@tanstack/react-query'
import { type ComponentType, createContext, createElement, type ReactNode, useContext } from 'react'

import type { PluginSlotName, PluginSlotProps, ThemeOverrideTarget } from '../gen/catalog.js'

export type { PluginSlotName, PluginSlotProps, ThemeOverrideTarget } from '../gen/catalog.js'

export type WebPluginSlotBinding<K extends PluginSlotName = PluginSlotName> =
  | ComponentType<PluginSlotProps[K]>
  | {
      component: ComponentType<PluginSlotProps[K]>
      order?: number
    }

export interface WebPluginDefinition {
  pages?: Record<string, ComponentType<Record<string, unknown>>>
  slots?: { [K in PluginSlotName]?: WebPluginSlotBinding<K> }
  blocks?: Record<string, ComponentType<{ props: Record<string, unknown> }>>
  overrides?: Partial<Record<ThemeOverrideTarget, ComponentType<Record<string, unknown>>>>
  settingsPage?: ComponentType<{
    settings: Record<string, unknown>
    onSave(next: Record<string, unknown>): Promise<void>
  }>
}

/** Declares a SkinLoom client-side plugin or Level 5 theme override module. */
export function defineWebPlugin(def: WebPluginDefinition): WebPluginDefinition {
  return def
}

export interface PluginWebViewer {
  id: number
  email: string
  nickname: string
  role: 'user' | 'admin' | 'superadmin'
  score: number
  locale: string
  avatarTextureId?: number | null
}

export interface PluginSiteMeta {
  name: string
  url: string
  description: string
  locale: string
  locales: string[]
}

export interface PluginThemeMeta {
  id: string
  name: string
  colorMode: 'light' | 'dark'
  tokens: Record<string, string>
}

export interface PluginToastApi {
  (message: string): void
  success(message: string): void
  error(message: string): void
  info(message: string): void
  warning(message: string): void
}

export interface PluginConfirmOptions {
  title: string
  description?: string
  confirmLabel?: string
  cancelLabel?: string
  destructive?: boolean
}

export interface PluginHostBridge {
  navigate?: (to: string) => void
  confirm?: (opts: PluginConfirmOptions) => Promise<boolean>
  toast?: PluginToastApi
}

export interface PluginWebRuntimeContextValue {
  pluginId: string
  settings: Record<string, unknown>
  messages: Record<string, string>
  session: PluginWebViewer | null
  site: PluginSiteMeta
  theme: PluginThemeMeta
  bridge?: PluginHostBridge
}

const defaultToast: PluginToastApi = Object.assign(
  (message: string) => {
    getHostBridge()?.toast?.(message)
  },
  {
    success: (message: string) => getHostBridge()?.toast?.success(message),
    error: (message: string) => getHostBridge()?.toast?.error(message),
    info: (message: string) => getHostBridge()?.toast?.info(message),
    warning: (message: string) => getHostBridge()?.toast?.warning(message),
  },
)

function getHostBridge(): PluginHostBridge | undefined {
  return (globalThis as { __SKINLOOM_PLUGIN_BRIDGE__?: PluginHostBridge }).__SKINLOOM_PLUGIN_BRIDGE__
}

const defaultContext: PluginWebRuntimeContextValue = {
  pluginId: '',
  settings: {},
  messages: {},
  session: null,
  site: { name: 'SkinLoom', url: '', description: '', locale: 'en', locales: ['zh-CN', 'en'] },
  theme: { id: 'default', name: 'SkinLoom Default', colorMode: 'light', tokens: {} },
}

const PluginWebContext = createContext<PluginWebRuntimeContextValue>(defaultContext)

export function PluginWebProvider({
  value,
  children,
}: {
  value: PluginWebRuntimeContextValue
  children?: ReactNode
}) {
  return createElement(PluginWebContext.Provider, { value }, children)
}

function resolvePluginContext(): PluginWebRuntimeContextValue {
  return useContext(PluginWebContext)
}

export function useSettings<T extends Record<string, unknown> = Record<string, unknown>>(): T {
  return resolvePluginContext().settings as T
}

export function useSession(): PluginWebViewer | null {
  return resolvePluginContext().session
}

export function useSite(): PluginSiteMeta {
  return resolvePluginContext().site
}

export function useTheme(): PluginThemeMeta {
  return resolvePluginContext().theme
}

export function useT(): (key: string, params?: Record<string, string | number>) => string {
  const { messages } = resolvePluginContext()
  return (key: string, params?: Record<string, string | number>) => {
    let text = Object.hasOwn(messages, key) ? messages[key] : key
    if (params) {
      for (const [k, v] of Object.entries(params)) {
        text = text.replaceAll(`{{${k}}}`, String(v)).replaceAll(`{${k}}`, String(v))
      }
    }
    return text
  }
}

export const toast: PluginToastApi = defaultToast

export async function confirm(opts: PluginConfirmOptions): Promise<boolean> {
  const hostConfirm = getHostBridge()?.confirm
  if (hostConfirm) return hostConfirm(opts)
  if (typeof globalThis.confirm === 'function') {
    return globalThis.confirm(opts.description ? `${opts.title}\n\n${opts.description}` : opts.title)
  }
  return false
}

export function navigate(to: string): void {
  const trimmed = to.trim()
  if (!trimmed || /[\x00-\x1f\x7f]/.test(trimmed)) {
    return
  }
  const isCleanPath = /^\/(?![/\\])/.test(trimmed)
  if (!isCleanPath) {
    try {
      const parsed = new URL(trimmed)
      if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
        return
      }
    } catch {
      return
    }
  }
  const hostNav = getHostBridge()?.navigate
  if (hostNav) {
    hostNav(trimmed)
    return
  }
  if (typeof window !== 'undefined' && window.location) {
    window.location.assign(trimmed)
  }
}

export class PluginApiError extends Error {
  readonly status: number
  readonly problem: { title?: string; detail?: string; [key: string]: unknown }

  constructor(status: number, problem: { title?: string; detail?: string; [key: string]: unknown }) {
    super(problem.detail ?? problem.title ?? `HTTP ${status}`)
    this.name = 'PluginApiError'
    this.status = status
    this.problem = problem
  }
}

function parseRouteSpec(spec: string, defaultMethod = 'GET'): { method: string; path: string } {
  const trimmed = spec.trim()
  const idx = trimmed.indexOf(' ')
  if (idx > 0) {
    return { method: trimmed.slice(0, idx).toUpperCase(), path: trimmed.slice(idx + 1).trim() }
  }
  return { method: defaultMethod, path: trimmed }
}

async function pluginFetch<T = unknown>(
  pluginId: string,
  method: string,
  subpath: string,
  init?: { body?: unknown; query?: Record<string, string | number | boolean | undefined>; headers?: Record<string, string> },
): Promise<T> {
  const cleanSub = subpath.startsWith('/') ? subpath : `/${subpath}`
  const qs = new URLSearchParams()
  if (init?.query) {
    for (const [k, v] of Object.entries(init.query)) {
      if (v !== undefined) qs.set(k, String(v))
    }
  }
  const queryStr = qs.toString()
  const basePath = `/api/plugins/${encodeURIComponent(pluginId)}${cleanSub}`
  const sep = basePath.includes('?') ? '&' : '?'
  const url = `${basePath}${queryStr ? `${sep}${queryStr}` : ''}`
  const headers: Record<string, string> = {
    'X-SkinLoom-Request': '1',
    ...(init?.headers ?? {}),
  }
  let body: BodyInit | undefined
  if (init?.body !== undefined && method !== 'GET' && method !== 'HEAD') {
    if (
      typeof init.body === 'string' ||
      init.body instanceof FormData ||
      init.body instanceof URLSearchParams ||
      (typeof Blob !== 'undefined' && init.body instanceof Blob) ||
      init.body instanceof ArrayBuffer ||
      ArrayBuffer.isView(init.body)
    ) {
      body = init.body as BodyInit
    } else {
      const hasContentType = Object.keys(headers).some((k) => k.toLowerCase() === 'content-type')
      if (!hasContentType) {
        headers['Content-Type'] = 'application/json'
      }
      body = JSON.stringify(init.body)
    }
  }
  const res = await fetch(url, {
    method,
    credentials: 'same-origin',
    headers,
    body,
  })
  if (res.status === 204) {
    return undefined as T
  }
  const text = await res.text()
  let parsed: unknown = null
  if (text) {
    try {
      parsed = JSON.parse(text)
    } catch {
      parsed = text
    }
  }
  if (!res.ok) {
    const prob = typeof parsed === 'object' && parsed !== null ? (parsed as Record<string, unknown>) : { detail: text }
    throw new PluginApiError(res.status, prob)
  }
  return parsed as T
}

export function usePluginApi(explicitPluginId?: string) {
  const ctx = resolvePluginContext()
  const id = explicitPluginId || ctx.pluginId

  const request = <T = unknown>(
    method: string,
    path: string,
    opts?: { body?: unknown; query?: Record<string, string | number | boolean | undefined>; headers?: Record<string, string> },
  ) => pluginFetch<T>(id, method.toUpperCase(), path, opts)

  return {
    pluginId: id,
    request,
    get: <T = unknown>(path: string, query?: Record<string, string | number | boolean | undefined>) =>
      request<T>('GET', path, { query }),
    post: <T = unknown>(path: string, body?: unknown) => request<T>('POST', path, { body }),
    put: <T = unknown>(path: string, body?: unknown) => request<T>('PUT', path, { body }),
    patch: <T = unknown>(path: string, body?: unknown) => request<T>('PATCH', path, { body }),
    delete: <T = unknown>(path: string, body?: unknown) => request<T>('DELETE', path, { body }),
    useQuery<TData = unknown, TError = PluginApiError>(
      route: string,
      opts?: Omit<UseQueryOptions<TData, TError>, 'queryKey' | 'queryFn'> & {
        query?: Record<string, string | number | boolean | undefined>
      },
    ) {
      const { method, path } = parseRouteSpec(route)
      const { query, ...rqOpts } = opts ?? {}
      return useQuery<TData, TError>({
        queryKey: ['skinloom-plugin', id, method, path, query ?? null],
        queryFn: () => request<TData>(method, path, { query }),
        ...rqOpts,
      })
    },
    useMutation<TData = unknown, TVariables = unknown, TError = PluginApiError>(
      route: string,
      opts?: Omit<UseMutationOptions<TData, TError, TVariables>, 'mutationFn'>,
    ) {
      const { method, path } = parseRouteSpec(route, 'POST')
      return useMutation<TData, TError, TVariables>({
        mutationFn: (vars: TVariables) => request<TData>(method, path, { body: vars }),
        ...opts,
      })
    },
  }
}
