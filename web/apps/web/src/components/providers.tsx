'use client'

import { QueryClientProvider } from '@tanstack/react-query'
import { ThemeProvider } from 'next-themes'
import { createContext, useContext, useState } from 'react'

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
  return (
    <BootstrapContext.Provider value={bootstrap}>
      <QueryClientProvider client={queryClient}>
        <ThemeProvider attribute="class" defaultTheme="system" enableSystem disableTransitionOnChange nonce={nonce}>
          {children}
        </ThemeProvider>
      </QueryClientProvider>
    </BootstrapContext.Provider>
  )
}
