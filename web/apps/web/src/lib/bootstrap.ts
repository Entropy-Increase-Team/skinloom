import { type Bootstrap, getBootstrap as fetchBootstrap } from '@skinloom/api-client'
import { headers } from 'next/headers'
import { cache } from 'react'

export type { Bootstrap }

export type SiteBootstrap = Bootstrap & {
  /** True when the backend could not be reached and defaults are shown. */
  offline?: boolean
}

/**
 * Site bootstrap for the current request (server only). Deduplicated per request, so
 * the layout, metadata, i18n config and pages share one backend call.
 */
export const getBootstrap = cache(async (): Promise<SiteBootstrap> => {
  const h = await headers()
  const forward: Record<string, string> = {}
  for (const name of ['accept-language', 'cookie']) {
    const v = h.get(name)
    if (v) forward[name] = v
  }
  try {
    const res = await fetchBootstrap(undefined, { headers: forward })
    return res.data as Bootstrap
  } catch {
    return offlineBootstrap(h.get('accept-language') ?? '')
  }
})

function offlineBootstrap(acceptLanguage: string): SiteBootstrap {
  const locale = acceptLanguage.toLowerCase().startsWith('en') ? 'en' : 'zh-CN'
  return {
    offline: true,
    version: -1,
    locale,
    locales: [
      { code: 'zh-CN', name: '简体中文' },
      { code: 'en', name: 'English' },
    ],
    site: { name: locale === 'en' ? 'SkinLoom' : '云织', description: '', url: '', announcement: '' },
    theme: { id: 'none', version: '0', tokens: { light: {}, dark: {} }, css: [], layout: {}, home: [{ type: 'hero' }] },
    menus: { explore: [], user: [], admin: [] },
    plugins: [],
    server: { name: 'SkinLoom', version: 'unknown' },
  }
}
