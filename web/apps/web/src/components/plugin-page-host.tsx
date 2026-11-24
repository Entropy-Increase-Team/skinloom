'use client'

import { Button, Empty, EmptyContent, EmptyHeader, EmptyMedia, EmptyTitle, Skeleton } from '@skinloom/ui'
import { Puzzle, TriangleAlert } from 'lucide-react'
import { useTranslations } from 'next-intl'

import { useBootstrap } from '@/components/providers'
import { PluginErrorBoundary, PluginWebRuntimeProvider, usePluginRuntime } from '@/lib/plugin-runtime'

import type { MenuItem } from '@skinloom/api-client'

export function isPluginPageAreaAllowed(
  menus: Partial<Record<'explore' | 'user' | 'admin', MenuItem[]>>,
  pluginId: string,
  area?: 'explore' | 'user' | 'admin',
  subPath?: string,
): boolean {
  if (!area) return true
  const areaPrefix = area === 'explore' ? `/p/${pluginId}` : `/${area}/p/${pluginId}`
  const normSub = !subPath ? '/' : subPath.startsWith('/') ? subPath : `/${subPath}`
  const trimmedSub = normSub === '/' ? '/' : normSub.replace(/\/+$/, '')
  const requestedHref = trimmedSub === '/' ? areaPrefix : `${areaPrefix}${trimmedSub}`
  return (menus[area] ?? []).some((m) => {
    const belongsToPlugin =
      m.id === `plugin:${pluginId}` ||
      m.id.startsWith(`plugin:${pluginId}/`) ||
      m.href === areaPrefix ||
      m.href.startsWith(`${areaPrefix}/`)
    if (!belongsToPlugin) return false
    if (trimmedSub === '/') return true
    const rawHref = (m.href ?? '').split(/[?#]/, 1)[0] ?? ''
    const normHref = rawHref === '/' ? '/' : rawHref.replace(/\/+$/, '')
    if (!normHref || !normHref.startsWith(areaPrefix)) {
      return true
    }
    return requestedHref === normHref || requestedHref.startsWith(`${normHref}/`)
  })
}

export function findPluginPage(
  pages: Record<string, React.ComponentType<Record<string, unknown>>> | undefined,
  pluginId: string,
  subPath: string,
  area?: 'explore' | 'user' | 'admin',
) {
  if (!pages) return undefined
  const norm = subPath.startsWith('/') ? subPath : `/${subPath}`
  const trimmed = norm === '/' ? '/' : norm.replace(/\/+$/, '')
  const scopedCandidates = area
    ? [
        `${area}:${trimmed}`,
        `/${area}${trimmed === '/' ? '' : trimmed}`,
        ...(trimmed === '/' ? [area, `${area}/index`] : [`${area}${trimmed}`, `${area}/${trimmed.slice(1)}`]),
      ]
    : []
  const candidates = [
    ...scopedCandidates,
    trimmed,
    `/${pluginId}${trimmed === '/' ? '' : trimmed}`,
    ...(trimmed === '/' ? ['index'] : [trimmed.slice(1)]),
  ]
  for (const key of candidates) {
    if (pages[key]) return pages[key]
  }
  return undefined
}

/** Client host container that renders a dynamic plugin route component under `/p/*`, `/user/p/*`, or `/admin/p/*`. */
export function PluginPageHost({
  pluginId,
  slug,
  area,
}: {
  pluginId: string
  slug?: string[]
  area?: 'explore' | 'user' | 'admin'
}) {
  const t = useTranslations('appearance.themes')
  const tc = useTranslations('common')
  const boot = useBootstrap()
  const { pluginsById, loading } = usePluginRuntime()
  const entry = pluginsById.get(pluginId)
  const subPath = slug && slug.length > 0 ? `/${slug.join('/')}` : '/'
  const areaAllowed = isPluginPageAreaAllowed(boot.menus, pluginId, area, subPath)

  if (loading && !entry) {
    return <Skeleton className="h-64 rounded-xl" />
  }

  const PageComp =
    entry && areaAllowed ? findPluginPage(entry.definition.pages, pluginId, subPath, area) : undefined
  if (!entry || !areaAllowed || !PageComp) {
    return (
      <Empty className="border border-dashed py-16">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <Puzzle />
          </EmptyMedia>
          <EmptyTitle>{t('pluginNotFound')}</EmptyTitle>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <PluginErrorBoundary
      key={`${area ?? 'any'}:${pluginId}:${subPath}:${entry.meta.rev || entry.meta.version}`}
      pluginId={pluginId}
      fallback={
        <Empty className="border border-dashed border-destructive/40 py-16">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <TriangleAlert className="text-destructive" />
            </EmptyMedia>
            <EmptyTitle>{t('pluginCrashed')}</EmptyTitle>
          </EmptyHeader>
          <EmptyContent>
            <Button type="button" variant="outline" size="sm" onClick={() => window.location.reload()}>
              {tc('retry')}
            </Button>
          </EmptyContent>
        </Empty>
      }
    >
      <PluginWebRuntimeProvider value={entry.runtimeContext}>
        <div data-sl-plugin-page={`${pluginId}${subPath}`} className="flex flex-col gap-6">
          <PageComp pluginId={pluginId} path={subPath} area={area} />
        </div>
      </PluginWebRuntimeProvider>
    </PluginErrorBoundary>
  )
}
