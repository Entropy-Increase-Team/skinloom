'use client'

import { ThemeProvider } from 'next-themes'
import { createContext, useContext } from 'react'

import type { SiteBootstrap } from '@/lib/bootstrap'

const BootstrapContext = createContext<SiteBootstrap | null>(null)

/** Bootstrap data for client components (theme, site name, menus, plugins). */
export function useBootstrap(): SiteBootstrap {
  const v = useContext(BootstrapContext)
  if (!v) throw new Error('useBootstrap outside <Providers>')
  return v
}

export function Providers({ bootstrap, children }: { bootstrap: SiteBootstrap; children: React.ReactNode }) {
  return (
    <BootstrapContext.Provider value={bootstrap}>
      <ThemeProvider attribute="class" defaultTheme="system" enableSystem disableTransitionOnChange>
        {children}
      </ThemeProvider>
    </BootstrapContext.Provider>
  )
}
