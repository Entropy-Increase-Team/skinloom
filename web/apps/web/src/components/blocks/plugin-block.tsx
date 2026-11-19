'use client'

import { PluginErrorBoundary, PluginWebRuntimeProvider, usePluginRuntime } from '@/lib/plugin-runtime'

/** Renders a plugin-provided homepage block (`type` or `<pluginId>:<type>`) safely inside an ErrorBoundary. */
export function PluginBlock({
  type,
  props,
}: {
  type: string
  props: Record<string, unknown>
}) {
  const { plugins } = usePluginRuntime()
  const [explicitPlugin, explicitBlock] = type.includes(':') ? type.split(':', 2) : ['', type]

  for (const entry of plugins) {
    if (explicitPlugin && entry.meta.id !== explicitPlugin) continue
    if (
      entry.meta.blocks &&
      entry.meta.blocks.length > 0 &&
      !entry.meta.blocks.some(
        (b) => b.type === type || b.type === explicitBlock || `${entry.meta.id}:${b.type}` === type,
      )
    ) {
      continue
    }
    const Comp =
      entry.definition.blocks?.[type] ??
      entry.definition.blocks?.[explicitBlock]
    if (!Comp) continue
    return (
      <PluginErrorBoundary
        key={`${type}:${entry.meta.id}:${entry.meta.rev || entry.meta.version}`}
        pluginId={entry.meta.id}
      >
        <PluginWebRuntimeProvider value={entry.runtimeContext}>
          <div data-sl-plugin-block={type}>
            <Comp props={props} />
          </div>
        </PluginWebRuntimeProvider>
      </PluginErrorBoundary>
    )
  }

  return null
}
