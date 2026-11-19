import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  navigate,
  PluginApiError,
  type PluginHostBridge,
  PluginWebProvider,
  usePluginApi,
  useSession,
  useSettings,
  useSite,
  useT,
  useTheme,
} from './index.js'

afterEach(() => {
  delete (globalThis as { __SKINLOOM_PLUGIN_BRIDGE__?: PluginHostBridge }).__SKINLOOM_PLUGIN_BRIDGE__
  vi.restoreAllMocks()
})

describe('@skinloom/plugin-sdk/web', () => {
  it('provides runtime context hooks and interpolates {{key}} and {key} params in useT', () => {
    function Probe() {
      const t = useT()
      const settings = useSettings<{ limit: number }>()
      const session = useSession()
      const site = useSite()
      const theme = useTheme()
      return createElement(
        'div',
        null,
        `${t('welcome', { name: 'Alex', count: 3 })}|${t('legacy', { n: 5 })}|${settings.limit}|${session?.nickname ?? 'anon'}|${site.name}|${theme.colorMode}`,
      )
    }

    const html = renderToStaticMarkup(
      createElement(
        PluginWebProvider,
        {
          value: {
            pluginId: 'demo',
            settings: { limit: 10 },
            messages: {
              welcome: 'Hi {{name}}, {count} items',
              legacy: 'Count: {n}',
            },
            session: {
              id: 1,
              email: 'a@b.c',
              nickname: 'Alex',
              role: 'user',
              score: 0,
              locale: 'en',
            },
            site: { name: 'SkinLoom', url: 'https://skinloom.example', description: '', locale: 'en', locales: ['en'] },
            theme: { id: 'default', name: 'Default', colorMode: 'dark', tokens: {} },
          },
        },
        createElement(Probe),
      ),
    )

    expect(html).toContain('Hi Alex, 3 items|Count: 5|10|Alex|SkinLoom|dark')
  })

  it('validates navigate targets and rejects unsafe schemes or protocol-relative URLs', () => {
    const calls: string[] = []
    ;(globalThis as { __SKINLOOM_PLUGIN_BRIDGE__?: PluginHostBridge }).__SKINLOOM_PLUGIN_BRIDGE__ = {
      navigate: (to) => calls.push(to),
    }

    navigate('/user/closet?tab=1')
    navigate('https://accounts.example/oauth')
    navigate('javascript:alert(1)')
    navigate('data:text/html,<script>')
    navigate('//evil.example/phish')
    navigate('/\\evil.example/phish')
    navigate('   ')
    navigate('/bad\npath')

    expect(calls).toEqual(['/user/closet?tab=1', 'https://accounts.example/oauth'])
  })

  it('formats PluginApiError detail/title/status fallback and executes pluginFetch requests', async () => {
    expect(new PluginApiError(422, { title: 'Unprocessable', detail: 'bad field' }).message).toBe('bad field')
    expect(new PluginApiError(403, { title: 'Forbidden' }).message).toBe('Forbidden')
    expect(new PluginApiError(500, {}).message).toBe('HTTP 500')

    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ ok: true }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    let apiRef: ReturnType<typeof usePluginApi> | null = null
    let mutationDefaultsToPost = false
    function ApiProbe() {
      apiRef = usePluginApi('demo')
      const mut = apiRef.useMutation('/submit')
      mutationDefaultsToPost = typeof mut.mutate === 'function'
      return null
    }

    const qc = new QueryClient()
    renderToStaticMarkup(createElement(QueryClientProvider, { client: qc }, createElement(ApiProbe)))

    expect(mutationDefaultsToPost).toBe(true)
    expect(apiRef).not.toBeNull()
    const res = await apiRef!.post<{ ok: boolean }>('/submit', { code: 'A' })
    expect(res).toEqual({ ok: true })
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/plugins/demo/submit',
      expect.objectContaining({
        method: 'POST',
        credentials: 'same-origin',
        headers: expect.objectContaining({ 'X-SkinLoom-Request': '1', 'Content-Type': 'application/json' }),
      }),
    )

    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(JSON.stringify({ detail: 'denied' }), { status: 403 })),
    )
    await expect(apiRef!.get('/secret')).rejects.toMatchObject({ status: 403, message: 'denied' })
  })
})
