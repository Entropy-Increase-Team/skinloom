'use client'

import type { ThemeOverrideTarget } from '@skinloom/plugin-sdk/web'
import { type ReactNode, useEffect, useState } from 'react'

import { useBootstrap } from '@/components/providers'
import { PluginErrorBoundary, PluginWebRuntimeProvider, usePluginRuntime } from '@/lib/plugin-runtime'

/**
 * Level 5 theme React component override point (docs/design/09 §3.2). Renders `fallback`
 * during SSR and initial hydration to prevent layout flash, then swaps to the theme's
 * custom component inside an ErrorBoundary if declared in `theme.overrides`.
 */
export function ThemeOverride({
  name,
  props,
  fallback,
}: {
  name: ThemeOverrideTarget
  props?: Record<string, unknown>
  fallback: ReactNode
}) {
  const boot = useBootstrap()
  const { themeOverrideModule, themeRuntimeContext } = usePluginRuntime()
  const [mounted, setMounted] = useState(false)

  useEffect(() => {
    setMounted(true)
  }, [])

  if (!mounted || !themeOverrideModule || !themeRuntimeContext) {
    return <>{fallback}</>
  }
  const allowed = boot.theme.overrides?.includes(name)
  const OverrideComp = allowed ? themeOverrideModule.overrides?.[name] : undefined
  if (!OverrideComp) {
    return <>{fallback}</>
  }

  return (
    <PluginErrorBoundary key={`${name}:${boot.theme.id}:${boot.theme.version}`} pluginId={boot.theme.id} fallback={fallback}>
      <PluginWebRuntimeProvider value={themeRuntimeContext}>
        <div data-sl-override={name} className="contents">
          <OverrideComp {...(props ?? {})} fallback={fallback} />
        </div>
      </PluginWebRuntimeProvider>
    </PluginErrorBoundary>
  )
}
