'use client'

import { ConfirmProvider, CSPProvider, toast, Toaster, TooltipProvider, type UiLabels, UiLabelsProvider, useConfirm } from '@skinloom/ui'
import type { PluginHostBridge } from '@skinloom/plugin-sdk/web'
import { QueryClientProvider } from '@tanstack/react-query'
import { useRouter } from 'next/navigation'
import { useMessages, useTranslations } from 'next-intl'
import { ThemeProvider } from 'next-themes'
import { createContext, useContext, useMemo, useState } from 'react'

import type { SiteBootstrap } from '@/lib/bootstrap'
import { PluginRuntimeProvider } from '@/lib/plugin-runtime'
import { makeQueryClient } from '@/lib/query'
import type { Viewer } from '@/lib/viewer'

const BootstrapContext = createContext<SiteBootstrap | null>(null)
export const BootstrapProvider = BootstrapContext.Provider

/** Bootstrap data for client components (theme, site name, menus, plugins). */
export function useBootstrap(): SiteBootstrap {
  const v = useContext(BootstrapContext)
  if (!v) throw new Error('useBootstrap outside <Providers>')
  return v
}

/** Filters out unsafe URL schemes and protocol-relative open redirects before navigating. */
export function resolveSafeNavigateTarget(to: string, currentOrigin?: string): { kind: 'internal'; path: string } | { kind: 'external'; url: string } | null {
  const trimmed = to.trim()
  if (!trimmed || /[\x00-\x1f\x7f]/.test(trimmed)) return null
  if (/^\/(?![/\\])/.test(trimmed)) {
    return { kind: 'internal', path: trimmed }
  }
  try {
    const parsed = new URL(trimmed)
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
      return null
    }
    if (currentOrigin && parsed.origin === currentOrigin) {
      return { kind: 'internal', path: `${parsed.pathname}${parsed.search}${parsed.hash}` }
    }
    return { kind: 'external', url: trimmed }
  } catch {
    return null
  }
}

function RuntimeShell({
  bootstrap,
  viewer,
  confirmLabel,
  children,
}: {
  bootstrap: SiteBootstrap
  viewer?: Viewer | null
  confirmLabel?: string
  children: React.ReactNode
}) {
  const router = useRouter()
  const confirm = useConfirm()
  const tc = useTranslations('common')
  const defaultConfirmLabel = confirmLabel ?? tc('confirm')
  const bridge = useMemo<PluginHostBridge>(
    () => ({
      navigate: (to: string) => {
        const target = resolveSafeNavigateTarget(to, typeof window !== 'undefined' ? window.location.origin : undefined)
        if (!target) return
        if (target.kind === 'internal') {
          router.push(target.path)
        } else if (typeof window !== 'undefined') {
          window.location.assign(target.url)
        }
      },
      confirm: (opts) => confirm({ ...opts, confirmLabel: opts.confirmLabel ?? defaultConfirmLabel }),
      toast,
    }),
    [confirm, defaultConfirmLabel, router],
  )
  return (
    <PluginRuntimeProvider bootstrap={bootstrap} viewer={viewer} bridge={bridge} onRefresh={() => router.refresh()}>
      {children}
    </PluginRuntimeProvider>
  )
}

export function Providers({
  bootstrap,
  viewer,
  nonce,
  children,
}: {
  bootstrap: SiteBootstrap
  viewer?: Viewer | null
  /** The page's CSP nonce, for next-themes' inline script that picks light or dark. */
  nonce?: string
  children: React.ReactNode
}) {
  // One cache per browser tab, kept across navigations and re-renders.
  const [queryClient] = useState(makeQueryClient)
  // The "ui" messages as written: the components fill in {count}, {text}… themselves.
  const messages = useMessages()
  const labels = useMemo(() => (messages.ui ?? {}) as Partial<UiLabels>, [messages])
  const forcedTheme = bootstrap.theme.colorScheme === 'light' || bootstrap.theme.colorScheme === 'dark' ? bootstrap.theme.colorScheme : undefined
  return (
    <BootstrapContext.Provider value={bootstrap}>
      {/* Base UI renders a few inline scripts and styles: they need the page's nonce. */}
      <CSPProvider nonce={nonce}>
        <QueryClientProvider client={queryClient}>
          <ThemeProvider attribute="class" defaultTheme="system" enableSystem disableTransitionOnChange forcedTheme={forcedTheme} nonce={nonce}>
            <UiLabelsProvider labels={labels}>
              <TooltipProvider delay={200}>
                <ConfirmProvider>
                  <RuntimeShell bootstrap={bootstrap} viewer={viewer}>
                    {children}
                  </RuntimeShell>
                  <Toaster position="bottom-right" />
                </ConfirmProvider>
              </TooltipProvider>
            </UiLabelsProvider>
          </ThemeProvider>
        </QueryClientProvider>
      </CSPProvider>
    </BootstrapContext.Provider>
  )
}
