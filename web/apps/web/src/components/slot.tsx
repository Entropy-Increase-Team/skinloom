'use client'

import type { PluginSlotName, PluginSlotProps, WebPluginSlotBinding } from '@skinloom/plugin-sdk/web'
import type { ComponentType, ReactNode } from 'react'

import { PluginErrorBoundary, PluginWebRuntimeProvider, usePluginRuntime } from '@/lib/plugin-runtime'

type ResolvedSlotEntry = {
  pluginId: string
  rev: string
  order: number
  Component: ComponentType<Record<string, unknown>>
  runtimeContext: ReturnType<typeof usePluginRuntime>['plugins'][number]['runtimeContext']
}

function normalizeBinding(
  binding: WebPluginSlotBinding<PluginSlotName> | undefined,
): { order: number; Component: ComponentType<Record<string, unknown>> } | null {
  if (!binding) return null
  if (typeof binding === 'function') {
    return { order: 0, Component: binding as ComponentType<Record<string, unknown>> }
  }
  if (typeof binding === 'object' && typeof binding.component === 'function') {
    return {
      order: binding.order ?? 0,
      Component: binding.component as ComponentType<Record<string, unknown>>,
    }
  }
  return null
}

/**
 * Named frontend extension point (docs/design/05 §5.3). Renders active plugin widgets
 * registered for `name` sorted by ascending `order`, each wrapped in a per-plugin ErrorBoundary.
 */
export function Slot<K extends PluginSlotName>({
  name,
  props,
  children,
}: {
  name: K
  props?: PluginSlotProps[K] extends Record<string, never> ? Record<string, unknown> | undefined : PluginSlotProps[K]
  children?: ReactNode
}) {
  const { plugins } = usePluginRuntime()

  const entries: ResolvedSlotEntry[] = []
  for (const entry of plugins) {
    if (!entry.meta.slots?.includes(name)) continue
    const raw = entry.definition.slots?.[name] as WebPluginSlotBinding<PluginSlotName> | undefined
    const parsed = normalizeBinding(raw)
    if (!parsed) continue
    entries.push({
      pluginId: entry.meta.id,
      rev: entry.meta.rev || entry.meta.version,
      order: parsed.order,
      Component: parsed.Component,
      runtimeContext: entry.runtimeContext,
    })
  }
  entries.sort((a, b) => a.order - b.order || a.pluginId.localeCompare(b.pluginId))

  if (entries.length === 0 && !children) {
    return null
  }

  return (
    <div data-sl-slot={name} className="contents">
      {entries.length > 0
        ? entries.map(({ pluginId, rev, Component, runtimeContext }) => (
            <PluginErrorBoundary key={`${name}:${pluginId}:${rev}`} pluginId={pluginId}>
              <PluginWebRuntimeProvider value={runtimeContext}>
                <Component {...((props ?? {}) as Record<string, unknown>)} />
              </PluginWebRuntimeProvider>
            </PluginErrorBoundary>
          ))
        : children}
    </div>
  )
}
