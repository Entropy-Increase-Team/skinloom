'use client'

import { ConfirmProvider, CSPProvider, Toaster, TooltipProvider, type UiLabels, UiLabelsProvider } from '@skinloom/ui'
import { QueryClientProvider } from '@tanstack/react-query'
import { useMessages } from 'next-intl'
import { ThemeProvider } from 'next-themes'
import { createContext, useContext, useMemo, useState } from 'react'

import type { SiteBootstrap } from '@/lib/bootstrap'
import { makeQueryClient } from '@/lib/query'

const BootstrapContext = createContext<SiteBootstrap | null>(null)

/** Bootstrap data for client components (theme, site name, menus, plugins). */
export function useBootstrap(): SiteBootstrap {
  const v = useContext(BootstrapContext)
  if (!v) throw new Error('useBootstrap outside <Providers>')
  return v
}

export function Providers({
  bootstrap,
  nonce,
  children,
}: {
  bootstrap: SiteBootstrap
  /** The page's CSP nonce, for next-themes' inline script that picks light or dark. */
  nonce?: string
  children: React.ReactNode
}) {
  // One cache per browser tab, kept across navigations and re-renders.
  const [queryClient] = useState(makeQueryClient)
  // The "ui" messages as written: the components fill in {count}, {text}… themselves.
  const messages = useMessages()
  const labels = useMemo(() => (messages.ui ?? {}) as Partial<UiLabels>, [messages])
  return (
    <BootstrapContext.Provider value={bootstrap}>
      {/* Base UI renders a few inline scripts and styles: they need the page's nonce. */}
      <CSPProvider nonce={nonce}>
        <QueryClientProvider client={queryClient}>
          <ThemeProvider attribute="class" defaultTheme="system" enableSystem disableTransitionOnChange nonce={nonce}>
            <UiLabelsProvider labels={labels}>
              <TooltipProvider delay={200}>
                <ConfirmProvider>
                  {children}
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
